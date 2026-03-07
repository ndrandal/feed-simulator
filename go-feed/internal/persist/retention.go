package persist

import (
	"context"
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
	log.Printf("trade retention: pruning trades older than %d days every %v", retentionDays, interval)

	// Run once immediately on startup, then on the ticker.
	prune(ctx, store, retentionDays)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune(ctx, store, retentionDays)
		}
	}
}

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
