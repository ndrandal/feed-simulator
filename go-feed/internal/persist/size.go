package persist

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SizeBudgetBytes is the hard storage ceiling the live trades DB is tuned
// against (2 GiB). Retention (TRADE_RETENTION_DAYS) is sized to keep the
// database comfortably under this; see README "Storage budget".
const SizeBudgetBytes int64 = 2 << 30

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
