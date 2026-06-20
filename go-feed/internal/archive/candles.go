package archive

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

// ReadCandles computes OHLCV bars from archived trades by streaming day-files
// and bucketing on the fly — the same aggregation the live SQL path performs.
// Bars are returned newest-first, capped at limit. before, when set, excludes
// buckets whose start is at/after the cursor (newest-first pagination). Buckets
// never cross a day boundary (all supported intervals divide a day), so each day
// is bucketed independently and at most one day's bars (≤ a day's worth) are
// held at once.
func (r *Reader) ReadCandles(ctx context.Context, locate uint16, from, to time.Time, secs, limit int, before *time.Time) ([]persist.Candle, error) {
	if secs <= 0 {
		return nil, fmt.Errorf("invalid interval seconds: %d", secs)
	}
	limit = persist.ClampLimit(limit)

	lo, hi := from, to
	if lo.IsZero() {
		lo = time.Time{}.AddDate(1, 0, 0)
	}
	if hi.IsZero() {
		hi = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	}
	dayFiles, err := r.cat.Resolve(lo, hi)
	if err != nil {
		return nil, err
	}

	result := make([]persist.Candle, 0, limit)
	for i := len(dayFiles) - 1; i >= 0 && len(result) < limit; i-- {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		dayBars, err := r.readDayCandles(ctx, dayFiles[i].Path, locate, from, to, secs, before)
		if err != nil {
			return result, err
		}
		result = append(result, dayBars...)
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

type candleAgg struct {
	open, high, low, close float64
	volume, count          int64
}

// readDayCandles buckets one day-file's matching trades and returns the bars
// newest-first.
func (r *Reader) readDayCandles(ctx context.Context, path string, locate uint16, from, to time.Time, secs int, before *time.Time) ([]persist.Candle, error) {
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

	step := int64(secs)
	buckets := map[int64]*candleAgg{}

	sc := bufio.NewScanner(gz)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for n := 0; sc.Scan(); n++ {
		if n%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var d tradeDoc
		if err := json.Unmarshal(line, &d); err != nil {
			return nil, fmt.Errorf("decode %s: %w", path, err)
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
		bucketStart := d.ExecutedAt.Unix() - d.ExecutedAt.Unix()%step
		if before != nil && bucketStart >= before.Unix() {
			continue
		}
		// Trades stream in ascending time within a day, so the first trade seen in
		// a bucket is the open and the last is the close.
		a := buckets[bucketStart]
		if a == nil {
			buckets[bucketStart] = &candleAgg{open: d.Price, high: d.Price, low: d.Price, close: d.Price, volume: int64(d.Shares), count: 1}
			continue
		}
		if d.Price > a.high {
			a.high = d.Price
		}
		if d.Price < a.low {
			a.low = d.Price
		}
		a.close = d.Price
		a.volume += int64(d.Shares)
		a.count++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}

	out := make([]persist.Candle, 0, len(buckets))
	for start, a := range buckets {
		out = append(out, persist.Candle{
			Bucket: time.Unix(start, 0).UTC(),
			Open:   a.open, High: a.high, Low: a.low, Close: a.close,
			Volume: a.volume, Count: a.count,
		})
	}
	// Newest-first.
	sort.Slice(out, func(i, j int) bool { return out[i].Bucket.After(out[j].Bucket) })
	return out, nil
}
