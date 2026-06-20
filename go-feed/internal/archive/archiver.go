package archive

import (
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

	// Archive whole UTC days that are fully older than maxAge, one day at a time.
	// Day-alignment means each day-file is written exactly once with its complete
	// contents, and streaming a single day keeps memory bounded (no whole-window
	// load, no in-memory buffer).
	cutoffDay := dayUTC(time.Now().Add(-a.maxAge))

	day, ok, err := a.startDay(ctx, cursor)
	if err != nil {
		log.Printf("trade archiver: start day: %v", err)
		return
	}
	if !ok {
		return // no trades to archive
	}

	for day.Before(cutoffDay) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		next := day.AddDate(0, 0, 1)
		n, err := a.archiveDay(ctx, day, next)
		if err != nil {
			log.Printf("trade archiver: archive %s: %v", day.Format("2006-01-02"), err)
			return
		}
		if n > 0 {
			log.Printf("trade archiver: archived %d trades for %s", n, day.Format("2006-01-02"))
		}
		a.saveCursor(ctx, next)
		day = next
	}

	a.rotate()
}

// dayUTC truncates t to UTC midnight.
func dayUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// startDay returns the first UTC day to archive: the day after the cursor, or
// the earliest trade's day when the cursor is unset. ok is false when there are
// no trades at all.
func (a *Archiver) startDay(ctx context.Context, cursor time.Time) (time.Time, bool, error) {
	if !cursor.IsZero() {
		return dayUTC(cursor), true, nil
	}
	var earliest *time.Time
	if err := a.pool.QueryRow(ctx, `SELECT min(executed_at) FROM trades`).Scan(&earliest); err != nil {
		return time.Time{}, false, fmt.Errorf("min executed_at: %w", err)
	}
	if earliest == nil {
		return time.Time{}, false, nil
	}
	return dayUTC(*earliest), true, nil
}

// archiveDay streams all trades in [day, next) to a single gzipped NDJSON
// day-file (written atomically via a temp file + rename), then deletes that
// range from the live table. Rows are streamed straight to the gzip writer, so
// neither the day nor the window is materialized in memory. Returns the count.
func (a *Archiver) archiveDay(ctx context.Context, day, next time.Time) (int, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT match_number, symbol_locate, ticker, price, shares, aggressor, executed_at
		 FROM trades
		 WHERE executed_at >= $1 AND executed_at < $2
		 ORDER BY executed_at ASC`,
		day, next)
	if err != nil {
		return 0, fmt.Errorf("query: %w", err)
	}

	var w *dayWriter
	count := 0
	for rows.Next() {
		var d tradeDoc
		if err := rows.Scan(&d.MatchNumber, &d.SymbolLocate, &d.Ticker, &d.Price, &d.Shares, &d.Aggressor, &d.ExecutedAt); err != nil {
			rows.Close()
			if w != nil {
				w.abort()
			}
			return 0, fmt.Errorf("scan: %w", err)
		}
		if w == nil {
			if w, err = newDayWriter(a.dir, day); err != nil {
				rows.Close()
				return 0, err
			}
		}
		if err := w.encode(&d); err != nil {
			rows.Close()
			w.abort()
			return 0, err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		if w != nil {
			w.abort()
		}
		return 0, fmt.Errorf("iterate: %w", err)
	}
	rows.Close()

	if w == nil {
		return 0, nil // no trades that day
	}
	if err := w.commit(); err != nil {
		return 0, err
	}

	if _, err := a.pool.Exec(ctx,
		`DELETE FROM trades WHERE executed_at >= $1 AND executed_at < $2`, day, next); err != nil {
		return 0, fmt.Errorf("delete archived range: %w", err)
	}
	return count, nil
}

// dayWriter streams trades to <dir>/trades/YYYY/MM/DD.jsonl.gz via a temp file
// that is renamed into place on commit (atomic) or discarded on abort.
type dayWriter struct {
	finalPath string
	tmpPath   string
	file      *os.File
	gz        *gzip.Writer
	enc       *json.Encoder
}

func newDayWriter(dir string, day time.Time) (*dayWriter, error) {
	final := filepath.Join(dir, "trades", day.UTC().Format("2006/01/02")+".jsonl.gz")
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	tmp := final + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("create: %w", err)
	}
	gz := gzip.NewWriter(f)
	return &dayWriter{finalPath: final, tmpPath: tmp, file: f, gz: gz, enc: json.NewEncoder(gz)}, nil
}

func (w *dayWriter) encode(d *tradeDoc) error {
	if err := w.enc.Encode(d); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	return nil
}

func (w *dayWriter) commit() error {
	if err := w.gz.Close(); err != nil {
		w.file.Close()
		return fmt.Errorf("gzip close: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		w.file.Close()
		return fmt.Errorf("sync: %w", err)
	}
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(w.tmpPath, w.finalPath); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (w *dayWriter) abort() {
	w.gz.Close()
	w.file.Close()
	os.Remove(w.tmpPath)
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
