package persist

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SizeBudgetBytes is the hard storage ceiling the live trades DB is tuned
// against (2 GiB). Retention (TRADE_RETENTION_DAYS) is sized to keep the
// database comfortably under this; see README "Storage budget".
const SizeBudgetBytes int64 = 2 << 30

// Storage-budget tuning constants. The per-trade cost and trade rate were
// measured against the default 30-symbol simulation (see README "Storage
// budget") and are used to derive the default TRADE_RETENTION_DAYS.
const (
	// HeadroomBytes is the soft target the live DB is sized to stay under,
	// leaving margin below the hard SizeBudgetBytes cap.
	HeadroomBytes int64 = 1600 << 20 // 1.6 GiB

	// HighWaterPct is the percent-of-budget at which RunRetention emits a WARN.
	HighWaterPct = 80.0

	// BytesPerTradeEstimate is the measured on-disk cost of one trade
	// (heap + both indexes), with margin.
	BytesPerTradeEstimate = 150.0

	// TradesPerSecEstimate is the measured steady-state trade rate of the
	// default simulation, with margin.
	TradesPerSecEstimate = 75.0
)

// SafeRetentionDays returns how many days of trades fit in budgetBytes given the
// per-trade on-disk cost (heap+index) and trade rate. Returns +Inf for a zero
// rate. This is the math behind the default TRADE_RETENTION_DAYS; see README.
func SafeRetentionDays(bytesPerTrade, tradesPerSec float64, budgetBytes int64) float64 {
	bytesPerDay := bytesPerTrade * tradesPerSec * 86400
	if bytesPerDay <= 0 {
		return math.Inf(1)
	}
	return float64(budgetBytes) / bytesPerDay
}

// DBSize reports on-disk usage for the simulator database.
type DBSize struct {
	DatabaseBytes    int64 `json:"databaseBytes"`    // pg_database_size(current_database())
	TradesBytes      int64 `json:"tradesBytes"`      // heap size of the trades table
	TradesIndexBytes int64 `json:"tradesIndexBytes"` // size of the trades indexes
}

// PctOfBudget returns the database size as a percentage of SizeBudgetBytes.
func (s DBSize) PctOfBudget() float64 {
	return float64(s.DatabaseBytes) / float64(SizeBudgetBytes) * 100
}

// queryDBSize reads live size figures from PostgreSQL.
func queryDBSize(ctx context.Context, pool *pgxpool.Pool) (DBSize, error) {
	var s DBSize
	err := pool.QueryRow(ctx,
		`SELECT pg_database_size(current_database()),
		        pg_table_size('trades'::regclass),
		        pg_indexes_size('trades'::regclass)`).
		Scan(&s.DatabaseBytes, &s.TradesBytes, &s.TradesIndexBytes)
	if err != nil {
		return DBSize{}, fmt.Errorf("query db size: %w", err)
	}
	return s, nil
}

// QueryDBSize returns live on-disk size figures for the database.
func (r *PgTradeReader) QueryDBSize(ctx context.Context) (DBSize, error) {
	return queryDBSize(ctx, r.pool)
}
