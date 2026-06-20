package archive

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

// writeArchiveFixture writes docs as a gzipped NDJSON day-file. When append is
// true and the file exists, a second gzip member is appended (exercising the
// multistream read path).
func writeArchiveFixture(t *testing.T, dir, day string, append bool, docs ...tradeDoc) {
	t.Helper()
	path := filepath.Join(dir, "trades", filepath.FromSlash(day)+fileExt)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if append {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	enc := json.NewEncoder(gz)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			t.Fatalf("encode fixture: %v", err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
}

func doc(mn int64, locate int16, ticker string, ts time.Time) tradeDoc {
	return tradeDoc{MatchNumber: mn, SymbolLocate: locate, Ticker: ticker, Price: 100, Shares: 10, Aggressor: "B", ExecutedAt: ts}
}

func TestReaderFiltersAndOrdersNewestFirst(t *testing.T) {
	dir := t.TempDir()
	d16 := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	d17 := time.Date(2026, 6, 17, 10, 0, 0, 0, time.UTC)
	// Day 16: locate 1 and a locate-2 noise row. Day 17: two locate-1 rows.
	writeArchiveFixture(t, dir, "2026/06/16", false,
		doc(1, 1, "NEXO", d16),
		doc(2, 2, "ACME", d16.Add(time.Minute)),
	)
	writeArchiveFixture(t, dir, "2026/06/17", false,
		doc(3, 1, "NEXO", d17),
		doc(4, 1, "NEXO", d17.Add(time.Minute)),
	)

	r := NewReader(NewCatalog(dir))
	got, err := r.Read(context.Background(), ReadFilter{SymbolLocate: 1, Limit: 100})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 locate-1 trades, got %d: %+v", len(got), got)
	}
	// Newest-first across days: match 4, 3 (day 17), then 1 (day 16).
	wantMN := []int64{4, 3, 1}
	for i, mn := range wantMN {
		if got[i].MatchNumber != mn {
			t.Errorf("got[%d].MatchNumber = %d, want %d", i, got[i].MatchNumber, mn)
		}
	}
	// Normalized to the API Trade shape (ticker carried, no symbol_locate field).
	if got[0].Ticker != "NEXO" {
		t.Errorf("ticker = %q", got[0].Ticker)
	}
}

func TestReaderTimeWindow(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	var docs []tradeDoc
	for i := 0; i < 10; i++ {
		docs = append(docs, doc(int64(i+1), 1, "NEXO", base.Add(time.Duration(i)*time.Hour)))
	}
	writeArchiveFixture(t, dir, "2026/06/16", false, docs...)

	r := NewReader(NewCatalog(dir))
	from := base.Add(3 * time.Hour)
	to := base.Add(6 * time.Hour)
	got, err := r.Read(context.Background(), ReadFilter{SymbolLocate: 1, From: from, To: to, Limit: 100})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Hours 3,4,5,6 inclusive -> 4 trades, newest-first (mn 7,6,5,4).
	if len(got) != 4 {
		t.Fatalf("expected 4 in window, got %d", len(got))
	}
	if got[0].MatchNumber != 7 || got[3].MatchNumber != 4 {
		t.Errorf("window bounds wrong: %d..%d", got[0].MatchNumber, got[3].MatchNumber)
	}
}

func TestReaderLimitKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	var docs []tradeDoc
	for i := 0; i < 100; i++ {
		docs = append(docs, doc(int64(i+1), 1, "NEXO", base.Add(time.Duration(i)*time.Minute)))
	}
	writeArchiveFixture(t, dir, "2026/06/16", false, docs...)

	r := NewReader(NewCatalog(dir))
	got, err := r.Read(context.Background(), ReadFilter{SymbolLocate: 1, Limit: 3})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	// Newest 3 are mn 100, 99, 98.
	if got[0].MatchNumber != 100 || got[1].MatchNumber != 99 || got[2].MatchNumber != 98 {
		t.Errorf("expected newest 3 (100,99,98), got %d,%d,%d", got[0].MatchNumber, got[1].MatchNumber, got[2].MatchNumber)
	}
}

func TestReaderMultistream(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	// Two appended gzip members in one day-file (as the archiver would on re-run).
	writeArchiveFixture(t, dir, "2026/06/16", false, doc(1, 1, "NEXO", base))
	writeArchiveFixture(t, dir, "2026/06/16", true, doc(2, 1, "NEXO", base.Add(time.Minute)))

	r := NewReader(NewCatalog(dir))
	got, err := r.Read(context.Background(), ReadFilter{SymbolLocate: 1, Limit: 100})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 trades across gzip members, got %d", len(got))
	}
}

func TestReaderDisabled(t *testing.T) {
	r := NewReader(NewCatalog(""))
	got, err := r.Read(context.Background(), ReadFilter{SymbolLocate: 1, Limit: 100})
	if err != nil || len(got) != 0 {
		t.Errorf("disabled reader: got %d err=%v", len(got), err)
	}
}

func TestTailRingBuffer(t *testing.T) {
	ts := time.Now()
	tl := newTail(3)
	for i := 1; i <= 7; i++ {
		tl.push(persist.Trade{MatchNumber: int64(i), ExecutedAt: ts})
	}
	// Only the newest 3 retained (5,6,7), newest-first.
	out := tl.newestFirst()
	if len(out) != 3 {
		t.Fatalf("expected 3 retained, got %d", len(out))
	}
	want := []int64{7, 6, 5}
	for i, w := range want {
		if out[i].MatchNumber != w {
			t.Errorf("tail[%d] = %d, want %d", i, out[i].MatchNumber, w)
		}
	}

	// Zero-capacity tail retains nothing and never panics.
	z := newTail(0)
	z.push(persist.Trade{MatchNumber: 1, ExecutedAt: ts})
	if len(z.newestFirst()) != 0 {
		t.Error("zero-cap tail should retain nothing")
	}
}
