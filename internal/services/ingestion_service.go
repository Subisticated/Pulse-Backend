package services

import (
	"context"
	"time"

	"pulse-backend/internal/detector"
	"pulse-backend/internal/models"
	"pulse-backend/internal/queue"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// IngestionService is a thin wrapper that wires the LogQueue with the DB and detector.
// The heavy lifting (batching, InsertMany, incident detection) is inside queue.LogQueue.
type IngestionService struct {
	Queue *queue.LogQueue
}

// NewIngestionService creates the async log queue with batch DB writes and incident detection wired in.
func NewIngestionService(db *mongo.Database, det *detector.IncidentDetector) *IngestionService {
	q := queue.New(db, det.AnalyzeLog)
	return &IngestionService{Queue: q}
}

// IngestLog is kept for backward compatibility with any direct service callers.
// Prefer using the Queue field directly for maximum throughput.
func (s *IngestionService) IngestLog(_ context.Context, event *models.LogEvent) (*models.LogEvent, error) {
	if event.ID.IsZero() {
		event.ID = primitive.NewObjectID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	s.Queue.Enqueue(*event)
	return event, nil
}
