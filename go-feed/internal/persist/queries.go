package persist

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Trade represents a persisted trade document.
type Trade struct {
	MatchNumber int64     `json:"matchNumber" bson:"match_number"`
	Ticker      string    `json:"ticker"      bson:"ticker"`
	Price       float64   `json:"price"       bson:"price"`
	Shares      int32     `json:"shares"      bson:"shares"`
	Aggressor   string    `json:"aggressor"   bson:"aggressor"`
	ExecutedAt  time.Time `json:"executedAt"  bson:"executed_at"`
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

// MongoTradeReader implements TradeReader using a mongo.Database.
type MongoTradeReader struct {
	db *mongo.Database
}

// NewMongoTradeReader creates a new MongoTradeReader.
func NewMongoTradeReader(db *mongo.Database) *MongoTradeReader {
	return &MongoTradeReader{db: db}
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
func (r *MongoTradeReader) QueryTrades(ctx context.Context, f TradeFilter) ([]Trade, error) {
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}

	filter := bson.M{"symbol_locate": f.SymbolLocate}
	if f.From != nil || f.To != nil {
		timeFilter := bson.M{}
		if f.From != nil {
			timeFilter["$gte"] = *f.From
		}
		if f.To != nil {
			timeFilter["$lte"] = *f.To
		}
		filter["executed_at"] = timeFilter
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "executed_at", Value: -1}}).
		SetLimit(int64(f.Limit)).
		SetSkip(int64(f.Offset))

	cursor, err := r.db.Collection("trades").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}
	defer cursor.Close(ctx)

	trades := []Trade{}
	if err := cursor.All(ctx, &trades); err != nil {
		return nil, fmt.Errorf("decode trades: %w", err)
	}
	return trades, nil
}

// QueryCandles returns OHLCV bars for a symbol at the given interval.
func (r *MongoTradeReader) QueryCandles(ctx context.Context, f CandleFilter) ([]Candle, error) {
	secs, ok := intervalSeconds[f.Interval]
	if !ok {
		return nil, fmt.Errorf("unsupported interval: %s", f.Interval)
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}

	matchFilter := bson.M{"symbol_locate": f.SymbolLocate}
	if f.From != nil || f.To != nil {
		timeFilter := bson.M{}
		if f.From != nil {
			timeFilter["$gte"] = *f.From
		}
		if f.To != nil {
			timeFilter["$lte"] = *f.To
		}
		matchFilter["executed_at"] = timeFilter
	}

	millisPerBucket := int64(secs) * 1000

	// Floor epoch-millis to interval boundary:
	// bucket = Date(toLong(executed_at) - (toLong(executed_at) % millisPerBucket))
	bucketExpr := bson.M{
		"$toDate": bson.M{
			"$subtract": bson.A{
				bson.M{"$toLong": "$executed_at"},
				bson.M{"$mod": bson.A{
					bson.M{"$toLong": "$executed_at"},
					millisPerBucket,
				}},
			},
		},
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchFilter}},
		{{Key: "$sort", Value: bson.D{{Key: "executed_at", Value: 1}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bucketExpr},
			{Key: "open", Value: bson.M{"$first": "$price"}},
			{Key: "high", Value: bson.M{"$max": "$price"}},
			{Key: "low", Value: bson.M{"$min": "$price"}},
			{Key: "close", Value: bson.M{"$last": "$price"}},
			{Key: "volume", Value: bson.M{"$sum": "$shares"}},
			{Key: "count", Value: bson.M{"$sum": 1}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: -1}}}},
		{{Key: "$limit", Value: int64(f.Limit)}},
	}

	cursor, err := r.db.Collection("trades").Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("query candles: %w", err)
	}
	defer cursor.Close(ctx)

	var raw []struct {
		Bucket time.Time `bson:"_id"`
		Open   float64   `bson:"open"`
		High   float64   `bson:"high"`
		Low    float64   `bson:"low"`
		Close  float64   `bson:"close"`
		Volume int64     `bson:"volume"`
		Count  int64     `bson:"count"`
	}
	if err := cursor.All(ctx, &raw); err != nil {
		return nil, fmt.Errorf("decode candles: %w", err)
	}

	candles := make([]Candle, len(raw))
	for i, r := range raw {
		candles[i] = Candle{
			Bucket: r.Bucket,
			Open:   r.Open,
			High:   r.High,
			Low:    r.Low,
			Close:  r.Close,
			Volume: r.Volume,
			Count:  r.Count,
		}
	}
	if candles == nil {
		candles = []Candle{}
	}
	return candles, nil
}

// QueryTradeStats returns aggregate trade count and volume.
func (r *MongoTradeReader) QueryTradeStats(ctx context.Context) (TradeStats, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "total_trades", Value: bson.M{"$sum": 1}},
			{Key: "total_volume", Value: bson.M{"$sum": "$shares"}},
		}}},
	}

	cursor, err := r.db.Collection("trades").Aggregate(ctx, pipeline)
	if err != nil {
		return TradeStats{}, fmt.Errorf("query trade stats: %w", err)
	}
	defer cursor.Close(ctx)

	var results []struct {
		TotalTrades int64 `bson:"total_trades"`
		TotalVolume int64 `bson:"total_volume"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return TradeStats{}, fmt.Errorf("decode trade stats: %w", err)
	}

	if len(results) == 0 {
		return TradeStats{}, nil
	}
	return TradeStats{
		TotalTrades: results[0].TotalTrades,
		TotalVolume: results[0].TotalVolume,
	}, nil
}
