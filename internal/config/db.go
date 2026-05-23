package config

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ConnectDB initializes a MongoDB connection with a production-grade connection pool
func ConnectDB(cfg *Config) (*mongo.Client, *mongo.Database, error) {
	log.Info().Msg("Connecting to MongoDB with optimized connection pool...")

	maxPool := uint64(100)
	minPool := uint64(10)

	clientOptions := options.Client().
		ApplyURI(cfg.MongoURI).
		SetMaxPoolSize(maxPool).
		SetMinPoolSize(minPool).
		SetMaxConnIdleTime(5 * time.Minute).
		SetServerSelectionTimeout(30 * time.Second)

	// Use a long-enough context for Atlas SRV DNS resolution
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		log.Error().Err(err).Msg("Failed to connect to MongoDB")
		return nil, nil, err
	}

	// Verify the connection is live with a separate ping context
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer pingCancel()
	if err = client.Ping(pingCtx, nil); err != nil {
		log.Error().Err(err).Msg("Failed to ping MongoDB")
		return nil, nil, err
	}

	db := client.Database(cfg.DBName)
	log.Info().
		Str("database", cfg.DBName).
		Uint64("max_pool", maxPool).
		Uint64("min_pool", minPool).
		Msg("✅ MongoDB connected with connection pool")

	return client, db, nil
}
