package archive

import (
	"context"
	"fmt"
	"time"

	"github.com/ndrandal/feed-simulator/go-feed/internal/persist"
)

// History serves trade lookups transparently across the live database and the
// cold archive. Recent trades (within the retention window) come from the live
// PgTradeReader; older trades come from the archive Reader. Callers use the
// persist.TradeReader interface and never need to know where a trade lives.
//
// Boundary: the split is the retention cutoff, now - retentionDays — the same
// cutoff the retention pruner uses. Trades at/after the cutoff are served from
// live; trades before it from the archive. Because retention is tuned to exceed
// the archive-after age (ENC-658), everything older than the cutoff has already
// been rolled to the archive, so the split has no gap and no double-counting.
type History struct {
	persist.TradeReader // live reader; supplies the non-trade queries and the live QueryTrades

	archive       *Reader
	retentionDays int
	now           func() time.Time
}

// NewHistory wraps a live persist.TradeReader with archive fallthrough. A nil or
// disabled archive, or retentionDays <= 0 (keep-forever), makes QueryTrades a
// pass-through to the live reader.
func NewHistory(live persist.TradeReader, archive *Reader, retentionDays int) *History {
	return &History{
		TradeReader:   live,
		archive:       archive,
		retentionDays: retentionDays,
		now:           time.Now,
	}
}

func (h *History) archiveActive() bool {
	return h.archive != nil && h.archive.Enabled() && h.retentionDays > 0
}

// QueryCandles returns OHLCV bars for one symbol newest-first, spanning the
// live/cold boundary. The split is day-aligned at the day after the newest
// archived day: live aggregates bars from that day on (SQL), the archive
// streams + buckets older trades. Day-alignment keeps every bucket sourced from
// exactly one store (no split or double-counted boundary bar). Respects the
// interval allow-list, the `before` cursor, and `fill` (applied once over the
// merged series). The archive is only read when live doesn't fill the page.
func (h *History) QueryCandles(ctx context.Context, f persist.CandleFilter) ([]persist.Candle, error) {
	if !h.archiveActive() {
		return h.TradeReader.QueryCandles(ctx, f)
	}
	secs, ok := persist.IntervalSeconds(f.Interval)
	if !ok {
		return nil, fmt.Errorf("unsupported interval: %s", f.Interval)
	}
	limit := persist.ClampLimit(f.Limit)

	_, archiveMax, hasArchive, err := h.archive.Bounds()
	if err != nil {
		return nil, err
	}
	if !hasArchive {
		return h.TradeReader.QueryCandles(ctx, f) // archiving enabled but nothing archived yet
	}
	// Start of the day after the newest archived day: live owns bars at/after it.
	split := archiveMax.AddDate(0, 0, 1)

	var merged []persist.Candle

	// Live bars: bucket start at/after the split.
	if f.To == nil || !f.To.Before(split) {
		liveFrom := split
		if f.From != nil && f.From.After(split) {
			liveFrom = *f.From
		}
		lf := f
		lf.From = &liveFrom
		lf.Fill = false // fill is applied once over the merged series below
		lf.Limit = limit
		live, err := h.TradeReader.QueryCandles(ctx, lf)
		if err != nil {
			return nil, err
		}
		merged = live
	}

	// Archive bars: bucket start strictly before the split.
	if len(merged) < limit && (f.From == nil || f.From.Before(split)) {
		archiveTo := split.Add(-time.Nanosecond)
		if f.To != nil && f.To.Before(archiveTo) {
			archiveTo = *f.To
		}
		var archiveFrom time.Time
		if f.From != nil {
			archiveFrom = *f.From
		}
		cold, err := h.archive.ReadCandles(ctx, f.SymbolLocate, archiveFrom, archiveTo, secs, limit-len(merged), f.Before)
		if err != nil {
			return nil, err
		}
		merged = append(merged, cold...)
	}

	if f.Fill {
		merged = persist.FillCandles(merged, f, secs, limit)
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// Meta describes the history available through this reader: the live retention
// window and, when archiving is enabled, the span of archived cold data.
type Meta struct {
	RetentionDays  int        `json:"retentionDays"`
	ArchiveEnabled bool       `json:"archiveEnabled"`
	ArchiveMinDay  *time.Time `json:"archiveMinDay,omitempty"`
	ArchiveMaxDay  *time.Time `json:"archiveMaxDay,omitempty"`
}

// HistoryMeta returns the retention window and archived-data bounds. It is the
// provider method the API's /api/history/meta handler looks for.
func (h *History) HistoryMeta(ctx context.Context) (Meta, error) {
	m := Meta{
		RetentionDays:  h.retentionDays,
		ArchiveEnabled: h.archive != nil && h.archive.Enabled(),
	}
	if !m.ArchiveEnabled {
		return m, nil
	}
	min, max, ok, err := h.archive.Bounds()
	if err != nil {
		return m, err
	}
	if ok {
		m.ArchiveMinDay = &min
		m.ArchiveMaxDay = &max
	}
	return m, nil
}

// QueryTrades returns trades for one symbol newest-first, spanning the live/cold
// boundary as needed. Limit/offset apply to the merged sequence. When the page
// is satisfied entirely from live, the archive is not touched.
func (h *History) QueryTrades(ctx context.Context, f persist.TradeFilter) ([]persist.Trade, error) {
	if !h.archiveActive() {
		return h.TradeReader.QueryTrades(ctx, f)
	}

	limit := persist.ClampLimit(f.Limit)
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	// Total newest-first items to gather before applying offset, bounded so the
	// live query stays within its row clamp.
	need := offset + limit
	if need > persist.MaxLimit {
		need = persist.MaxLimit
	}

	cutoff := h.now().AddDate(0, 0, -h.retentionDays)

	var combined []persist.Trade

	// Live portion: range reaches live when To is unbounded or at/after cutoff.
	if f.To == nil || !f.To.Before(cutoff) {
		liveFrom := cutoff
		if f.From != nil && f.From.After(cutoff) {
			liveFrom = *f.From
		}
		live, err := h.TradeReader.QueryTrades(ctx, persist.TradeFilter{
			SymbolLocate: f.SymbolLocate,
			From:         &liveFrom,
			To:           f.To,
			Limit:        need,
			Offset:       0,
		})
		if err != nil {
			return nil, err
		}
		combined = live
	}

	// Archive portion: only if live didn't already fill the page and the range
	// reaches before the cutoff.
	if len(combined) < need && (f.From == nil || f.From.Before(cutoff)) {
		archiveTo := cutoff.Add(-time.Nanosecond) // strictly before the cutoff
		if f.To != nil && f.To.Before(archiveTo) {
			archiveTo = *f.To
		}
		var archiveFrom time.Time
		if f.From != nil {
			archiveFrom = *f.From
		}
		cold, err := h.archive.Read(ctx, ReadFilter{
			SymbolLocate: f.SymbolLocate,
			From:         archiveFrom,
			To:           archiveTo,
			Limit:        need - len(combined),
		})
		if err != nil {
			return nil, err
		}
		combined = append(combined, cold...)
	}

	// Apply offset/limit over the merged newest-first sequence.
	if offset >= len(combined) {
		return []persist.Trade{}, nil
	}
	end := offset + limit
	if end > len(combined) {
		end = len(combined)
	}
	return combined[offset:end], nil
}
