package archive

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Archiver periodically moves old trades from PostgreSQL to local gzipped NDJSON
// files, deleting the oldest archives when total size exceeds maxBytes.
type Archiver struct {
	pool     *pgxpool.Pool
	dir      string
	maxBytes int64
	interval time.Duration
	maxAge   time.Duration
}

// New creates a new Archiver.
func New(pool *pgxpool.Pool, dir string, maxGB, intervalHours, afterHours int) *Archiver {
	return &Archiver{
		pool:     pool,
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

// tradeDoc mirrors the trades table row.
type tradeDoc struct {
	MatchNumber  int64     `json:"match_number"`
	SymbolLocate int16     `json:"symbol_locate"`
	Ticker       string    `json:"ticker"`
	Price        float64   `json:"price"`
	Shares       int32     `json:"shares"`
	Aggressor    string    `json:"aggressor"`
	ExecutedAt   time.Time `json:"executed_at"`
}

func (a *Archiver) loadCursor(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := a.pool.QueryRow(ctx,
		`SELECT value_time FROM sim_state WHERE key = 'archive_cursor'`).Scan(&t)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return t, nil
}

func (a *Archiver) saveCursor(ctx context.Context, t time.Time) {
	_, err := a.pool.Exec(ctx,
		`INSERT INTO sim_state (key, value_time, updated_at)
		 VALUES ('archive_cursor', $1, $2)
		 ON CONFLICT (key) DO UPDATE SET value_time = EXCLUDED.value_time, updated_at = EXCLUDED.updated_at`,
		t, time.Now())
	if err != nil {
		log.Printf("trade archiver: save cursor: %v", err)
	}
}

func (a *Archiver) queryTrades(ctx context.Context, from, to time.Time) ([]tradeDoc, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT match_number, symbol_locate, ticker, price, shares, aggressor, executed_at
		 FROM trades
		 WHERE executed_at >= $1 AND executed_at < $2
		 ORDER BY executed_at ASC`,
		from, to)
	if err != nil {
		return nil, fmt.Errorf("find trades: %w", err)
	}
	defer rows.Close()

	var trades []tradeDoc
	for rows.Next() {
		var t tradeDoc
		if err := rows.Scan(&t.MatchNumber, &t.SymbolLocate, &t.Ticker, &t.Price, &t.Shares, &t.Aggressor, &t.ExecutedAt); err != nil {
			return nil, fmt.Errorf("scan trade: %w", err)
		}
		trades = append(trades, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trades: %w", err)
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

	_, err := a.pool.Exec(ctx,
		`DELETE FROM trades WHERE match_number = ANY($1)`, ids)
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
