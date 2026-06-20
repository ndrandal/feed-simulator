package archive

import (
	"context"
	"testing"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

func tradeDocAt(mn int64, locate int16, price float64, shares int32, ts time.Time) tradeDoc {
	return tradeDoc{MatchNumber: mn, SymbolLocate: locate, Ticker: "NEXO", Price: price, Shares: shares, Aggressor: "B", ExecutedAt: ts}
}

func TestReadCandlesBucketing(t *testing.T) {
	dir := t.TempDir()
	d := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	// 10:00 bucket: two trades (open 100, high 102, low 99, close 102, vol 15).
	// 10:01 bucket: one trade (101, vol 3).
	writeArchiveFixture(t, dir, "2026/06/16", false,
		tradeDocAt(1, 1, 100, 5, d.Add(10*time.Second)),
		tradeDocAt(2, 1, 99, 5, d.Add(20*time.Second)),
		tradeDocAt(3, 1, 102, 5, d.Add(40*time.Second)),
		tradeDocAt(4, 1, 101, 3, d.Add(60*time.Second)),
	)

	r := NewReader(NewCatalog(dir))
	got, err := r.ReadCandles(context.Background(), 1, time.Time{}, time.Time{}, 60, 100, nil)
	if err != nil {
		t.Fatalf("ReadCandles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 buckets, got %d: %+v", len(got), got)
	}
	// Newest-first: 10:01 then 10:00.
	if !got[0].Bucket.Equal(d.Add(time.Minute)) {
		t.Errorf("got[0].Bucket = %v, want 10:01", got[0].Bucket)
	}
	b0 := got[1] // the 10:00 bucket
	if b0.Open != 100 || b0.High != 102 || b0.Low != 99 || b0.Close != 102 || b0.Volume != 15 || b0.Count != 3 {
		t.Errorf("10:00 OHLCV wrong: %+v", b0)
	}
}

func TestReadCandlesBeforeAndLimit(t *testing.T) {
	dir := t.TempDir()
	d := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	writeArchiveFixture(t, dir, "2026/06/16", false,
		tradeDocAt(1, 1, 100, 5, d),
		tradeDocAt(2, 1, 101, 5, d.Add(time.Minute)),
		tradeDocAt(3, 1, 102, 5, d.Add(2*time.Minute)),
	)
	r := NewReader(NewCatalog(dir))

	// before excludes the 10:02 bucket (and newer).
	before := d.Add(2 * time.Minute)
	got, err := r.ReadCandles(context.Background(), 1, time.Time{}, time.Time{}, 60, 100, &before)
	if err != nil {
		t.Fatalf("ReadCandles before: %v", err)
	}
	if len(got) != 2 || !got[0].Bucket.Equal(d.Add(time.Minute)) {
		t.Fatalf("before: expected 2 buckets ending 10:01, got %d: %+v", len(got), got)
	}

	// limit keeps the newest bucket.
	got, err = r.ReadCandles(context.Background(), 1, time.Time{}, time.Time{}, 60, 1, nil)
	if err != nil {
		t.Fatalf("ReadCandles limit: %v", err)
	}
	if len(got) != 1 || !got[0].Bucket.Equal(d.Add(2*time.Minute)) {
		t.Errorf("limit: expected newest bucket 10:02, got %+v", got)
	}
}

// TestHistoryCandlesMerge composes live candles (>= split) with archive-derived
// candles (< split) into one newest-first series.
func TestHistoryCandlesMerge(t *testing.T) {
	dir := t.TempDir()
	// Archive holds 2026-06-16 -> split is 2026-06-17.
	dArch := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	writeArchiveFixture(t, dir, "2026/06/16", false,
		tradeDocAt(1, 1, 100, 5, dArch),
		tradeDocAt(2, 1, 105, 5, dArch.Add(30*time.Second)),
	)

	// Live candles on 06-18 and 06-19 (after the split).
	live := &fakeLive{candles: []persist.Candle{
		{Bucket: time.Date(2026, 6, 19, 9, 0, 0, 0, time.UTC), Open: 200, High: 201, Low: 199, Close: 200, Volume: 10, Count: 1},
		{Bucket: time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC), Open: 190, High: 191, Low: 189, Close: 190, Volume: 10, Count: 1},
	}}

	h := NewHistory(live, NewReader(NewCatalog(dir)), 2)
	h.now = func() time.Time { return time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC) }

	got, err := h.QueryCandles(context.Background(), persist.CandleFilter{SymbolLocate: 1, Interval: "1m", Limit: 100})
	if err != nil {
		t.Fatalf("QueryCandles: %v", err)
	}
	// Newest-first across the boundary: 06-19, 06-18 (live), then the 06-16 archive bucket.
	if len(got) != 3 {
		t.Fatalf("expected 3 merged candles, got %d: %+v", len(got), got)
	}
	wantDays := []time.Time{
		time.Date(2026, 6, 19, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC),
		dArch.Truncate(time.Minute),
	}
	for i, w := range wantDays {
		if !got[i].Bucket.Equal(w) {
			t.Errorf("got[%d].Bucket = %v, want %v", i, got[i].Bucket, w)
		}
	}
	// The archive bucket aggregates both trades (open 100, close 105, vol 10).
	arch := got[2]
	if arch.Open != 100 || arch.Close != 105 || arch.Volume != 10 {
		t.Errorf("archive candle wrong: %+v", arch)
	}
}

func TestHistoryCandlesPassthroughWhenDisabled(t *testing.T) {
	live := &fakeLive{candles: []persist.Candle{
		{Bucket: time.Date(2026, 6, 20, 9, 0, 0, 0, time.UTC), Close: 1},
	}}
	h := NewHistory(live, NewReader(NewCatalog("")), 2)
	got, err := h.QueryCandles(context.Background(), persist.CandleFilter{SymbolLocate: 1, Interval: "1m", Limit: 100})
	if err != nil {
		t.Fatalf("QueryCandles: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("passthrough candles = %d, want 1", len(got))
	}
}
