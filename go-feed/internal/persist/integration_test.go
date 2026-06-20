package persist

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestPool connects to the database named by TEST_DATABASE_URL and skips the
// test when it is unset, so unit-only runs (and CI without a DB) stay green.
// Example: TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5456/feedsim_test?sslmode=disable
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run persist integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping: %v", err)
	}
	if err := CreateTables(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedTrades replaces the trades table contents with the given rows.
func seedTrades(t *testing.T, pool *pgxpool.Pool, trades []Trade, locate uint16) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE trades`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	for i, tr := range trades {
		_, err := pool.Exec(ctx,
			`INSERT INTO trades (match_number, symbol_locate, ticker, price, shares, aggressor, executed_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			int64(i+1), int16(locate), tr.Ticker, tr.Price, tr.Shares, tr.Aggressor, tr.ExecutedAt)
		if err != nil {
			t.Fatalf("insert trade: %v", err)
		}
	}
}

func TestPgQueryTradesClampAndOrder(t *testing.T) {
	pool := newTestPool(t)
	r := NewPgTradeReader(pool)
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	var trades []Trade
	for i := 0; i < 5; i++ {
		trades = append(trades, Trade{
			Ticker: "NEXO", Price: 100 + float64(i), Shares: 10,
			Aggressor: "B", ExecutedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	seedTrades(t, pool, trades, 1)

	// Oversized limit clamps but still returns all 5, newest-first.
	got, err := r.QueryTrades(context.Background(), TradeFilter{SymbolLocate: 1, Limit: 9999})
	if err != nil {
		t.Fatalf("QueryTrades: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("expected 5 trades, got %d", len(got))
	}
	if !got[0].ExecutedAt.After(got[4].ExecutedAt) {
		t.Errorf("expected newest-first ordering, got %v then %v", got[0].ExecutedAt, got[4].ExecutedAt)
	}
}

func TestPgQueryCandlesBeforeAndFill(t *testing.T) {
	pool := newTestPool(t)
	r := NewPgTradeReader(pool)
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	// Trades in minute buckets 10:00, 10:01, and 10:03 (gap at 10:02).
	trades := []Trade{
		{Ticker: "NEXO", Price: 100, Shares: 10, Aggressor: "B", ExecutedAt: base},
		{Ticker: "NEXO", Price: 101, Shares: 10, Aggressor: "B", ExecutedAt: base.Add(1 * time.Minute)},
		{Ticker: "NEXO", Price: 103, Shares: 10, Aggressor: "B", ExecutedAt: base.Add(3 * time.Minute)},
	}
	seedTrades(t, pool, trades, 1)

	// Without fill: 3 non-empty buckets, newest-first.
	got, err := r.QueryCandles(context.Background(), CandleFilter{SymbolLocate: 1, Interval: "1m"})
	if err != nil {
		t.Fatalf("QueryCandles: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 candles, got %d", len(got))
	}

	// before cursor excludes the 10:03 bucket (executed_at < 10:03).
	cursor := base.Add(3 * time.Minute)
	got, err = r.QueryCandles(context.Background(), CandleFilter{SymbolLocate: 1, Interval: "1m", Before: &cursor})
	if err != nil {
		t.Fatalf("QueryCandles before: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 candles before cursor, got %d", len(got))
	}

	// fill=zero across 10:00..10:03 yields 4 contiguous buckets with a zero bar at 10:02.
	from := base
	to := base.Add(3 * time.Minute)
	got, err = r.QueryCandles(context.Background(), CandleFilter{SymbolLocate: 1, Interval: "1m", From: &from, To: &to, Fill: true})
	if err != nil {
		t.Fatalf("QueryCandles fill: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 filled buckets, got %d", len(got))
	}
	// newest-first; the gap bucket (10:02) is zero volume.
	gap := got[1]
	if !gap.Bucket.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("expected gap bucket at 10:02, got %v", gap.Bucket)
	}
	if gap.Volume != 0 || gap.Count != 0 {
		t.Errorf("expected zero bar at gap, got %+v", gap)
	}
}

func TestPgQueryTradesMulti(t *testing.T) {
	pool := newTestPool(t)
	r := NewPgTradeReader(pool)
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// Two symbols interleaved in time; ACME(locate 2) and NEXO(locate 1).
	if _, err := pool.Exec(ctx, `TRUNCATE trades`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	rows := []struct {
		mn     int64
		locate int16
		ticker string
		minute int
	}{
		{1, 1, "NEXO", 0},
		{2, 2, "ACME", 1},
		{3, 1, "NEXO", 2},
		{4, 2, "ACME", 2}, // same bucket time as #3 -> ticker tiebreak
	}
	for _, x := range rows {
		_, err := pool.Exec(ctx,
			`INSERT INTO trades (match_number, symbol_locate, ticker, price, shares, aggressor, executed_at)
			 VALUES ($1,$2,$3,100,10,'B',$4)`,
			x.mn, x.locate, x.ticker, base.Add(time.Duration(x.minute)*time.Minute))
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	got, err := r.QueryTradesMulti(ctx, MultiTradeFilter{Locates: []uint16{1, 2}, Limit: 100})
	if err != nil {
		t.Fatalf("QueryTradesMulti: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 trades, got %d", len(got))
	}
	// Newest-first; the 10:02 tie resolves ACME before NEXO (ticker ASC).
	if got[0].ExecutedAt.Before(got[3].ExecutedAt) {
		t.Errorf("expected newest-first, got %v .. %v", got[0].ExecutedAt, got[3].ExecutedAt)
	}
	if got[0].Ticker != "ACME" || got[1].Ticker != "NEXO" {
		t.Errorf("ticker tiebreak failed: got[0]=%s got[1]=%s", got[0].Ticker, got[1].Ticker)
	}

	// Empty locates -> empty result, no error.
	empty, err := r.QueryTradesMulti(ctx, MultiTradeFilter{Locates: nil, Limit: 100})
	if err != nil || len(empty) != 0 {
		t.Errorf("expected empty result for no locates, got %d err=%v", len(empty), err)
	}
}

func TestPgQueryDBSize(t *testing.T) {
	pool := newTestPool(t)
	r := NewPgTradeReader(pool)
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	seedTrades(t, pool, []Trade{
		{Ticker: "NEXO", Price: 100, Shares: 10, Aggressor: "B", ExecutedAt: base},
	}, 1)

	size, err := r.QueryDBSize(context.Background())
	if err != nil {
		t.Fatalf("QueryDBSize: %v", err)
	}
	if size.DatabaseBytes <= 0 {
		t.Errorf("expected positive database size, got %d", size.DatabaseBytes)
	}
	if size.TradesBytes < 0 || size.TradesIndexBytes <= 0 {
		t.Errorf("unexpected table/index sizes: %+v", size)
	}
	if size.PctOfBudget() <= 0 {
		t.Errorf("expected positive pct, got %v", size.PctOfBudget())
	}
}
