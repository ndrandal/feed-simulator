package persist

import (
	"context"
	"fmt"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// EnsureIndexes creates idempotent indexes on all collections.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	type idx struct {
		collection string
		model      mongo.IndexModel
	}

	indexes := []idx{
		{
			collection: "symbols",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "locate_code", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "symbols",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "ticker", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "orders",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "id", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "orders",
			model: mongo.IndexModel{
				Keys: bson.D{{Key: "symbol_locate", Value: 1}},
			},
		},
		{
			collection: "sim_state",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "key", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "trades",
			model: mongo.IndexModel{
				Keys:    bson.D{{Key: "match_number", Value: 1}},
				Options: options.Index().SetUnique(true),
			},
		},
		{
			collection: "trades",
			model: mongo.IndexModel{
				Keys: bson.D{
					{Key: "symbol_locate", Value: 1},
					{Key: "executed_at", Value: -1},
				},
			},
		},
	}

	for _, i := range indexes {
		_, err := db.Collection(i.collection).Indexes().CreateOne(ctx, i.model)
		if err != nil {
			return fmt.Errorf("create index on %s: %w", i.collection, err)
		}
	}

	log.Println("MongoDB indexes ensured")
	return nil
}
