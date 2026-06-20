package persist

import (
	"context"
	"fmt"
	"log"
	"time"
)

// RunRetention periodically deletes trades older than the retention period.
// Blocks until ctx is cancelled. Pass retentionDays <= 0 to disable.
func RunRetention(ctx context.Context, store *Store, retentionDays int) {
	if retentionDays <= 0 {
		log.Println("trade retention disabled (keep forever)")
		return
	}

	interval := 1 * time.Hour
	log.Printf("trade retention: pruning trades older than %d days every %v (storage budget %d GiB)",
		retentionDays, interval, SizeBudgetBytes>>30)

	mon := &sizeMonitor{}

	// Run once immediately on startup, then on the ticker.
	prune(ctx, store, retentionDays)
	mon.report(ctx, store)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			prune(ctx, store, retentionDays)
			mon.reportAt(ctx, store, now)
		}
	}
}

// sizeMonitor logs DB size each retention tick and estimates days-to-cap from
// the growth observed between ticks.
type sizeMonitor struct {
	prevBytes int64
	prevTime  time.Time
	havesPrev bool
}

func (m *sizeMonitor) report(ctx context.Context, store *Store) {
	m.reportAt(ctx, store, time.Now())
}

func (m *sizeMonitor) reportAt(ctx context.Context, store *Store, now time.Time) {
	size, err := queryDBSize(ctx, store.pool)
	if err != nil {
		log.Printf("db size: %v", err)
		return
	}

	msg := fmt.Sprintf("db size: %.1f MiB (%.1f%% of %d GiB budget; trades %.1f MiB + idx %.1f MiB)",
		mib(size.DatabaseBytes), size.PctOfBudget(), SizeBudgetBytes>>30,
		mib(size.TradesBytes), mib(size.TradesIndexBytes))

	if m.havesPrev {
		dt := now.Sub(m.prevTime).Seconds()
		grew := size.DatabaseBytes - m.prevBytes
		if dt > 0 && grew > 0 {
			bytesPerDay := float64(grew) / dt * 86400
			remaining := float64(SizeBudgetBytes - size.DatabaseBytes)
			daysToCap := remaining / bytesPerDay
			msg += fmt.Sprintf("; +%.1f MiB/day -> ~%.1f days to cap", bytesPerDay/(1<<20), daysToCap)
		}
	}
	log.Print(msg)

	m.prevBytes = size.DatabaseBytes
	m.prevTime = now
	m.havesPrev = true
}

func mib(b int64) float64 { return float64(b) / (1 << 20) }

func prune(ctx context.Context, store *Store, retentionDays int) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	result, err := store.pool.Exec(ctx,
		`DELETE FROM trades WHERE executed_at < $1`, cutoff)
	if err != nil {
		log.Printf("trade retention prune error: %v", err)
		return
	}

	if result.RowsAffected() > 0 {
		log.Printf("trade retention: pruned %d trades older than %s", result.RowsAffected(), cutoff.Format(time.DateOnly))
	}
}
