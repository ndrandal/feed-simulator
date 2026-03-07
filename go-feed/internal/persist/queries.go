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

// intervalSeconds maps interval strings to their duration in seconds.
var intervalSeconds = map[string]int{
	"1m":  60,
	"5m":  300,
	"15m": 900,
	"1h":  3600,
	"4h":  14400,
	"1d":  86400,
}

// QueryTrades returns trades for a symbol with optional time range and pagination.
func (r *PgTradeReader) QueryTrades(ctx context.Context, f TradeFilter) ([]Trade, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}

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
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}

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
		 GROUP BY bucket
		 ORDER BY bucket DESC
		 LIMIT $5`,
		int16(f.SymbolLocate), secs, f.From, f.To, f.Limit)
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
	return candles, nil
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
