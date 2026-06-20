package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	// capture filter args for assertions
	lastTradeFilter  persist.TradeFilter
	lastCandleFilter persist.CandleFilter
}

func (s *stubTradeReader) QueryTrades(_ context.Context, f persist.TradeFilter) ([]persist.Trade, error) {
	s.lastTradeFilter = f
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
