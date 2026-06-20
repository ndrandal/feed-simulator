package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/archive"
	"github.com/ndrandal/feed-simulator/go-feed/internal/engine"
	"github.com/ndrandal/feed-simulator/go-feed/internal/orderbook"
	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
	"github.com/ndrandal/feed-simulator/go-feed/internal/session"
	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

// --- stub TradeReader ---

type stubTradeReader struct {
	trades     []persist.Trade
	tradesErr  error
	candles    []persist.Candle
	candlesErr error
	stats      persist.TradeStats
	statsErr   error
	dbSize     persist.DBSize
	dbSizeErr  error

	// capture filter args for assertions
	lastTradeFilter  persist.TradeFilter
	lastMultiFilter  persist.MultiTradeFilter
	lastCandleFilter persist.CandleFilter
}

func (s *stubTradeReader) QueryDBSize(_ context.Context) (persist.DBSize, error) {
	return s.dbSize, s.dbSizeErr
}

func (s *stubTradeReader) QueryTrades(_ context.Context, f persist.TradeFilter) ([]persist.Trade, error) {
	s.lastTradeFilter = f
	return s.trades, s.tradesErr
}

func (s *stubTradeReader) QueryTradesMulti(_ context.Context, f persist.MultiTradeFilter) ([]persist.Trade, error) {
	s.lastMultiFilter = f
	return s.trades, s.tradesErr
}

func (s *stubTradeReader) QueryCandles(_ context.Context, f persist.CandleFilter) ([]persist.Candle, error) {
	s.lastCandleFilter = f
	return s.candles, s.candlesErr
}

func (s *stubTradeReader) QueryTradeStats(_ context.Context) (persist.TradeStats, error) {
	return s.stats, s.statsErr
}

// --- test helpers ---

// newTestServer creates a Server with real MarketEngine and one initialized orderbook (NEXO, locate=1).
func newTestServer(stub *stubTradeReader) (*Server, *http.ServeMux) {
	syms := symbol.AllSymbols()
	rng := engine.NewRNG(42)
	market := engine.NewMarketEngine(rng, syms)

	// Only initialize one book (NEXO, locate=1) to keep tests fast.
	nexoBook := orderbook.NewBook(1, 0.01)
	nexoSim := orderbook.NewSimulator(rng, nexoBook, 1, 0.01)
	nexoSim.Initialize(185.00)

	books := map[uint16]*orderbook.Simulator{
		1: nexoSim,
	}

	mgr := session.NewManager(syms, 64)
	srv := NewServer(stub, market, books, mgr, syms)

	mux := http.NewServeMux()
	srv.Register(mux)
	return srv, mux
}

func mustDecodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}
}

// --- tests ---

func TestHandleSymbols(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/symbols", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out []map[string]any
	mustDecodeJSON(t, w.Result(), &out)

	if len(out) != 30 {
		t.Fatalf("expected 30 symbols, got %d", len(out))
	}

	first := out[0]
	for _, key := range []string{"ticker", "price", "bestBid", "bestAsk"} {
		if _, ok := first[key]; !ok {
			t.Errorf("missing key %q in symbol JSON", key)
		}
	}
}

func TestHandleSymbolDetail(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/symbols/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)

	if out["ticker"] != "NEXO" {
		t.Errorf("expected ticker NEXO, got %v", out["ticker"])
	}
	if out["locateCode"] != float64(1) {
		t.Errorf("expected locateCode 1, got %v", out["locateCode"])
	}
	if _, ok := out["price"]; !ok {
		t.Error("missing price field")
	}
}

func TestHandleSymbolDetailNotFound(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/symbols/ZZZZ", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}

	var out map[string]string
	mustDecodeJSON(t, w.Result(), &out)

	if out["error"] == "" {
		t.Error("expected error message in response")
	}
}

func TestHandleBookDepth(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/book/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)

	for _, key := range []string{"bids", "asks", "midPrice", "spread"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing key %q in depth response", key)
		}
	}
}

func TestHandleBookDepthNotFound(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/book/ZZZZ", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleTrades(t *testing.T) {
	stub := &stubTradeReader{
		trades: []persist.Trade{
			{MatchNumber: 1, Ticker: "NEXO", Price: 185.50, Shares: 100, Aggressor: "B", ExecutedAt: time.Now()},
			{MatchNumber: 2, Ticker: "NEXO", Price: 185.60, Shares: 200, Aggressor: "S", ExecutedAt: time.Now()},
		},
	}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/trades/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out []persist.Trade
	mustDecodeJSON(t, w.Result(), &out)

	if len(out) != 2 {
		t.Fatalf("expected 2 trades, got %d", len(out))
	}
}

func TestHandleTradesNotFound(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/trades/ZZZZ", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleTradesParams(t *testing.T) {
	stub := &stubTradeReader{trades: []persist.Trade{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/trades/NEXO?limit=5&offset=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if stub.lastTradeFilter.Limit != 5 {
		t.Errorf("expected limit=5, got %d", stub.lastTradeFilter.Limit)
	}
	if stub.lastTradeFilter.Offset != 10 {
		t.Errorf("expected offset=10, got %d", stub.lastTradeFilter.Offset)
	}
	if stub.lastTradeFilter.SymbolLocate != 1 {
		t.Errorf("expected symbolLocate=1, got %d", stub.lastTradeFilter.SymbolLocate)
	}
}

func TestHandleTradesDBError(t *testing.T) {
	stub := &stubTradeReader{tradesErr: errors.New("db connection lost")}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/trades/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHandleTradesMultiList(t *testing.T) {
	stub := &stubTradeReader{trades: []persist.Trade{
		{MatchNumber: 1, Ticker: "NEXO", Price: 185, Shares: 100, Aggressor: "B", ExecutedAt: time.Now()},
	}}
	_, mux := newTestServer(stub)
	// NEXO=locate 1; pick a second known ticker dynamically.
	syms := symbol.AllSymbols()
	second := syms[1].Ticker
	req := httptest.NewRequest("GET", "/api/trades/NEXO,"+second+"?limit=50", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(stub.lastMultiFilter.Locates) != 2 {
		t.Errorf("expected 2 locates, got %v", stub.lastMultiFilter.Locates)
	}
	if stub.lastMultiFilter.Limit != 50 {
		t.Errorf("expected limit 50, got %d", stub.lastMultiFilter.Limit)
	}
	// Single-symbol fast path must not be taken.
	if stub.lastTradeFilter.SymbolLocate != 0 {
		t.Error("multi request should not hit single-symbol QueryTrades")
	}
}

func TestHandleTradesWildcard(t *testing.T) {
	stub := &stubTradeReader{trades: []persist.Trade{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/trades/*", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(stub.lastMultiFilter.Locates) != len(symbol.AllSymbols()) {
		t.Errorf("wildcard should select all %d symbols, got %d", len(symbol.AllSymbols()), len(stub.lastMultiFilter.Locates))
	}
}

func TestHandleTradesMultiUnknown(t *testing.T) {
	stub := &stubTradeReader{}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/trades/NEXO,ZZZZ", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown ticker in list, got %d", w.Code)
	}
}

func TestHandleTradesMultiDedupe(t *testing.T) {
	stub := &stubTradeReader{trades: []persist.Trade{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/trades/NEXO,NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if len(stub.lastMultiFilter.Locates) != 1 {
		t.Errorf("expected deduped to 1 locate, got %v", stub.lastMultiFilter.Locates)
	}
}

func TestHandleCandles(t *testing.T) {
	stub := &stubTradeReader{
		candles: []persist.Candle{
			{Bucket: time.Now(), Open: 185.0, High: 186.0, Low: 184.0, Close: 185.5, Volume: 1000, Count: 10},
		},
	}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out []persist.Candle
	mustDecodeJSON(t, w.Result(), &out)

	if len(out) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(out))
	}
}

func TestHandleCandlesNotFound(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/candles/ZZZZ", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleCandlesDefaultInterval(t *testing.T) {
	stub := &stubTradeReader{candles: []persist.Candle{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if stub.lastCandleFilter.Interval != "1m" {
		t.Errorf("expected default interval 1m, got %q", stub.lastCandleFilter.Interval)
	}
}

func TestHandleCandlesCustomInterval(t *testing.T) {
	stub := &stubTradeReader{candles: []persist.Candle{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO?interval=5m", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if stub.lastCandleFilter.Interval != "5m" {
		t.Errorf("expected interval 5m, got %q", stub.lastCandleFilter.Interval)
	}
}

func TestHandleCandlesDBError(t *testing.T) {
	stub := &stubTradeReader{candlesErr: errors.New("unsupported interval: 99x")}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleCandlesPaginationParams(t *testing.T) {
	stub := &stubTradeReader{candles: []persist.Candle{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO?before=2025-01-15T10:00:00Z&fill=zero", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stub.lastCandleFilter.Before == nil {
		t.Error("expected Before cursor to be passed through")
	}
	if !stub.lastCandleFilter.Fill {
		t.Error("expected Fill=true for fill=zero")
	}
}

func TestHandleCandlesBadFill(t *testing.T) {
	stub := &stubTradeReader{candles: []persist.Candle{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO?fill=bogus", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad fill, got %d", w.Code)
	}
}

func TestHandleCandlesNextCursor(t *testing.T) {
	oldest := time.Date(2025, 1, 15, 10, 28, 0, 0, time.UTC)
	// A full page (len == limit) sets the cursor to the oldest bucket.
	stub := &stubTradeReader{candles: []persist.Candle{
		{Bucket: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)},
		{Bucket: oldest},
	}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO?limit=2", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got := w.Header().Get("X-Next-Cursor"); got != oldest.Format(time.RFC3339) {
		t.Errorf("X-Next-Cursor = %q, want %q", got, oldest.Format(time.RFC3339))
	}

	// A partial page (len < limit) sets no cursor.
	stub2 := &stubTradeReader{candles: []persist.Candle{{Bucket: oldest}}}
	_, mux2 := newTestServer(stub2)
	req2 := httptest.NewRequest("GET", "/api/candles/NEXO?limit=10", nil)
	w2 := httptest.NewRecorder()
	mux2.ServeHTTP(w2, req2)
	if got := w2.Header().Get("X-Next-Cursor"); got != "" {
		t.Errorf("expected no cursor on partial page, got %q", got)
	}
}

func TestHandleStats(t *testing.T) {
	stub := &stubTradeReader{
		stats: persist.TradeStats{TotalTrades: 42, TotalVolume: 10000},
	}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)

	for _, key := range []string{"uptime", "clients", "symbols", "totalOrders", "totalTrades", "totalVolume"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing key %q in stats response", key)
		}
	}

	if out["totalTrades"] != float64(42) {
		t.Errorf("expected totalTrades=42, got %v", out["totalTrades"])
	}
	if out["totalVolume"] != float64(10000) {
		t.Errorf("expected totalVolume=10000, got %v", out["totalVolume"])
	}
}

func TestHandleStatsDBSize(t *testing.T) {
	stub := &stubTradeReader{
		stats:  persist.TradeStats{TotalTrades: 5, TotalVolume: 50},
		dbSize: persist.DBSize{DatabaseBytes: persist.SizeBudgetBytes / 4, TradesBytes: 100, TradesIndexBytes: 20},
	}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)
	if out["dbSizeBytes"] != float64(persist.SizeBudgetBytes/4) {
		t.Errorf("dbSizeBytes = %v", out["dbSizeBytes"])
	}
	if out["dbPctOf2GB"] != float64(25) {
		t.Errorf("dbPctOf2GB = %v, want 25", out["dbPctOf2GB"])
	}
	if out["dbBudgetBytes"] != float64(persist.SizeBudgetBytes) {
		t.Errorf("dbBudgetBytes = %v", out["dbBudgetBytes"])
	}
}

func TestHandleStatsDBSizeBestEffort(t *testing.T) {
	// A size-probe failure must not fail the stats response.
	stub := &stubTradeReader{
		stats:     persist.TradeStats{TotalTrades: 1},
		dbSizeErr: errors.New("size probe failed"),
	}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 despite size error, got %d", w.Code)
	}
	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)
	if out["dbSizeBytes"] != float64(0) {
		t.Errorf("expected 0 dbSizeBytes on probe error, got %v", out["dbSizeBytes"])
	}
}

func TestHandleHistoryMetaNoProvider(t *testing.T) {
	// A plain reader (no history layer) reports archive disabled, not an error.
	_, mux := newTestServer(&stubTradeReader{})
	req := httptest.NewRequest("GET", "/api/history/meta", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)
	if out["archiveEnabled"] != false {
		t.Errorf("archiveEnabled = %v, want false", out["archiveEnabled"])
	}
}

func TestHandleHistoryMetaWithHistory(t *testing.T) {
	// Wrap the stub in a real History over an archive fixture dir and confirm the
	// endpoint surfaces archive bounds.
	dir := t.TempDir()
	tradesDir := filepath.Join(dir, "trades", "2026", "06")
	if err := os.MkdirAll(tradesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, day := range []string{"16", "18"} {
		if err := os.WriteFile(filepath.Join(tradesDir, day+".jsonl.gz"), []byte{}, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	hist := archive.NewHistory(&stubTradeReader{}, archive.NewReader(archive.NewCatalog(dir)), 2)
	srv := NewServer(hist, nil, nil, session.NewManager(symbol.AllSymbols(), 64), symbol.AllSymbols())
	mux := http.NewServeMux()
	srv.Register(mux)

	req := httptest.NewRequest("GET", "/api/history/meta", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)
	if out["archiveEnabled"] != true {
		t.Errorf("archiveEnabled = %v, want true", out["archiveEnabled"])
	}
	if out["retentionDays"] != float64(2) {
		t.Errorf("retentionDays = %v, want 2", out["retentionDays"])
	}
	if _, ok := out["archiveMinDay"]; !ok {
		t.Error("expected archiveMinDay in response")
	}
}

func TestHandleHealth(t *testing.T) {
	stub := &stubTradeReader{dbSize: persist.DBSize{DatabaseBytes: persist.SizeBudgetBytes / 2}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var out map[string]any
	mustDecodeJSON(t, w.Result(), &out)
	if out["status"] != "ok" {
		t.Errorf("status = %v", out["status"])
	}
	if out["dbPctOf2GB"] != float64(50) {
		t.Errorf("dbPctOf2GB = %v, want 50", out["dbPctOf2GB"])
	}
}

func TestHandleHealthBestEffort(t *testing.T) {
	// Health stays 200 even when the DB-size probe errors.
	stub := &stubTradeReader{dbSizeErr: errors.New("db down")}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleStatsDBError(t *testing.T) {
	stub := &stubTradeReader{statsErr: errors.New("db down")}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestContentTypeJSON(t *testing.T) {
	_, mux := newTestServer(&stubTradeReader{
		stats: persist.TradeStats{},
	})

	endpoints := []string{
		"/api/symbols",
		"/api/symbols/NEXO",
		"/api/book/NEXO",
		"/api/stats",
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest("GET", ep, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		ct := w.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("%s: expected Content-Type application/json, got %q", ep, ct)
		}
	}
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		url     string
		key     string
		def     int
		want    int
		wantErr bool
	}{
		{"/test", "limit", 100, 100, false},         // missing → default
		{"/test?limit=50", "limit", 100, 50, false}, // valid int
		{"/test?limit=0", "limit", 100, 0, false},   // zero is a valid int (clamping happens later)
		{"/test?limit=-5", "limit", 100, -5, false}, // negative is a valid int
		{"/test?limit=abc", "limit", 100, 0, true},  // malformed → error
		{"/test?limit=1.5", "limit", 100, 0, true},  // malformed → error
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.url, nil)
		got, err := parseIntParam(req, tt.key, tt.def)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseIntParam(%q): err = %v, wantErr %v", tt.url, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("parseIntParam(%q, %q, %d) = %d, want %d", tt.url, tt.key, tt.def, got, tt.want)
		}
	}
}

func TestParseTimeParam(t *testing.T) {
	// empty → nil, no error
	req := httptest.NewRequest("GET", "/test", nil)
	got, err := parseTimeParam(req, "from")
	if err != nil || got != nil {
		t.Errorf("expected (nil,nil) for empty param, got (%v,%v)", got, err)
	}

	// bad format → error
	req = httptest.NewRequest("GET", "/test?from=not-a-time", nil)
	if _, err := parseTimeParam(req, "from"); err == nil {
		t.Error("expected error for malformed time, got nil")
	}

	// valid RFC3339
	ts := "2025-01-15T10:30:00Z"
	req = httptest.NewRequest("GET", "/test?from="+ts, nil)
	got, err = parseTimeParam(req, "from")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil time")
	}
	expected, _ := time.Parse(time.RFC3339, ts)
	if !got.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, *got)
	}
}

// TestHandleTradesLimitClamp verifies the documented clamp semantics on the
// trades endpoint: oversized limits clamp to MaxLimit, non-positive limits fall
// back to DefaultLimit, and negative offsets floor at zero.
func TestHandleTradesLimitClamp(t *testing.T) {
	tests := []struct {
		query     string
		wantLimit int
		wantOff   int
	}{
		{"", persist.DefaultLimit, 0},
		{"?limit=5000", persist.MaxLimit, 0},
		{"?limit=0", persist.DefaultLimit, 0},
		{"?limit=-3", persist.DefaultLimit, 0},
		{"?limit=250&offset=40", 250, 40},
		{"?offset=-9", persist.DefaultLimit, 0},
	}
	for _, tt := range tests {
		stub := &stubTradeReader{trades: []persist.Trade{}}
		_, mux := newTestServer(stub)
		req := httptest.NewRequest("GET", "/api/trades/NEXO"+tt.query, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%q: expected 200, got %d", tt.query, w.Code)
		}
		if stub.lastTradeFilter.Limit != tt.wantLimit {
			t.Errorf("%q: limit = %d, want %d", tt.query, stub.lastTradeFilter.Limit, tt.wantLimit)
		}
		if stub.lastTradeFilter.Offset != tt.wantOff {
			t.Errorf("%q: offset = %d, want %d", tt.query, stub.lastTradeFilter.Offset, tt.wantOff)
		}
	}
}

// TestHandleTradesBadParams verifies malformed params are rejected with 400
// rather than silently ignored.
func TestHandleTradesBadParams(t *testing.T) {
	for _, q := range []string{"?limit=abc", "?offset=xyz", "?from=not-a-time", "?to=2025-13-99"} {
		stub := &stubTradeReader{trades: []persist.Trade{}}
		_, mux := newTestServer(stub)
		req := httptest.NewRequest("GET", "/api/trades/NEXO"+q, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%q: expected 400, got %d", q, w.Code)
		}
	}
}

func TestHandleCandlesLimitClamp(t *testing.T) {
	stub := &stubTradeReader{candles: []persist.Candle{}}
	_, mux := newTestServer(stub)
	req := httptest.NewRequest("GET", "/api/candles/NEXO?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if stub.lastCandleFilter.Limit != persist.MaxLimit {
		t.Errorf("limit = %d, want %d", stub.lastCandleFilter.Limit, persist.MaxLimit)
	}
}

// TestHandleCandlesBadParams verifies a bad interval is rejected at the handler
// (400) and malformed time/limit params are too.
func TestHandleCandlesBadParams(t *testing.T) {
	for _, q := range []string{"?interval=99x", "?limit=abc", "?from=nope"} {
		stub := &stubTradeReader{candles: []persist.Candle{}}
		_, mux := newTestServer(stub)
		req := httptest.NewRequest("GET", "/api/candles/NEXO"+q, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%q: expected 400, got %d", q, w.Code)
		}
	}
}
