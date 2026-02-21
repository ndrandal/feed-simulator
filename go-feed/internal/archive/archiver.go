package archive

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Archiver periodically moves old trades from MongoDB to local gzipped NDJSON
// files, deleting the oldest archives when total size exceeds maxBytes.
type Archiver struct {
	db       *mongo.Database
	dir      string
	maxBytes int64
	interval time.Duration
	maxAge   time.Duration
}

// New creates a new Archiver.
func New(db *mongo.Database, dir string, maxGB, intervalHours, afterHours int) *Archiver {
	return &Archiver{
		db:       db,
		dir:      dir,
		maxBytes: int64(maxGB) * 1 << 30,
		interval: time.Duration(intervalHours) * time.Hour,
		maxAge:   time.Duration(afterHours) * time.Hour,
	}
}

// Run starts the periodic archive loop. Blocks until ctx is cancelled.
func (a *Archiver) Run(ctx context.Context) {
	log.Printf("trade archiver: dir=%s max=%dGB interval=%v age=%v",
		a.dir, a.maxBytes>>30, a.interval, a.maxAge)

	a.cycle(ctx)

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.cycle(ctx)
		}
	}
}

func (a *Archiver) cycle(ctx context.Context) {
	cursor, err := a.loadCursor(ctx)
	if err != nil {
		log.Printf("trade archiver: load cursor: %v", err)
		return
	}

	cutoff := time.Now().Add(-a.maxAge)
	if !cursor.Before(cutoff) {
		return
	}

	trades, err := a.queryTrades(ctx, cursor, cutoff)
	if err != nil {
		log.Printf("trade archiver: query: %v", err)
		return
	}
	if len(trades) == 0 {
		a.saveCursor(ctx, cutoff)
		return
	}

	batches := groupByDay(trades)

	for day, batch := range batches {
		if err := a.writeBatch(day, batch); err != nil {
			log.Printf("trade archiver: write %s: %v", day, err)
			return
		}

		if err := a.deleteBatch(ctx, batch); err != nil {
			log.Printf("trade archiver: delete %s: %v", day, err)
			return
		}

		log.Printf("trade archiver: archived %d trades for %s", len(batch), day)
	}

	a.saveCursor(ctx, cutoff)
	a.rotate()
}

// tradeDoc mirrors the MongoDB trade document.
type tradeDoc struct {
	MatchNumber  int64     `bson:"match_number"  json:"match_number"`
	SymbolLocate uint16    `bson:"symbol_locate" json:"symbol_locate"`
	Ticker       string    `bson:"ticker"        json:"ticker"`
	Price        float64   `bson:"price"         json:"price"`
	Shares       int32     `bson:"shares"        json:"shares"`
	Aggressor    string    `bson:"aggressor"     json:"aggressor"`
	ExecutedAt   time.Time `bson:"executed_at"   json:"executed_at"`
}

func (a *Archiver) loadCursor(ctx context.Context) (time.Time, error) {
	var doc struct {
		ValueTime time.Time `bson:"value_time"`
	}
	err := a.db.Collection("sim_state").FindOne(ctx, bson.M{"key": "archive_cursor"}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return doc.ValueTime, nil
}

func (a *Archiver) saveCursor(ctx context.Context, t time.Time) {
	_, err := a.db.Collection("sim_state").UpdateOne(ctx,
		bson.M{"key": "archive_cursor"},
		bson.M{"$set": bson.M{
			"key":        "archive_cursor",
			"value_time": t,
			"updated_at": time.Now(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		log.Printf("trade archiver: save cursor: %v", err)
	}
}

func (a *Archiver) queryTrades(ctx context.Context, from, to time.Time) ([]tradeDoc, error) {
	filter := bson.M{
		"executed_at": bson.M{"$gte": from, "$lt": to},
	}
	opts := options.Find().SetSort(bson.D{{Key: "executed_at", Value: 1}})

	cur, err := a.db.Collection("trades").Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find trades: %w", err)
	}
	defer cur.Close(ctx)

	var trades []tradeDoc
	if err := cur.All(ctx, &trades); err != nil {
		return nil, fmt.Errorf("decode trades: %w", err)
	}
	return trades, nil
}

func groupByDay(trades []tradeDoc) map[string][]tradeDoc {
	batches := make(map[string][]tradeDoc)
	for _, t := range trades {
		day := t.ExecutedAt.UTC().Format("2006/01/02")
		batches[day] = append(batches[day], t)
	}
	return batches
}

// writeBatch writes trades as gzipped NDJSON to dir/trades/YYYY/MM/DD.jsonl.gz.
func (a *Archiver) writeBatch(day string, trades []tradeDoc) error {
	path := filepath.Join(a.dir, "trades", day+".jsonl.gz")

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, t := range trades {
		if err := enc.Encode(t); err != nil {
			gz.Close()
			return fmt.Errorf("encode: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func (a *Archiver) deleteBatch(ctx context.Context, trades []tradeDoc) error {
	ids := make([]int64, len(trades))
	for i, t := range trades {
		ids[i] = t.MatchNumber
	}

	_, err := a.db.Collection("trades").DeleteMany(ctx, bson.M{
		"match_number": bson.M{"$in": ids},
	})
	if err != nil {
		return fmt.Errorf("delete archived trades: %w", err)
	}
	return nil
}

// rotate deletes the oldest archive files until total size is under maxBytes.
func (a *Archiver) rotate() {
	root := filepath.Join(a.dir, "trades")

	type entry struct {
		path string
		size int64
	}

	var files []entry
	var total int64

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		files = append(files, entry{path: path, size: info.Size()})
		total += info.Size()
		return nil
	})

	if total <= a.maxBytes {
		return
	}

	// Sort oldest first (path is YYYY/MM/DD so lexicographic = chronological).
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})

	for _, f := range files {
		if total <= a.maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil {
			log.Printf("trade archiver: remove %s: %v", f.path, err)
			continue
		}
		total -= f.size
		log.Printf("trade archiver: rotated out %s (%d bytes)", f.path, f.size)
	}
}
