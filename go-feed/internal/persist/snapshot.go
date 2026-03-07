package persist

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

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
	tickerMap map[uint16]string // locate -> ticker for trade denormalization
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

// Save persists the full simulator state to PostgreSQL in a single transaction.
func (s *Snapshotter) Save(ctx context.Context) error {
	start := time.Now()

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now()

	// 1. Upsert symbol prices
	prices := s.market.AllPrices()
	for _, sym := range s.syms {
		price := prices[sym.LocateCode]
		_, err := tx.Exec(ctx,
			`INSERT INTO symbols (locate_code, ticker, name, sector, base_price, current_price, tick_size, volatility, is_stress)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			 ON CONFLICT (locate_code) DO UPDATE SET current_price = EXCLUDED.current_price`,
			int16(sym.LocateCode), sym.Ticker, sym.Name, string(sym.Sector),
			sym.BasePrice, price, sym.TickSize, sym.VolatilityMultiplier, sym.IsStress)
		if err != nil {
			return fmt.Errorf("upsert symbol %s: %w", sym.Ticker, err)
		}
	}

	// 2. Replace all orders: delete then bulk copy
	if _, err := tx.Exec(ctx, "DELETE FROM orders"); err != nil {
		return fmt.Errorf("delete orders: %w", err)
	}

	var allOrders []*orderbook.Order
	for _, sim := range s.books {
		allOrders = append(allOrders, sim.Book().AllOrders()...)
	}
	if len(allOrders) > 0 {
		_, err = tx.CopyFrom(ctx,
			pgx.Identifier{"orders"},
			[]string{"id", "symbol_locate", "side", "price", "shares", "priority", "mpid"},
			pgx.CopyFromSlice(len(allOrders), func(i int) ([]any, error) {
				o := allOrders[i]
				return []any{int64(o.ID), int16(o.Locate), string(o.Side), o.Price, o.Shares, o.Priority, o.MPID}, nil
			}),
		)
		if err != nil {
			return fmt.Errorf("copy orders: %w", err)
		}
	}

	// 3. Upsert PRNG state
	rngState := s.rng.StateBytes()
	_, err = tx.Exec(ctx,
		`INSERT INTO sim_state (key, value_bytes, updated_at)
		 VALUES ('rng_state', $1, $2)
		 ON CONFLICT (key) DO UPDATE SET value_bytes = EXCLUDED.value_bytes, updated_at = EXCLUDED.updated_at`,
		rngState, now)
	if err != nil {
		return fmt.Errorf("save rng state: %w", err)
	}

	// 4. Upsert order ID counter
	_, err = tx.Exec(ctx,
		`INSERT INTO sim_state (key, value_int, updated_at)
		 VALUES ('order_id_counter', $1, $2)
		 ON CONFLICT (key) DO UPDATE SET value_int = EXCLUDED.value_int, updated_at = EXCLUDED.updated_at`,
		int64(orderbook.GetOrderIDCounter()), now)
	if err != nil {
		return fmt.Errorf("save order counter: %w", err)
	}

	// 5. Upsert match counter
	_, err = tx.Exec(ctx,
		`INSERT INTO sim_state (key, value_int, updated_at)
		 VALUES ('match_counter', $1, $2)
		 ON CONFLICT (key) DO UPDATE SET value_int = EXCLUDED.value_int, updated_at = EXCLUDED.updated_at`,
		int64(orderbook.GetMatchCounter()), now)
	if err != nil {
		return fmt.Errorf("save match counter: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit snapshot: %w", err)
	}

	log.Printf("snapshot saved in %v", time.Since(start))
	return nil
}

// Load restores simulator state from PostgreSQL.
// Returns true if state was found and loaded, false for fresh start.
func (s *Snapshotter) Load(ctx context.Context) (bool, error) {
	pool := s.store.pool

	// Check if symbols table has data
	var count int
	err := pool.QueryRow(ctx, "SELECT count(*) FROM symbols").Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check symbols: %w", err)
	}
	if count == 0 {
		log.Println("no persisted state found, starting fresh")
		return false, nil
	}

	// Load prices
	rows, err := pool.Query(ctx, "SELECT locate_code, current_price FROM symbols")
	if err != nil {
		return false, fmt.Errorf("load prices: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var locate int16
		var price float64
		if err := rows.Scan(&locate, &price); err != nil {
			return false, fmt.Errorf("scan symbol: %w", err)
		}
		s.market.SetPrice(uint16(locate), price)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate symbols: %w", err)
	}

	// Load orders
	orderRows, err := pool.Query(ctx, "SELECT id, symbol_locate, side, price, shares, priority, mpid FROM orders")
	if err != nil {
		return false, fmt.Errorf("load orders: %w", err)
	}
	defer orderRows.Close()

	orderCount := 0
	for orderRows.Next() {
		var id int64
		var locate int16
		var side string
		var price float64
		var shares, priority int32
		var mpid string
		if err := orderRows.Scan(&id, &locate, &side, &price, &shares, &priority, &mpid); err != nil {
			return false, fmt.Errorf("scan order: %w", err)
		}

		sim, ok := s.books[uint16(locate)]
		if !ok {
			continue
		}

		o := &orderbook.Order{
			ID:       uint64(id),
			Locate:   uint16(locate),
			Side:     orderbook.Side(side[0]),
			Price:    price,
			Shares:   shares,
			Priority: priority,
			MPID:     mpid,
		}
		sim.Book().RestoreOrder(o)
		orderCount++
	}
	if err := orderRows.Err(); err != nil {
		return false, fmt.Errorf("iterate orders: %w", err)
	}

	// Load PRNG state
	var rngState []byte
	err = pool.QueryRow(ctx, "SELECT value_bytes FROM sim_state WHERE key = 'rng_state'").Scan(&rngState)
	if err == nil && len(rngState) >= 16 {
		s.rng.RestoreStateBytes(rngState)
	}

	// Load counters
	var intVal int64
	err = pool.QueryRow(ctx, "SELECT value_int FROM sim_state WHERE key = 'order_id_counter'").Scan(&intVal)
	if err == nil {
		orderbook.SetOrderIDCounter(uint64(intVal))
	}

	err = pool.QueryRow(ctx, "SELECT value_int FROM sim_state WHERE key = 'match_counter'").Scan(&intVal)
	if err == nil {
		orderbook.SetMatchCounter(uint64(intVal))
	}

	log.Printf("restored state: %d symbols, %d orders", count, orderCount)
	return true, nil
}

// SaveTrade persists a single trade to the trades log.
func (s *Snapshotter) SaveTrade(ctx context.Context, matchNumber uint64, locate uint16, price float64, shares int32, aggressor byte) error {
	ticker := s.tickerMap[locate]
	_, err := s.store.pool.Exec(ctx,
		`INSERT INTO trades (match_number, symbol_locate, ticker, price, shares, aggressor, executed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (match_number) DO NOTHING`,
		int64(matchNumber), int16(locate), ticker, price, shares, string(aggressor), time.Now())
	return err
}
