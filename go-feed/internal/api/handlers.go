package api

import (
	"context"
	"net/http"
	"time"

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

// handleTrades returns paginated trades for a symbol from the database.
func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	ticker := r.PathValue("ticker")
	sym := s.resolveTicker(w, ticker)
	if sym == nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	trades, err := s.reader.QueryTrades(ctx, persist.TradeFilter{
		SymbolLocate: sym.LocateCode,
		Limit:        parseIntParam(r, "limit", 100),
		Offset:       parseIntParam(r, "offset", 0),
		From:         parseTimeParam(r, "from"),
		To:           parseTimeParam(r, "to"),
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
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	candles, err := s.reader.QueryCandles(ctx, persist.CandleFilter{
		SymbolLocate: sym.LocateCode,
		Interval:     interval,
		Limit:        parseIntParam(r, "limit", 100),
		From:         parseTimeParam(r, "from"),
		To:           parseTimeParam(r, "to"),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, candles)
}

type statsResponse struct {
	Uptime      string `json:"uptime"`
	Clients     int    `json:"clients"`
	Symbols     int    `json:"symbols"`
	TotalOrders int    `json:"totalOrders"`
	TotalTrades int64  `json:"totalTrades"`
	TotalVolume int64  `json:"totalVolume"`
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

	writeJSON(w, http.StatusOK, statsResponse{
		Uptime:      time.Since(s.startAt).Truncate(time.Second).String(),
		Clients:     s.mgr.ClientCount(),
		Symbols:     len(s.syms),
		TotalOrders: totalOrders,
		TotalTrades: ts.TotalTrades,
		TotalVolume: ts.TotalVolume,
	})
}
