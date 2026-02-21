package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/api"
	"github.com/ndrandal/feed-simulator/go-feed/internal/archive"
	"github.com/ndrandal/feed-simulator/go-feed/internal/config"
	"github.com/ndrandal/feed-simulator/go-feed/internal/engine"
	"github.com/ndrandal/feed-simulator/go-feed/internal/itch"
	"github.com/ndrandal/feed-simulator/go-feed/internal/orderbook"
	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
	"github.com/ndrandal/feed-simulator/go-feed/internal/session"
	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

func main() {
	cfg := config.Load()

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Println("feed simulator starting")

	// Context with graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		cancel()
	}()

	// PRNG
	rng := engine.NewRNG(cfg.Seed)
	log.Printf("PRNG seed: %d", cfg.Seed)

	// Symbols
	syms := symbol.AllSymbols()
	log.Printf("loaded %d symbols", len(syms))

	// Market engine
	market := engine.NewMarketEngine(rng, syms)

	// Order books + simulators
	books := make(map[uint16]*orderbook.Simulator, len(syms))
	for _, s := range syms {
		book := orderbook.NewBook(s.LocateCode, s.TickSize)
		sim := orderbook.NewSimulator(rng, book, s.LocateCode, s.TickSize)
		books[s.LocateCode] = sim
	}

	// MongoDB
	store, err := persist.NewStore(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer store.Close(context.Background())

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	// Persistence snapshotter
	snapshotter := persist.NewSnapshotter(store, market, books, rng, syms)

	// Try to restore state
	restored, err := snapshotter.Load(ctx)
	if err != nil {
		log.Printf("warning: failed to load state: %v", err)
	}

	// If not restored, initialize order books with base prices
	if !restored {
		log.Println("initializing order books from base prices...")
		for _, s := range syms {
			sim := books[s.LocateCode]
			sim.Initialize(s.BasePrice)
		}
	}

	// Session manager
	mgr := session.NewManager(syms, cfg.SendBufferSize)

	// Trade persistence workers
	tradeCh := make(chan tradeRecord, 4096)
	for i := 0; i < 2; i++ {
		go tradeWriter(ctx, snapshotter, tradeCh)
	}

	// Start symbol runners (29 normal + 1 stress)
	for _, s := range syms {
		if s.IsStress {
			go stressRunner(ctx, s, market, books[s.LocateCode], mgr, rng, cfg, tradeCh)
		} else {
			go symbolRunner(ctx, s, market, books[s.LocateCode], mgr, cfg.TickInterval, tradeCh)
		}
	}
	log.Printf("started %d symbol runners", len(syms))

	// Start persister
	go snapshotter.Run(ctx, cfg.SnapshotInterval)
	log.Println("started persistence snapshotter")

	// Start trade retention pruner
	go persist.RunRetention(ctx, store, cfg.TradeRetentionDays)

	// Start trade archiver (opt-in)
	if cfg.ArchiveDir != "" {
		archiver := archive.New(store.DB(), cfg.ArchiveDir, cfg.ArchiveMaxGB, cfg.ArchiveIntervalHours, cfg.ArchiveAfterHours)
		go archiver.Run(ctx)
	}

	// HTTP/WebSocket server
	mux := http.NewServeMux()
	mux.HandleFunc("/feed", session.Handler(mgr))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","clients":%d,"symbols":%d}`, mgr.ClientCount(), len(syms))
	})

	// REST API
	apiServer := api.NewServer(persist.NewMongoTradeReader(store.DB()), market, books, mgr, syms)
	apiServer.Register(mux)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.WSPort)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("WebSocket server listening on ws://%s/feed", addr)
	log.Printf("Health check: http://%s/health", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}

	log.Println("feed simulator stopped")
}

// symbolRunner runs a single normal symbol's tick loop at a fixed interval.
func symbolRunner(ctx context.Context, sym symbol.Symbol, market *engine.MarketEngine, sim *orderbook.Simulator, mgr *session.Manager, interval time.Duration, tradeCh chan<- tradeRecord) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Generate sector shocks (safe to call from multiple goroutines)
			market.GenerateSectorShocks()

			// Tick price
			price := market.Tick(sym.LocateCode)

			// Order book actions (1-3 per tick for normal symbols)
			numActions := 1 + int(sim.Book().OrderCount()%3) // vary slightly
			if numActions > 3 {
				numActions = 3
			}
			if numActions < 1 {
				numActions = 1
			}

			msgs := sim.Step(price, numActions)

			// Enqueue trades for persistence
			enqueueTrades(tradeCh, sym.LocateCode, msgs)

			// Broadcast to subscribed clients
			mgr.Broadcast(sym.LocateCode, sym.Ticker, msgs)
		}
	}
}

// stressRunner runs the BLITZ stress symbol with variable-rate ticking.
func stressRunner(ctx context.Context, sym symbol.Symbol, market *engine.MarketEngine, sim *orderbook.Simulator, mgr *session.Manager, rng *engine.RNG, cfg *config.Config, tradeCh chan<- tradeRecord) {
	stressCfg := engine.StressConfig{
		CalmMinMs:   cfg.StressCalmMinMs,
		CalmMaxMs:   cfg.StressCalmMaxMs,
		ActiveMinMs: cfg.StressActiveMinMs,
		ActiveMaxMs: cfg.StressActiveMaxMs,
		BurstMinMs:  cfg.StressBurstMinMs,
		BurstMaxMs:  cfg.StressBurstMaxMs,
	}
	ctrl := engine.NewStressController(rng, stressCfg)

	lastPhaseLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		interval, numActions := ctrl.Tick()

		// Log phase changes periodically
		if time.Since(lastPhaseLog) > 5*time.Second {
			log.Printf("BLITZ: phase=%s intensity=%.2f interval=%v actions=%d",
				ctrl.Phase(), ctrl.Intensity(), interval, numActions)
			lastPhaseLog = time.Now()
		}

		// Generate sector shocks
		market.GenerateSectorShocks()

		// Tick price
		price := market.Tick(sym.LocateCode)

		// Order book actions
		msgs := sim.Step(price, numActions)

		// Enqueue trades for persistence
		enqueueTrades(tradeCh, sym.LocateCode, msgs)

		// Broadcast
		mgr.Broadcast(sym.LocateCode, sym.Ticker, msgs)

		// Send system event for burst starts
		if ctrl.Phase() == engine.PhaseBurst && ctrl.Intensity() > 0.9 {
			burstMsg := itch.Message{
				Type:        itch.MsgSystemEvent,
				StockLocate: sym.LocateCode,
				EventCode:   itch.EventStartOfMarket,
			}
			mgr.Broadcast(sym.LocateCode, sym.Ticker, []itch.Message{burstMsg})
		}

		time.Sleep(interval)
	}
}

// tradeRecord is a value sent through the trade persistence channel.
type tradeRecord struct {
	matchNumber uint64
	locate      uint16
	price       float64
	shares      int32
	aggressor   byte
}

// enqueueTrades sends trade messages to the persistence channel.
// Drops silently if the channel buffer is full (back-pressure).
func enqueueTrades(ch chan<- tradeRecord, locate uint16, msgs []itch.Message) {
	for i := range msgs {
		if msgs[i].Type != itch.MsgTrade {
			continue
		}
		select {
		case ch <- tradeRecord{
			matchNumber: msgs[i].MatchNumber,
			locate:      locate,
			price:       msgs[i].Price,
			shares:      msgs[i].Shares,
			aggressor:   msgs[i].Side,
		}:
		default:
			// buffer full — drop trade rather than block the ticker
		}
	}
}

// tradeWriter drains the trade channel and writes to the DB.
func tradeWriter(ctx context.Context, snap *persist.Snapshotter, ch <-chan tradeRecord) {
	for {
		select {
		case <-ctx.Done():
			return
		case tr := <-ch:
			snap.SaveTrade(context.Background(), tr.matchNumber, tr.locate, tr.price, tr.shares, tr.aggressor)
		}
	}
}
