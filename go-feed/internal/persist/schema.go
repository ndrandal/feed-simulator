package persist

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CreateTables creates all tables and indexes idempotently.
func CreateTables(ctx context.Context, pool *pgxpool.Pool) error {
	ddl := `
CREATE TABLE IF NOT EXISTS symbols (
	locate_code SMALLINT PRIMARY KEY,
	ticker      TEXT NOT NULL UNIQUE,
	name        TEXT NOT NULL,
	sector      TEXT NOT NULL,
	base_price  DOUBLE PRECISION NOT NULL,
	current_price DOUBLE PRECISION NOT NULL,
	tick_size   DOUBLE PRECISION NOT NULL,
	volatility  DOUBLE PRECISION NOT NULL,
	is_stress   BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS orders (
	id             BIGINT PRIMARY KEY,
	symbol_locate  SMALLINT NOT NULL,
	side           CHAR(1) NOT NULL,
	price          DOUBLE PRECISION NOT NULL,
	shares         INTEGER NOT NULL,
	priority       INTEGER NOT NULL DEFAULT 0,
	mpid           TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_orders_locate ON orders(symbol_locate);

CREATE TABLE IF NOT EXISTS trades (
	match_number   BIGINT PRIMARY KEY,
	symbol_locate  SMALLINT NOT NULL,
	ticker         TEXT NOT NULL,
	price          DOUBLE PRECISION NOT NULL,
	shares         INTEGER NOT NULL,
	aggressor      CHAR(1) NOT NULL,
	executed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_trades_locate_time ON trades(symbol_locate, executed_at);

CREATE TABLE IF NOT EXISTS sim_state (
	key         TEXT PRIMARY KEY,
	value_bytes BYTEA,
	value_int   BIGINT,
	value_time  TIMESTAMPTZ,
	updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`
	_, err := pool.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	log.Println("PostgreSQL tables ensured")
	return nil
}
