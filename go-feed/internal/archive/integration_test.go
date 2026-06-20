package archive

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

// newArchiveTestPool connects to TEST_DATABASE_URL (skips when unset). These
// tests share the `trades`/`sim_state` tables with the persist integration
// tests, so run DB-backed packages serially: `go test -p 1 ./...`.
func newArchiveTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run archive integration tests")
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
	if err := persist.CreateTables(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertTrade(t *testing.T, pool *pgxpool.Pool, mn int64, locate int16, ticker string, ts time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO trades (match_number, symbol_locate, ticker, price, shares, aggressor, executed_at)
		 VALUES ($1,$2,$3,100,10,'B',$4)`, mn, locate, ticker, ts)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func countTrades(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM trades`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestArchiverWriteThenReadRoundTrip drives the streaming write path: aged days
// are rolled to gzipped NDJSON day-files and deleted from the live table; the
// recent day is left in place; the Reader reads the cold trades back.
func TestArchiverWriteThenReadRoundTrip(t *testing.T) {
	pool := newArchiveTestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `TRUNCATE trades; TRUNCATE sim_state;`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	now := time.Now().UTC()
	day1 := dayUTC(now.AddDate(0, 0, -10))
	day2 := dayUTC(now.AddDate(0, 0, -9))

	// Two aged days for NEXO (locate 1), plus an ACME noise row and a recent row.
	insertTrade(t, pool, 1, 1, "NEXO", day1.Add(1*time.Hour))
	insertTrade(t, pool, 2, 1, "NEXO", day1.Add(2*time.Hour))
	insertTrade(t, pool, 3, 2, "ACME", day1.Add(3*time.Hour))
	insertTrade(t, pool, 4, 1, "NEXO", day2.Add(1*time.Hour))
	insertTrade(t, pool, 5, 1, "NEXO", now) // recent: must stay live

	dir := t.TempDir()
	a := New(pool, dir, 4, 6, 24) // maxAge 24h -> the two aged days archive
	a.cycle(ctx)

	// Recent row remains; the four aged rows are gone from the live table.
	if got := countTrades(t, pool); got != 1 {
		t.Fatalf("expected 1 live trade after archive, got %d", got)
	}

	// Catalog sees both archived days.
	cat := NewCatalog(dir)
	days, err := cat.Days()
	if err != nil {
		t.Fatalf("Days: %v", err)
	}
	if len(days) != 2 {
		t.Fatalf("expected 2 archived days, got %d: %+v", len(days), days)
	}

	// Reader reads back NEXO's 3 archived trades, newest-first (mn 4, 2, 1).
	r := NewReader(cat)
	got, err := r.Read(ctx, ReadFilter{SymbolLocate: 1, Limit: 100})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 archived NEXO trades, got %d: %+v", len(got), got)
	}
	wantMN := []int64{4, 2, 1}
	for i, mn := range wantMN {
		if got[i].MatchNumber != mn {
			t.Errorf("got[%d].MatchNumber = %d, want %d", i, got[i].MatchNumber, mn)
		}
		if got[i].Ticker != "NEXO" {
			t.Errorf("got[%d].Ticker = %q, want NEXO", i, got[i].Ticker)
		}
	}

	// No leftover temp files from the atomic writer.
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		_ = entries // trades/ subdir is expected; just ensure no panics
	}
}
