package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/archive"
	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

type symbolInfo struct {
	LocateCode uint16  `json:"locateCode"`
	Ticker     string  `json:"ticker"`
	Name       string  `json:"name"`
	Sector     string  `json:"sector"`
	Price      float64 `json:"price"`
	BestBid    float64 `json:"bestBid"`
	BestAsk    float64 `json:"bestAsk"`
	Spread     float64 `json:"spread"`
}

// handleSymbols returns all symbols with live prices and top-of-book.
func (s *Server) handleSymbols(w http.ResponseWriter, r *http.Request) {
	prices := s.market.AllPrices()
	out := make([]symbolInfo, 0, len(s.syms))

	for _, sym := range s.syms {
		si := symbolInfo{
			LocateCode: sym.LocateCode,
			Ticker:     sym.Ticker,
			Name:       sym.Name,
			Sector:     string(sym.Sector),
			Price:      prices[sym.LocateCode],
		}
		if sim, ok := s.books[sym.LocateCode]; ok {
			book := sim.Book()
			si.BestBid = book.BestBid()
			si.BestAsk = book.BestAsk()
			si.Spread = si.BestAsk - si.BestBid
		}
		out = append(out, si)
	}

	writeJSON(w, http.StatusOK, out)
}

// handleSymbolDetail returns a single symbol with live price and top-of-book.
func (s *Server) handleSymbolDetail(w http.ResponseWriter, r *http.Request) {
	ticker := r.PathValue("ticker")
	sym := s.resolveTicker(w, ticker)
	if sym == nil {
		return
	}

	price := s.market.Price(sym.LocateCode)
	si := symbolInfo{
		LocateCode: sym.LocateCode,
		Ticker:     sym.Ticker,
		Name:       sym.Name,
		Sector:     string(sym.Sector),
		Price:      price,
	}
	if sim, ok := s.books[sym.LocateCode]; ok {
		book := sim.Book()
		si.BestBid = book.BestBid()
		si.BestAsk = book.BestAsk()
		si.Spread = si.BestAsk - si.BestBid
	}

	writeJSON(w, http.StatusOK, si)
}

type depthResponse struct {
	Ticker   string      `json:"ticker"`
	Bids     []levelJSON `json:"bids"`
	Asks     []levelJSON `json:"asks"`
	BestBid  float64     `json:"bestBid"`
	BestAsk  float64     `json:"bestAsk"`
	MidPrice float64     `json:"midPrice"`
	Spread   float64     `json:"spread"`
}

type levelJSON struct {
	Price       float64 `json:"price"`
	Orders      int     `json:"orders"`
	TotalShares int32   `json:"totalShares"`
}

// handleBookDepth returns the order book depth for a symbol.
func (s *Server) handleBookDepth(w http.ResponseWriter, r *http.Request) {
	ticker := r.PathValue("ticker")
	sym := s.resolveTicker(w, ticker)
	if sym == nil {
		return
	}

	sim, ok := s.books[sym.LocateCode]
	if !ok {
		writeError(w, http.StatusNotFound, "no book for symbol: "+ticker)
		return
	}

	snap := sim.Book().Depth()

	resp := depthResponse{
		Ticker:   sym.Ticker,
		BestBid:  snap.BestBid,
		BestAsk:  snap.BestAsk,
		MidPrice: snap.MidPrice,
		Spread:   snap.Spread,
	}

	resp.Bids = make([]levelJSON, len(snap.Bids))
	for i, lvl := range snap.Bids {
		resp.Bids[i] = levelJSON{Price: lvl.Price, Orders: lvl.Orders, TotalShares: lvl.TotalShares}
	}
	resp.Asks = make([]levelJSON, len(snap.Asks))
	for i, lvl := range snap.Asks {
		resp.Asks[i] = levelJSON{Price: lvl.Price, Orders: lvl.Orders, TotalShares: lvl.TotalShares}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleTrades returns paginated trades from the database. The {ticker} path
// value may be a single symbol (fast path), a comma-separated list, or "*" for
// all symbols; multi-symbol results are ordered newest-first with a ticker
// tiebreak.
func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	ticker := r.PathValue("ticker")

	limit, err := parseIntParam(r, "limit", persist.DefaultLimit)
	if badRequest(w, err) {
		return
	}
	offset, err := parseIntParam(r, "offset", 0)
	if badRequest(w, err) {
		return
	}
	from, err := parseTimeParam(r, "from")
	if badRequest(w, err) {
		return
	}
	to, err := parseTimeParam(r, "to")
	if badRequest(w, err) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if isMultiTicker(ticker) {
		locates, ok := s.resolveTickers(w, ticker)
		if !ok {
			return
		}
		trades, err := s.reader.QueryTradesMulti(ctx, persist.MultiTradeFilter{
			Locates: locates,
			Limit:   persist.ClampLimit(limit),
			Offset:  max(offset, 0),
			From:    from,
			To:      to,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, trades)
		return
	}

	sym := s.resolveTicker(w, ticker)
	if sym == nil {
		return
	}

	trades, err := s.reader.QueryTrades(ctx, persist.TradeFilter{
		SymbolLocate: sym.LocateCode,
		Limit:        persist.ClampLimit(limit),
		Offset:       max(offset, 0),
		From:         from,
		To:           to,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, trades)
}

// handleCandles returns OHLCV bars for a symbol.
func (s *Server) handleCandles(w http.ResponseWriter, r *http.Request) {
	ticker := r.PathValue("ticker")
	sym := s.resolveTicker(w, ticker)
	if sym == nil {
		return
	}

	interval := r.URL.Query().Get("interval")
	if interval == "" {
		interval = "1m"
	} else if !persist.ValidInterval(interval) {
		writeError(w, http.StatusBadRequest, "invalid interval: "+interval)
		return
	}

	limit, err := parseIntParam(r, "limit", persist.DefaultLimit)
	if badRequest(w, err) {
		return
	}
	from, err := parseTimeParam(r, "from")
	if badRequest(w, err) {
		return
	}
	to, err := parseTimeParam(r, "to")
	if badRequest(w, err) {
		return
	}
	before, err := parseTimeParam(r, "before")
	if badRequest(w, err) {
		return
	}

	fill, err := parseFill(r)
	if badRequest(w, err) {
		return
	}

	clamped := persist.ClampLimit(limit)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	candles, err := s.reader.QueryCandles(ctx, persist.CandleFilter{
		SymbolLocate: sym.LocateCode,
		Interval:     interval,
		Limit:        clamped,
		From:         from,
		To:           to,
		Before:       before,
		Fill:         fill,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// A full page implies older buckets may remain: expose the oldest bucket as
	// the cursor for the next (older) page via ?before=.
	if len(candles) == clamped && clamped > 0 {
		oldest := candles[len(candles)-1].Bucket
		w.Header().Set("X-Next-Cursor", oldest.UTC().Format(time.RFC3339))
	}

	writeJSON(w, http.StatusOK, candles)
}

type statsResponse struct {
	Uptime        string  `json:"uptime"`
	Clients       int     `json:"clients"`
	Symbols       int     `json:"symbols"`
	TotalOrders   int     `json:"totalOrders"`
	TotalTrades   int64   `json:"totalTrades"`
	TotalVolume   int64   `json:"totalVolume"`
	DBSizeBytes   int64   `json:"dbSizeBytes"`
	DBTradesBytes int64   `json:"dbTradesBytes"`
	DBIndexBytes  int64   `json:"dbIndexBytes"`
	DBPctOf2GB    float64 `json:"dbPctOf2GB"`
	DBBudgetBytes int64   `json:"dbBudgetBytes"`
}

// handleStats returns runtime and aggregate statistics.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var totalOrders int
	for _, sim := range s.books {
		totalOrders += sim.Book().OrderCount()
	}

	ts, err := s.reader.QueryTradeStats(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := statsResponse{
		Uptime:        time.Since(s.startAt).Truncate(time.Second).String(),
		Clients:       s.mgr.ClientCount(),
		Symbols:       len(s.syms),
		TotalOrders:   totalOrders,
		TotalTrades:   ts.TotalTrades,
		TotalVolume:   ts.TotalVolume,
		DBBudgetBytes: persist.SizeBudgetBytes,
	}

	// DB size is best-effort: a size-query failure should not 500 the stats.
	if size, err := s.reader.QueryDBSize(ctx); err == nil {
		resp.DBSizeBytes = size.DatabaseBytes
		resp.DBTradesBytes = size.TradesBytes
		resp.DBIndexBytes = size.TradesIndexBytes
		resp.DBPctOf2GB = size.PctOfBudget()
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleHistoryMeta reports the available history: the live retention window and
// the archived (disk-limited) date span. Degrades to archive-disabled when the
// reader has no history layer.
func (s *Server) handleHistoryMeta(w http.ResponseWriter, r *http.Request) {
	prov, ok := s.reader.(historyMetaProvider)
	if !ok {
		writeJSON(w, http.StatusOK, archive.Meta{ArchiveEnabled: false})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	meta, err := prov.HistoryMeta(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

type healthResponse struct {
	Status      string  `json:"status"`
	Clients     int     `json:"clients"`
	Symbols     int     `json:"symbols"`
	DBSizeBytes int64   `json:"dbSizeBytes"`
	DBPctOf2GB  float64 `json:"dbPctOf2GB"`
}

// handleHealth reports liveness plus a cheap DB-size snapshot so operators can
// watch growth against the 2 GiB budget. It stays 200 even if the size probe
// fails (size fields are then zero).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := healthResponse{
		Status:  "ok",
		Clients: s.mgr.ClientCount(),
		Symbols: len(s.syms),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if size, err := s.reader.QueryDBSize(ctx); err == nil {
		resp.DBSizeBytes = size.DatabaseBytes
		resp.DBPctOf2GB = size.PctOfBudget()
	}

	writeJSON(w, http.StatusOK, resp)
}
