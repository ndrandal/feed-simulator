package archive

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

// maxLineBytes bounds a single NDJSON line so a corrupt file can't blow up the
// scanner buffer. A trade record is well under this.
const maxLineBytes = 1 << 20 // 1 MiB

// Reader streams archived trades back from cold day-files via a Catalog. It
// decodes gzipped NDJSON line-by-line and never holds more than one day's
// limit-sized tail (plus the result) in memory, so neither a single day-file
// nor the whole window is slurped into RAM.
type Reader struct {
	cat *Catalog
}

// NewReader returns a Reader backed by cat.
func NewReader(cat *Catalog) *Reader {
	return &Reader{cat: cat}
}

// Enabled reports whether an archive directory is configured.
func (r *Reader) Enabled() bool { return r.cat != nil && r.cat.Enabled() }

// ReadFilter selects archived trades for one symbol within a time window.
type ReadFilter struct {
	SymbolLocate uint16
	From         time.Time // zero == unbounded lower bound
	To           time.Time // zero == unbounded upper bound
	Limit        int
}

// Read returns up to f.Limit archived trades for the symbol within [From, To],
// ordered newest-first. Day-files are opened newest-first; each is gzip-streamed
// and filtered line-by-line, keeping only the newest f.Limit matches via a tail
// ring buffer. Respects ctx cancellation.
func (r *Reader) Read(ctx context.Context, f ReadFilter) ([]persist.Trade, error) {
	limit := persist.ClampLimit(f.Limit)

	lo, hi := f.From, f.To
	if lo.IsZero() {
		lo = time.Time{}.AddDate(1, 0, 0) // any very-early instant
	}
	if hi.IsZero() {
		hi = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}

	dayFiles, err := r.cat.Resolve(lo, hi)
	if err != nil {
		return nil, err
	}

	result := make([]persist.Trade, 0, min(limit, 1024))
	remaining := limit

	// Newest-first: walk resolved day-files in reverse (catalog returns ascending).
	for i := len(dayFiles) - 1; i >= 0 && remaining > 0; i-- {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		tail, err := r.readDayTail(ctx, dayFiles[i].Path, f.SymbolLocate, f.From, f.To, remaining)
		if err != nil {
			return result, err
		}
		dayTrades := tail.newestFirst()
		result = append(result, dayTrades...)
		remaining -= len(dayTrades)
	}

	return result, nil
}

// readDayTail streams one day-file and returns the newest `keep` matching trades
// (by symbol + [from,to]) as a tail ring buffer, so memory stays O(keep).
func (r *Reader) readDayTail(ctx context.Context, path string, locate uint16, from, to time.Time, keep int) (*tail, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", path, err)
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("gunzip %s: %w", path, err)
	}
	defer gz.Close()
	// Multistream is on by default, so concatenated gzip members (appended across
	// archive cycles) decode transparently.

	t := newTail(keep)
	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for n := 0; sc.Scan(); n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return t, err
			}
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var d tradeDoc
		if err := json.Unmarshal(line, &d); err != nil {
			return t, fmt.Errorf("decode %s: %w", path, err)
		}
		if uint16(d.SymbolLocate) != locate {
			continue
		}
		if !from.IsZero() && d.ExecutedAt.Before(from) {
			continue
		}
		if !to.IsZero() && d.ExecutedAt.After(to) {
			continue
		}
		t.push(persist.Trade{
			MatchNumber: d.MatchNumber,
			Ticker:      d.Ticker,
			Price:       d.Price,
			Shares:      d.Shares,
			Aggressor:   d.Aggressor,
			ExecutedAt:  d.ExecutedAt,
		})
	}
	if err := sc.Err(); err != nil {
		return t, fmt.Errorf("scan %s: %w", path, err)
	}
	return t, nil
}

// tail is a fixed-capacity ring buffer that retains the most recently pushed
// items (the newest, since day-files are ascending), in O(1) per push.
type tail struct {
	buf   []persist.Trade
	count int
	head  int // index of the oldest retained item
}

func newTail(capacity int) *tail {
	if capacity < 0 {
		capacity = 0
	}
	return &tail{buf: make([]persist.Trade, capacity)}
}

func (t *tail) push(v persist.Trade) {
	if len(t.buf) == 0 {
		return
	}
	if t.count < len(t.buf) {
		t.buf[(t.head+t.count)%len(t.buf)] = v
		t.count++
		return
	}
	// Full: overwrite the oldest and advance head.
	t.buf[t.head] = v
	t.head = (t.head + 1) % len(t.buf)
}

// newestFirst returns the retained items ordered newest-first.
func (t *tail) newestFirst() []persist.Trade {
	out := make([]persist.Trade, t.count)
	for i := 0; i < t.count; i++ {
		out[t.count-1-i] = t.buf[(t.head+i)%len(t.buf)]
	}
	return out
}
