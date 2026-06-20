package archive

import (
	"context"
	"testing"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

// fakeLive is a persist.TradeReader whose QueryTrades filters an in-memory,
// newest-first trade slice (the "live" trades, i.e. within retention).
type fakeLive struct {
	trades   []persist.Trade  // newest-first
	candles  []persist.Candle // newest-first
	lastFrom *time.Time
	queried  bool
}

func (f *fakeLive) QueryTrades(_ context.Context, flt persist.TradeFilter) ([]persist.Trade, error) {
	f.queried = true
	f.lastFrom = flt.From
	out := []persist.Trade{}
	for _, t := range f.trades {
		if flt.From != nil && t.ExecutedAt.Before(*flt.From) {
			continue
		}
		if flt.To != nil && t.ExecutedAt.After(*flt.To) {
			continue
		}
		out = append(out, t)
	}
	lim := persist.ClampLimit(flt.Limit)
	off := flt.Offset
	if off < 0 {
		off = 0
	}
	if off >= len(out) {
		return []persist.Trade{}, nil
	}
	end := off + lim
	if end > len(out) {
		end = len(out)
	}
	return out[off:end], nil
}

func (f *fakeLive) QueryTradesMulti(context.Context, persist.MultiTradeFilter) ([]persist.Trade, error) {
	return nil, nil
}
func (f *fakeLive) QueryCandles(_ context.Context, flt persist.CandleFilter) ([]persist.Candle, error) {
	out := []persist.Candle{}
	for _, c := range f.candles {
		if flt.From != nil && c.Bucket.Before(*flt.From) {
			continue
		}
		if flt.To != nil && c.Bucket.After(*flt.To) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}
func (f *fakeLive) QueryTradeStats(context.Context) (persist.TradeStats, error) {
	return persist.TradeStats{}, nil
}
func (f *fakeLive) QueryDBSize(context.Context) (persist.DBSize, error) {
	return persist.DBSize{}, nil
}

func tr(mn int64, ts time.Time) persist.Trade {
	return persist.Trade{MatchNumber: mn, Ticker: "NEXO", Price: 100, Shares: 10, Aggressor: "B", ExecutedAt: ts}
}

func mnSeq(trades []persist.Trade) []int64 {
	out := make([]int64, len(trades))
	for i, t := range trades {
		out[i] = t.MatchNumber
	}
	return out
}

func eqSeq(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// historyFixture builds a History with fixed now (2026-06-20T12:00Z), retention
// 2 days (cutoff 2026-06-18T12:00Z), two live trades after the cutoff, and three
// archived trades before it.
func historyFixture(t *testing.T) (*History, *fakeLive) {
	t.Helper()
	now := time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC)

	live := &fakeLive{trades: []persist.Trade{
		tr(101, time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)),
		tr(100, time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)),
	}}

	dir := t.TempDir()
	writeArchiveFixture(t, dir, "2026/06/16", false,
		doc(50, 1, "NEXO", time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)),
		doc(51, 1, "NEXO", time.Date(2026, 6, 16, 11, 0, 0, 0, time.UTC)),
	)
	writeArchiveFixture(t, dir, "2026/06/15", false,
		doc(40, 1, "NEXO", time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)),
	)

	h := NewHistory(live, NewReader(NewCatalog(dir)), 2)
	h.now = func() time.Time { return now }
	return h, live
}

func TestHistoryLiveOnly(t *testing.T) {
	h, live := historyFixture(t)
	got, err := h.QueryTrades(context.Background(), persist.TradeFilter{SymbolLocate: 1, Limit: 2})
	if err != nil {
		t.Fatalf("QueryTrades: %v", err)
	}
	if !eqSeq(mnSeq(got), []int64{101, 100}) {
		t.Errorf("live-only page = %v, want [101 100]", mnSeq(got))
	}
	// Live query was bounded at the cutoff.
	cutoff := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	if live.lastFrom == nil || !live.lastFrom.Equal(cutoff) {
		t.Errorf("live From = %v, want cutoff %v", live.lastFrom, cutoff)
	}
}

func TestHistoryFallthroughMerge(t *testing.T) {
	h, _ := historyFixture(t)
	got, err := h.QueryTrades(context.Background(), persist.TradeFilter{SymbolLocate: 1, Limit: 5})
	if err != nil {
		t.Fatalf("QueryTrades: %v", err)
	}
	// 2 live (newest) then 3 archived (older), newest-first.
	if !eqSeq(mnSeq(got), []int64{101, 100, 51, 50, 40}) {
		t.Errorf("merged page = %v, want [101 100 51 50 40]", mnSeq(got))
	}
}

func TestHistoryArchiveOnly(t *testing.T) {
	h, live := historyFixture(t)
	to := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC) // entirely before the cutoff
	got, err := h.QueryTrades(context.Background(), persist.TradeFilter{SymbolLocate: 1, To: &to, Limit: 2})
	if err != nil {
		t.Fatalf("QueryTrades: %v", err)
	}
	if !eqSeq(mnSeq(got), []int64{51, 50}) {
		t.Errorf("archive-only page = %v, want [51 50]", mnSeq(got))
	}
	if live.queried {
		t.Error("live reader should not be queried for an archive-only range")
	}
}

func TestHistoryOffsetAcrossBoundary(t *testing.T) {
	h, _ := historyFixture(t)
	got, err := h.QueryTrades(context.Background(), persist.TradeFilter{SymbolLocate: 1, Limit: 3, Offset: 1})
	if err != nil {
		t.Fatalf("QueryTrades: %v", err)
	}
	// Full newest-first sequence is [101 100 51 50 40]; offset 1, limit 3.
	if !eqSeq(mnSeq(got), []int64{100, 51, 50}) {
		t.Errorf("offset page = %v, want [100 51 50]", mnSeq(got))
	}
}

func TestHistoryPassthroughWhenArchiveDisabled(t *testing.T) {
	live := &fakeLive{trades: []persist.Trade{
		tr(2, time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)),
		tr(1, time.Date(2026, 6, 19, 10, 0, 0, 0, time.UTC)),
	}}
	h := NewHistory(live, NewReader(NewCatalog("")), 2) // disabled archive
	got, err := h.QueryTrades(context.Background(), persist.TradeFilter{SymbolLocate: 1, Limit: 10})
	if err != nil {
		t.Fatalf("QueryTrades: %v", err)
	}
	if !eqSeq(mnSeq(got), []int64{2, 1}) {
		t.Errorf("passthrough page = %v, want [2 1]", mnSeq(got))
	}
}
