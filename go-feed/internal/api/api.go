package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/engine"
	"github.com/ndrandal/feed-simulator/go-feed/internal/orderbook"
	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
	"github.com/ndrandal/feed-simulator/go-feed/internal/session"
	"github.com/ndrandal/feed-simulator/go-feed/internal/symbol"
)

// Server provides REST API endpoints for the simulator.
type Server struct {
	reader  persist.TradeReader
	market  *engine.MarketEngine
	books   map[uint16]*orderbook.Simulator
	mgr     *session.Manager
	syms    []symbol.Symbol
	byTick  map[string]*symbol.Symbol
	startAt time.Time
}

// NewServer creates a new API server.
func NewServer(reader persist.TradeReader, market *engine.MarketEngine, books map[uint16]*orderbook.Simulator, mgr *session.Manager, syms []symbol.Symbol) *Server {
	byTick := make(map[string]*symbol.Symbol, len(syms))
	for i := range syms {
		byTick[syms[i].Ticker] = &syms[i]
	}
	return &Server{
		reader:  reader,
		market:  market,
		books:   books,
		mgr:     mgr,
		syms:    syms,
		byTick:  byTick,
		startAt: time.Now(),
	}
}

// Register attaches API routes to the given mux.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/symbols", s.handleSymbols)
	mux.HandleFunc("GET /api/symbols/{ticker}", s.handleSymbolDetail)
	mux.HandleFunc("GET /api/book/{ticker}", s.handleBookDepth)
	mux.HandleFunc("GET /api/trades/{ticker}", s.handleTrades)
	mux.HandleFunc("GET /api/candles/{ticker}", s.handleCandles)
	mux.HandleFunc("GET /api/stats", s.handleStats)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// resolveTicker looks up a symbol by ticker, writing a 404 if not found.
// Returns nil if the symbol was not found (error already written).
func (s *Server) resolveTicker(w http.ResponseWriter, ticker string) *symbol.Symbol {
	sym, ok := s.byTick[ticker]
	if !ok {
		writeError(w, http.StatusNotFound, "symbol not found: "+ticker)
		return nil
	}
	return sym
}

// parseIntParam parses an integer query parameter with a default value.
func parseIntParam(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// parseTimeParam parses an RFC3339 query parameter.
func parseTimeParam(r *http.Request, key string) *time.Time {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil
	}
	return &t
}
