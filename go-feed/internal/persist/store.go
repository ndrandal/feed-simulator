package persist

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps the PostgreSQL connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore connects to PostgreSQL and returns a Store.
func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	log.Println("connected to PostgreSQL")
	return &Store{pool: pool}, nil
}

// Close shuts down the connection pool.
func (s *Store) Close(_ context.Context) {
	s.pool.Close()
}

// Pool returns the underlying pgxpool.Pool.
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Migrate creates tables and indexes.
func (s *Store) Migrate(ctx context.Context) error {
	return CreateTables(ctx, s.pool)
}
