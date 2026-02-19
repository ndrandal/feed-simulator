package persist

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Store wraps the MongoDB client and database.
type Store struct {
	client *mongo.Client
	db     *mongo.Database
}

// NewStore connects to MongoDB and returns a Store.
// The URI should include the database name (e.g. mongodb://localhost:27017/feedsim).
// If no database is specified in the URI, "feedsim" is used.
func NewStore(ctx context.Context, uri string) (*Store, error) {
	clientOpts := options.Client().ApplyURI(uri)

	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("connect to mongodb: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("ping mongodb: %w", err)
	}

	// Extract database name from URI path, default to "feedsim".
	dbName := "feedsim"
	if u, err := url.Parse(uri); err == nil {
		if name := strings.TrimPrefix(u.Path, "/"); name != "" {
			dbName = name
		}
	}

	log.Printf("connected to MongoDB (db=%s)", dbName)
	return &Store{client: client, db: client.Database(dbName)}, nil
}

// Close disconnects from MongoDB.
func (s *Store) Close(ctx context.Context) {
	s.client.Disconnect(ctx)
}

// DB returns the underlying mongo.Database.
func (s *Store) DB() *mongo.Database {
	return s.db
}

// Client returns the underlying mongo.Client (needed for transactions).
func (s *Store) Client() *mongo.Client {
	return s.client
}

// Migrate creates indexes for all collections.
func (s *Store) Migrate(ctx context.Context) error {
	return EnsureIndexes(ctx, s.db)
}
