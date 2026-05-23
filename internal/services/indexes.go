package services

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// EnsureIndexes creates all required MongoDB indexes for optimal query performance.
// Safe to call on every startup — MongoDB skips creation if the index already exists.
func EnsureIndexes(ctx context.Context, db *mongo.Database) {
	indexCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	logs := db.Collection("logs")
	incidents := db.Collection("incidents")

	// ── logs collection indexes ───────────────────────────────────────────────
	logIndexes := []mongo.IndexModel{
		// Timestamp descending — primary sort for most queries
		{
			Keys:    bson.D{{Key: "timestamp", Value: -1}},
			Options: options.Index().SetName("idx_logs_timestamp"),
		},
		// Status — incident detector error count queries
		{
			Keys:    bson.D{{Key: "status", Value: 1}},
			Options: options.Index().SetName("idx_logs_status"),
		},
		// Service + environment + timestamp — compound for sliding-window queries
		{
			Keys: bson.D{
				{Key: "service", Value: 1},
				{Key: "environment", Value: 1},
				{Key: "timestamp", Value: -1},
			},
			Options: options.Index().SetName("idx_logs_service_env_ts"),
		},
		// Service + status + timestamp — error rate queries
		{
			Keys: bson.D{
				{Key: "service", Value: 1},
				{Key: "status", Value: 1},
				{Key: "timestamp", Value: -1},
			},
			Options: options.Index().SetName("idx_logs_service_status_ts"),
		},
		// Endpoint — for endpoint-level metrics grouping
		{
			Keys:    bson.D{{Key: "endpoint", Value: 1}},
			Options: options.Index().SetName("idx_logs_endpoint"),
		},
	}

	_, err := logs.Indexes().CreateMany(indexCtx, logIndexes)
	if err != nil {
		log.Warn().Err(err).Msg("Log index creation failed (may already exist)")
	} else {
		log.Info().Msg("✅ Logs collection indexes ensured")
	}

	// ── incidents collection indexes ──────────────────────────────────────────
	incidentIndexes := []mongo.IndexModel{
		// Active incident dedup query — used by detector on every log
		{
			Keys: bson.D{
				{Key: "service", Value: 1},
				{Key: "environment", Value: 1},
				{Key: "cause", Value: 1},
				{Key: "resolved", Value: 1},
			},
			Options: options.Index().SetName("idx_incidents_dedup"),
		},
		// List queries sorted by newest first
		{
			Keys:    bson.D{{Key: "created_at", Value: -1}},
			Options: options.Index().SetName("idx_incidents_created_at"),
		},
		// Resolve filter
		{
			Keys:    bson.D{{Key: "resolved", Value: 1}},
			Options: options.Index().SetName("idx_incidents_resolved"),
		},
	}

	_, err = incidents.Indexes().CreateMany(indexCtx, incidentIndexes)
	if err != nil {
		log.Warn().Err(err).Msg("Incident index creation failed (may already exist)")
	} else {
		log.Info().Msg("✅ Incidents collection indexes ensured")
	}
}
