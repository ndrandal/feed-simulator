package archive

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// S3Uploader is the subset of the S3 client we need.
type S3Uploader interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// Archiver periodically moves old trades from MongoDB to S3 Glacier.
type Archiver struct {
	db       *mongo.Database
	s3       S3Uploader
	bucket   string
	prefix   string
	interval time.Duration
	maxAge   time.Duration
}

// New creates a new Archiver.
func New(db *mongo.Database, s3Client S3Uploader, bucket, prefix string, intervalHours, afterHours int) *Archiver {
	return &Archiver{
		db:       db,
		s3:       s3Client,
		bucket:   bucket,
		prefix:   prefix,
		interval: time.Duration(intervalHours) * time.Hour,
		maxAge:   time.Duration(afterHours) * time.Hour,
	}
}

// Run starts the periodic archive loop. Blocks until ctx is cancelled.
func (a *Archiver) Run(ctx context.Context) {
	log.Printf("trade archiver: archiving trades older than %v every %v → s3://%s/%s/",
		a.maxAge, a.interval, a.bucket, a.prefix)

	// Run once immediately, then on ticker.
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

// cycle runs one archive pass.
func (a *Archiver) cycle(ctx context.Context) {
	cursor, err := a.loadCursor(ctx)
	if err != nil {
		log.Printf("trade archiver: load cursor error: %v", err)
		return
	}

	cutoff := time.Now().Add(-a.maxAge)
	if !cursor.Before(cutoff) {
		return // nothing to archive
	}

	trades, err := a.queryTrades(ctx, cursor, cutoff)
	if err != nil {
		log.Printf("trade archiver: query error: %v", err)
		return
	}
	if len(trades) == 0 {
		a.saveCursor(ctx, cutoff)
		return
	}

	// Group trades by calendar day (UTC).
	batches := groupByDay(trades)

	for day, batch := range batches {
		if err := a.uploadBatch(ctx, day, batch); err != nil {
			log.Printf("trade archiver: upload %s error: %v", day, err)
			return // stop this cycle; retry next time
		}

		if err := a.deleteBatch(ctx, batch); err != nil {
			log.Printf("trade archiver: delete %s error: %v", day, err)
			return
		}

		log.Printf("trade archiver: archived %d trades for %s", len(batch), day)
	}

	a.saveCursor(ctx, cutoff)
}

// tradeDoc mirrors the MongoDB trade document.
type tradeDoc struct {
	MatchNumber int64     `bson:"match_number" json:"match_number"`
	SymbolLocate uint16   `bson:"symbol_locate" json:"symbol_locate"`
	Ticker      string    `bson:"ticker"        json:"ticker"`
	Price       float64   `bson:"price"         json:"price"`
	Shares      int32     `bson:"shares"        json:"shares"`
	Aggressor   string    `bson:"aggressor"     json:"aggressor"`
	ExecutedAt  time.Time `bson:"executed_at"   json:"executed_at"`
}

func (a *Archiver) loadCursor(ctx context.Context) (time.Time, error) {
	var doc struct {
		ValueTime time.Time `bson:"value_time"`
	}
	err := a.db.Collection("sim_state").FindOne(ctx, bson.M{"key": "archive_cursor"}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return time.Time{}, nil // epoch zero — archive everything eligible
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
		log.Printf("trade archiver: save cursor error: %v", err)
	}
}

func (a *Archiver) queryTrades(ctx context.Context, from, to time.Time) ([]tradeDoc, error) {
	filter := bson.M{
		"executed_at": bson.M{
			"$gte": from,
			"$lt":  to,
		},
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

// uploadBatch gzips trades as NDJSON and uploads to S3 with GLACIER storage class.
func (a *Archiver) uploadBatch(ctx context.Context, day string, trades []tradeDoc) error {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	enc := json.NewEncoder(gz)
	for _, t := range trades {
		if err := enc.Encode(t); err != nil {
			gz.Close()
			return fmt.Errorf("encode trade: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}

	key := fmt.Sprintf("%s/trades/%s.jsonl.gz", a.prefix, day)

	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(a.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(buf.Bytes()),
		ContentType:  aws.String("application/gzip"),
		StorageClass: s3types.StorageClassGlacier,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
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
