package services

import (
	"context"
	"time"

	"pulse-backend/internal/models"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// IncidentService manages incident tracking operations
type IncidentService struct {
	db *mongo.Database
}

// NewIncidentService constructs an IncidentService
func NewIncidentService(db *mongo.Database) *IncidentService {
	return &IncidentService{db: db}
}

// GetIncidents queries all incidents, optionally filtered by resolved status.
// ?resolved=true | ?resolved=false — omit param for all.
func (s *IncidentService) GetIncidents(ctx context.Context, resolvedFilter string) ([]models.Incident, error) {
	filter := bson.M{}
	if resolvedFilter == "true" {
		filter["resolved"] = true
	} else if resolvedFilter == "false" {
		filter["resolved"] = false
	}

	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}})
	cur, err := s.db.Collection("incidents").Find(ctx, filter, opts)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query incidents")
		return nil, err
	}
	defer cur.Close(ctx)

	var incidents []models.Incident
	if err = cur.All(ctx, &incidents); err != nil {
		return nil, err
	}
	return incidents, nil
}

// GetIncidentByID fetches a single incident by ObjectID hex string
func (s *IncidentService) GetIncidentByID(ctx context.Context, idStr string) (*models.Incident, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, err
	}
	var incident models.Incident
	err = s.db.Collection("incidents").FindOne(ctx, bson.M{"_id": id}).Decode(&incident)
	if err != nil {
		return nil, err
	}
	return &incident, nil
}

// ResolveIncident sets resolved=true and records the resolved timestamp
func (s *IncidentService) ResolveIncident(ctx context.Context, idStr string) (*models.Incident, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"resolved":    true,
			"resolved_at": now,
		},
	}

	after := options.After
	opts := options.FindOneAndUpdate().SetReturnDocument(after)

	var updated models.Incident
	err = s.db.Collection("incidents").FindOneAndUpdate(ctx, bson.M{"_id": id}, update, opts).Decode(&updated)
	if err != nil {
		log.Error().Err(err).Str("id", idStr).Msg("Failed to resolve incident")
		return nil, err
	}
	return &updated, nil
}

// GetRecentLogsForService fetches the N most recent logs for a service+env pair
func (s *IncidentService) GetRecentLogsForService(ctx context.Context, service, env string, limit int64) ([]models.LogEvent, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit)

	cur, err := s.db.Collection("logs").Find(ctx, bson.M{
		"service":     service,
		"environment": env,
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var logs []models.LogEvent
	if err = cur.All(ctx, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}
