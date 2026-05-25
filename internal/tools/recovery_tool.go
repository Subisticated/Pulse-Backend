package tools

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// RecoveryTool polls MongoDB until the service error rate drops below threshold
// or the timeout is exceeded.
type RecoveryTool struct {
	db *mongo.Database
}

func NewRecoveryTool(db *mongo.Database) *RecoveryTool {
	return &RecoveryTool{db: db}
}

func (t *RecoveryTool) Name() string { return "monitor_recovery" }
func (t *RecoveryTool) Description() string {
	return "Poll service metrics until error rate recovers below 5% or timeout (3 min)"
}

func (t *RecoveryTool) Run(ctx context.Context, params map[string]interface{}) (string, error) {
	service, _ := params["service"].(string)
	incidentID, _ := params["incidentId"].(string)

	timeout := 3 * time.Minute
	pollInterval := 10 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "Recovery monitor cancelled.", nil
		case <-time.After(pollInterval):
		}

		rate, total, err := t.currentErrorRate(ctx, service)
		if err != nil {
			continue
		}

		if total == 0 {
			continue
		}

		if rate < 5.0 {
			// Mark the incident resolved if ID is present
			if incidentID != "" {
				t.resolveIncident(ctx, incidentID)
			}
			return fmt.Sprintf(
				"✅ Service '%s' recovered! Error rate dropped to %.1f%% (%d requests sampled).",
				service, rate, total), nil
		}
	}

	return fmt.Sprintf(
		"⏱️  Recovery timeout after 3 minutes. Service '%s' still unhealthy. Manual intervention recommended.",
		service), nil
}

func (t *RecoveryTool) currentErrorRate(ctx context.Context, service string) (float64, int64, error) {
	since := time.Now().Add(-2 * time.Minute)
	filter := bson.M{
		"service":   service,
		"timestamp": bson.M{"$gte": since},
	}
	total, err := t.db.Collection("logs").CountDocuments(ctx, filter)
	if err != nil {
		return 0, 0, err
	}
	filter["status"] = bson.M{"$gte": 500}
	errors, err := t.db.Collection("logs").CountDocuments(ctx, filter)
	if err != nil {
		return 0, 0, err
	}
	if total == 0 {
		return 0, 0, nil
	}
	return float64(errors) / float64(total) * 100, total, nil
}

func (t *RecoveryTool) resolveIncident(ctx context.Context, idStr string) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return
	}
	now := time.Now()
	_, _ = t.db.Collection("incidents").UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"resolved":    true,
			"status":      "resolved",
			"resolved_at": now,
		}},
	)
}
