package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

// badRequest writes a 400 with the error message and reports whether err was
// non-nil, so callers can `if badRequest(w, err) { return }`.
func badRequest(w http.ResponseWriter, err error) bool {
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return true
	}
	return false
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

// isMultiTicker reports whether a {ticker} path value selects multiple symbols:
// the wildcard "*" or a comma-separated list.
func isMultiTicker(ticker string) bool {
	return ticker == "*" || strings.Contains(ticker, ",")
}

// resolveTickers resolves a multi-symbol selector ("*" or "A,B,C") to locate
// codes. On an unknown ticker it writes a 404 and returns ok=false; on an empty
// selection it writes a 400. Duplicates are collapsed.
func (s *Server) resolveTickers(w http.ResponseWriter, selector string) (locates []uint16, ok bool) {
	if selector == "*" {
		out := make([]uint16, len(s.syms))
		for i := range s.syms {
			out[i] = s.syms[i].LocateCode
		}
		return out, true
	}

	seen := make(map[uint16]struct{})
	for _, part := range strings.Split(selector, ",") {
		t := strings.TrimSpace(part)
		if t == "" {
			continue
		}
		sym, found := s.byTick[t]
		if !found {
			writeError(w, http.StatusNotFound, "symbol not found: "+t)
			return nil, false
		}
		if _, dup := seen[sym.LocateCode]; dup {
			continue
		}
		seen[sym.LocateCode] = struct{}{}
		locates = append(locates, sym.LocateCode)
	}

	if len(locates) == 0 {
		writeError(w, http.StatusBadRequest, "no valid tickers in selector")
		return nil, false
	}
	return locates, true
}

// parseIntParam parses an integer query parameter. An absent parameter yields
// def with no error; a present-but-malformed parameter yields an error so the
// caller can reject the request with 400 instead of silently using the default.
func parseIntParam(r *http.Request, key string, def int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %q is not an integer", key, v)
	}
	return n, nil
}

// parseFill parses the optional `fill` query parameter for candle queries.
// "zero" enables zero-volume gap filling; "" or "none" disables it; anything
// else is rejected so typos surface as 400 rather than silently disabling fill.
func parseFill(r *http.Request) (bool, error) {
	switch v := r.URL.Query().Get("fill"); v {
	case "", "none":
		return false, nil
	case "zero":
		return true, nil
	default:
		return false, fmt.Errorf("invalid fill: %q (want \"zero\" or \"none\")", v)
	}
}

// parseTimeParam parses an RFC3339 query parameter. An absent parameter yields
// nil with no error; a present-but-malformed parameter yields an error so the
// caller can reject the request with 400 instead of silently ignoring it.
func parseTimeParam(r *http.Request, key string) (*time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %q is not an RFC3339 timestamp", key, v)
	}
	return &t, nil
}
