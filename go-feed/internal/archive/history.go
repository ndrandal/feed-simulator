package archive

import (
	"context"
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
