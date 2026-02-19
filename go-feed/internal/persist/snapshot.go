package persist

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/ndrandal/feed-simulator/go-feed/internal/engine"
	"github.com/ndrandal/feed-simulator/go-feed/internal/orderbook"
	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

// Snapshotter manages periodic persistence of simulator state.
type Snapshotter struct {
	store     *Store
	market    *engine.MarketEngine
	books     map[uint16]*orderbook.Simulator
	rng       *engine.RNG
	syms      []symbol.Symbol
	tickerMap map[uint16]string // locate → ticker for trade denormalization
}

// NewSnapshotter creates a new snapshotter.
func NewSnapshotter(store *Store, market *engine.MarketEngine, books map[uint16]*orderbook.Simulator, rng *engine.RNG, syms []symbol.Symbol) *Snapshotter {
	tm := make(map[uint16]string, len(syms))
	for _, s := range syms {
		tm[s.LocateCode] = s.Ticker
	}
	return &Snapshotter{
		store:     store,
		market:    market,
		books:     books,
		rng:       rng,
		syms:      syms,
		tickerMap: tm,
	}
}

// Run starts the periodic snapshot loop. Blocks until ctx is cancelled.
func (s *Snapshotter) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final snapshot on shutdown
			log.Println("performing final snapshot...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := s.Save(shutdownCtx); err != nil {
				log.Printf("final snapshot error: %v", err)
			}
			cancel()
			return
		case <-ticker.C:
			if err := s.Save(ctx); err != nil {
				log.Printf("snapshot error: %v", err)
			}
		}
	}
}

// Save persists the full simulator state to MongoDB in a single transaction.
func (s *Snapshotter) Save(ctx context.Context) error {
	start := time.Now()

	session, err := s.store.client.StartSession()
	if err != nil {
		return fmt.Errorf("start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sc context.Context) (any, error) {
		db := s.store.db
		now := time.Now()

		// 1. Upsert symbol prices
		prices := s.market.AllPrices()
		for _, sym := range s.syms {
			price := prices[sym.LocateCode]
			filter := bson.M{"locate_code": sym.LocateCode}
			update := bson.M{"$set": bson.M{
				"locate_code":   sym.LocateCode,
				"ticker":        sym.Ticker,
				"name":          sym.Name,
				"sector":        string(sym.Sector),
				"base_price":    sym.BasePrice,
				"current_price": price,
				"tick_size":     sym.TickSize,
				"volatility":    sym.VolatilityMultiplier,
				"is_stress":     sym.IsStress,
			}}
			opts := options.UpdateOne().SetUpsert(true)
			if _, err := db.Collection("symbols").UpdateOne(sc, filter, update, opts); err != nil {
				return nil, fmt.Errorf("upsert symbol %s: %w", sym.Ticker, err)
			}
		}

		// 2. Replace all orders: delete then bulk insert
		if _, err := db.Collection("orders").DeleteMany(sc, bson.M{}); err != nil {
			return nil, fmt.Errorf("delete orders: %w", err)
		}

		var docs []any
		for _, sim := range s.books {
			for _, o := range sim.Book().AllOrders() {
				docs = append(docs, bson.M{
					"id":            int64(o.ID),
					"symbol_locate": o.Locate,
					"side":          string(o.Side),
					"price":         o.Price,
					"shares":        o.Shares,
					"priority":      o.Priority,
					"mpid":          o.MPID,
				})
			}
		}
		if len(docs) > 0 {
			if _, err := db.Collection("orders").InsertMany(sc, docs); err != nil {
				return nil, fmt.Errorf("insert orders: %w", err)
			}
		}

		// 3. Upsert PRNG state
		rngState := s.rng.StateBytes()
		if _, err := db.Collection("sim_state").UpdateOne(sc,
			bson.M{"key": "rng_state"},
			bson.M{"$set": bson.M{
				"key":         "rng_state",
				"value_bytes": rngState,
				"updated_at":  now,
			}},
			options.UpdateOne().SetUpsert(true),
		); err != nil {
			return nil, fmt.Errorf("save rng state: %w", err)
		}

		// 4. Upsert order ID counter
		if _, err := db.Collection("sim_state").UpdateOne(sc,
			bson.M{"key": "order_id_counter"},
			bson.M{"$set": bson.M{
				"key":        "order_id_counter",
				"value_int":  int64(orderbook.GetOrderIDCounter()),
				"updated_at": now,
			}},
			options.UpdateOne().SetUpsert(true),
		); err != nil {
			return nil, fmt.Errorf("save order counter: %w", err)
		}

		// 5. Upsert match counter
		if _, err := db.Collection("sim_state").UpdateOne(sc,
			bson.M{"key": "match_counter"},
			bson.M{"$set": bson.M{
				"key":        "match_counter",
				"value_int":  int64(orderbook.GetMatchCounter()),
				"updated_at": now,
			}},
			options.UpdateOne().SetUpsert(true),
		); err != nil {
			return nil, fmt.Errorf("save match counter: %w", err)
		}

		return nil, nil
	})
	if err != nil {
		return fmt.Errorf("snapshot transaction: %w", err)
	}

	log.Printf("snapshot saved in %v", time.Since(start))
	return nil
}

// Load restores simulator state from MongoDB.
// Returns true if state was found and loaded, false for fresh start.
func (s *Snapshotter) Load(ctx context.Context) (bool, error) {
	db := s.store.db

	// Check if symbols collection has data
	count, err := db.Collection("symbols").CountDocuments(ctx, bson.M{})
	if err != nil {
		return false, fmt.Errorf("check symbols: %w", err)
	}
	if count == 0 {
		log.Println("no persisted state found, starting fresh")
		return false, nil
	}

	// Load prices
	cursor, err := db.Collection("symbols").Find(ctx, bson.M{})
	if err != nil {
		return false, fmt.Errorf("load prices: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var doc struct {
			LocateCode   uint16  `bson:"locate_code"`
			CurrentPrice float64 `bson:"current_price"`
		}
		if err := cursor.Decode(&doc); err != nil {
			return false, fmt.Errorf("decode symbol: %w", err)
		}
		s.market.SetPrice(doc.LocateCode, doc.CurrentPrice)
	}
	if err := cursor.Err(); err != nil {
		return false, fmt.Errorf("iterate symbols: %w", err)
	}

	// Load orders
	orderCursor, err := db.Collection("orders").Find(ctx, bson.M{})
	if err != nil {
		return false, fmt.Errorf("load orders: %w", err)
	}
	defer orderCursor.Close(ctx)

	orderCount := 0
	for orderCursor.Next(ctx) {
		var doc struct {
			ID       int64   `bson:"id"`
			Locate   uint16  `bson:"symbol_locate"`
			Side     string  `bson:"side"`
			Price    float64 `bson:"price"`
			Shares   int32   `bson:"shares"`
			Priority int32   `bson:"priority"`
			MPID     string  `bson:"mpid"`
		}
		if err := orderCursor.Decode(&doc); err != nil {
			return false, fmt.Errorf("decode order: %w", err)
		}

		sim, ok := s.books[doc.Locate]
		if !ok {
			continue
		}

		o := &orderbook.Order{
			ID:       uint64(doc.ID),
			Locate:   doc.Locate,
			Side:     orderbook.Side(doc.Side[0]),
			Price:    doc.Price,
			Shares:   doc.Shares,
			Priority: doc.Priority,
			MPID:     doc.MPID,
		}
		sim.Book().RestoreOrder(o)
		orderCount++
	}
	if err := orderCursor.Err(); err != nil {
		return false, fmt.Errorf("iterate orders: %w", err)
	}

	// Load PRNG state
	var stateDoc struct {
		ValueBytes []byte `bson:"value_bytes"`
	}
	err = db.Collection("sim_state").FindOne(ctx, bson.M{"key": "rng_state"}).Decode(&stateDoc)
	if err == nil && len(stateDoc.ValueBytes) >= 16 {
		s.rng.RestoreStateBytes(stateDoc.ValueBytes)
	}

	// Load counters
	var intDoc struct {
		ValueInt int64 `bson:"value_int"`
	}
	err = db.Collection("sim_state").FindOne(ctx, bson.M{"key": "order_id_counter"}).Decode(&intDoc)
	if err == nil {
		orderbook.SetOrderIDCounter(uint64(intDoc.ValueInt))
	}

	err = db.Collection("sim_state").FindOne(ctx, bson.M{"key": "match_counter"}).Decode(&intDoc)
	if err == nil {
		orderbook.SetMatchCounter(uint64(intDoc.ValueInt))
	}

	log.Printf("restored state: %d symbols, %d orders", count, orderCount)
	return true, nil
}

// SaveTrade persists a single trade to the trades log.
func (s *Snapshotter) SaveTrade(ctx context.Context, matchNumber uint64, locate uint16, price float64, shares int32, aggressor byte) error {
	ticker := s.tickerMap[locate]
	_, err := s.store.db.Collection("trades").InsertOne(ctx, bson.M{
		"match_number":  int64(matchNumber),
		"symbol_locate": locate,
		"ticker":        ticker,
		"price":         price,
		"shares":        shares,
		"aggressor":     string(aggressor),
		"executed_at":   time.Now(),
	})
	if err != nil && mongo.IsDuplicateKeyError(err) {
		return nil // idempotent — ignore duplicates
	}
	return err
}
