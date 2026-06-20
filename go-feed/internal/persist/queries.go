package persist

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Trade represents a persisted trade document.
type Trade struct {
	MatchNumber int64     `json:"matchNumber"`
	Ticker      string    `json:"ticker"`
	Price       float64   `json:"price"`
	Shares      int32     `json:"shares"`
	Aggressor   string    `json:"aggressor"`
	ExecutedAt  time.Time `json:"executedAt"`
}

// TradeFilter controls which trades to return.
type TradeFilter struct {
	SymbolLocate uint16
	Limit        int
	Offset       int
	From         *time.Time
	To           *time.Time
}

// Candle represents an OHLCV bar.
type Candle struct {
	Bucket time.Time `json:"t"`
	Open   float64   `json:"o"`
	High   float64   `json:"h"`
	Low    float64   `json:"l"`
	Close  float64   `json:"c"`
	Volume int64     `json:"v"`
	Count  int64     `json:"n"`
}

// CandleFilter controls candle query parameters.
type CandleFilter struct {
	SymbolLocate uint16
	Interval     string // "1m","5m","15m","1h","4h","1d"
	Limit        int
	From         *time.Time
	To           *time.Time
	// Before is an exclusive upper-bound cursor for newest-first pagination:
	// only buckets starting strictly before this instant are returned.
	Before *time.Time
	// Fill, when true, emits zero-volume bars for empty buckets across the
	// resolved range (default: empty buckets are omitted).
	Fill bool
}

// TradeStats holds aggregate trade statistics.
type TradeStats struct {
	TotalTrades int64 `json:"totalTrades"`
	TotalVolume int64 `json:"totalVolume"`
}

// TradeReader abstracts read-only trade/candle/stats queries.
type TradeReader interface {
	QueryTrades(ctx context.Context, f TradeFilter) ([]Trade, error)
	QueryCandles(ctx context.Context, f CandleFilter) ([]Candle, error)
	QueryTradeStats(ctx context.Context) (TradeStats, error)
}

// PgTradeReader implements TradeReader using a pgxpool.Pool.
type PgTradeReader struct {
	pool *pgxpool.Pool
}

// NewPgTradeReader creates a new PgTradeReader.
func NewPgTradeReader(pool *pgxpool.Pool) *PgTradeReader {
	return &PgTradeReader{pool: pool}
}

// Row-limit bounds shared by the history endpoints.
const (
	DefaultLimit = 100  // used when no (or a non-positive) limit is requested
	MaxLimit     = 1000 // hard ceiling; larger requests are clamped, not rejected
)

// ClampLimit normalizes a requested row limit: a non-positive limit falls back
// to DefaultLimit, and anything above MaxLimit is clamped down to MaxLimit
// (rather than silently reset to the default).
func ClampLimit(n int) int {
	switch {
	case n <= 0:
		return DefaultLimit
	case n > MaxLimit:
		return MaxLimit
	default:
		return n
	}
}

// intervalSeconds maps interval strings to their duration in seconds.
var intervalSeconds = map[string]int{
	"1m":  60,
	"5m":  300,
	"15m": 900,
	"1h":  3600,
	"4h":  14400,
	"1d":  86400,
}

// ValidInterval reports whether s is a supported candle interval.
func ValidInterval(s string) bool {
	_, ok := intervalSeconds[s]
	return ok
}

// QueryTrades returns trades for a symbol with optional time range and pagination.
func (r *PgTradeReader) QueryTrades(ctx context.Context, f TradeFilter) ([]Trade, error) {
	f.Limit = ClampLimit(f.Limit)

	rows, err := r.pool.Query(ctx,
		`SELECT match_number, ticker, price, shares, aggressor, executed_at
		 FROM trades
		 WHERE symbol_locate = $1
		   AND ($2::timestamptz IS NULL OR executed_at >= $2)
		   AND ($3::timestamptz IS NULL OR executed_at <= $3)
		 ORDER BY executed_at DESC
		 LIMIT $4 OFFSET $5`,
		int16(f.SymbolLocate), f.From, f.To, f.Limit, f.Offset)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer rows.Close()

	trades := []Trade{}
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.MatchNumber, &t.Ticker, &t.Price, &t.Shares, &t.Aggressor, &t.ExecutedAt); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		trades = append(trades, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trades: %w", err)
	}
	return trades, nil
}

// QueryCandles returns OHLCV bars for a symbol at the given interval.
func (r *PgTradeReader) QueryCandles(ctx context.Context, f CandleFilter) ([]Candle, error) {
	secs, ok := intervalSeconds[f.Interval]
	if !ok {
		return nil, fmt.Errorf("unsupported interval: %s", f.Interval)
	}
	f.Limit = ClampLimit(f.Limit)

	rows, err := r.pool.Query(ctx,
		`SELECT
			to_timestamp(floor(extract(epoch FROM executed_at) / $2) * $2) AS bucket,
			(array_agg(price ORDER BY executed_at ASC))[1] AS open,
			max(price) AS high,
			min(price) AS low,
			(array_agg(price ORDER BY executed_at DESC))[1] AS close,
			sum(shares)::bigint AS volume,
			count(*)::bigint AS count
		 FROM trades
		 WHERE symbol_locate = $1
		   AND ($3::timestamptz IS NULL OR executed_at >= $3)
		   AND ($4::timestamptz IS NULL OR executed_at <= $4)
		   AND ($6::timestamptz IS NULL OR executed_at < $6)
		 GROUP BY bucket
		 ORDER BY bucket DESC
		 LIMIT $5`,
		int16(f.SymbolLocate), secs, f.From, f.To, f.Limit, f.Before)
	if err != nil {
		return nil, fmt.Errorf("query candles: %w", err)
	}
	defer rows.Close()

	candles := []Candle{}
	for rows.Next() {
		var c Candle
		if err := rows.Scan(&c.Bucket, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.Count); err != nil {
			return nil, fmt.Errorf("scan candle: %w", err)
		}
		candles = append(candles, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candles: %w", err)
	}

	if f.Fill {
		if hi, lo, ok := f.fillBounds(secs, candles); ok {
			return zeroFill(candles, hi, lo, secs, f.Limit), nil
		}
	}
	return candles, nil
}

// alignDown returns the start (UTC) of the interval bucket containing t.
func alignDown(t time.Time, secs int) time.Time {
	e := t.Unix()
	return time.Unix(e-e%int64(secs), 0).UTC()
}

// fillBounds resolves the inclusive [lo, hi] bucket range to zero-fill over.
// Precedence: hi from Before (the bucket just under the cursor), else To, else
// the newest returned bucket; lo from From, else the oldest returned bucket.
// ok is false when no range can be determined (no candles and no bounds).
func (f CandleFilter) fillBounds(secs int, candles []Candle) (hi, lo time.Time, ok bool) {
	switch {
	case f.Before != nil:
		hi = alignDown(f.Before.Add(-time.Duration(secs)*time.Second), secs)
	case f.To != nil:
		hi = alignDown(*f.To, secs)
	case len(candles) > 0:
		hi = candles[0].Bucket.UTC()
	default:
		return time.Time{}, time.Time{}, false
	}

	switch {
	case f.From != nil:
		lo = alignDown(*f.From, secs)
	case len(candles) > 0:
		lo = candles[len(candles)-1].Bucket.UTC()
	default:
		lo = hi
	}
	if lo.After(hi) {
		lo = hi
	}
	return hi, lo, true
}

// zeroFill expands DB candles (newest-first) into a contiguous newest-first
// series from hi down to lo (inclusive, step secs), inserting zero-volume bars
// for empty buckets. Output is capped at limit (newest kept).
func zeroFill(dbCandles []Candle, hi, lo time.Time, secs, limit int) []Candle {
	byBucket := make(map[int64]Candle, len(dbCandles))
	for _, c := range dbCandles {
		byBucket[c.Bucket.Unix()] = c
	}
	step := int64(secs)
	out := []Candle{}
	for t := hi.Unix(); t >= lo.Unix() && len(out) < limit; t -= step {
		if c, ok := byBucket[t]; ok {
			out = append(out, c)
		} else {
			out = append(out, Candle{Bucket: time.Unix(t, 0).UTC()})
		}
	}
	return out
}

// QueryTradeStats returns aggregate trade count and volume.
func (r *PgTradeReader) QueryTradeStats(ctx context.Context) (TradeStats, error) {
	var ts TradeStats
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(count(*), 0), COALESCE(sum(shares)::bigint, 0) FROM trades`).
		Scan(&ts.TotalTrades, &ts.TotalVolume)
	if err != nil {
		return TradeStats{}, fmt.Errorf("query trade stats: %w", err)
	}
	return ts, nil
}
