package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"pulse-backend/internal/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// IncidentsTool fetches historical incidents and can create new ones
type IncidentsTool struct {
	db *mongo.Database
}

func NewIncidentsTool(db *mongo.Database) *IncidentsTool {
	return &IncidentsTool{db: db}
}

func (t *IncidentsTool) Name() string { return "fetch_incidents" }
func (t *IncidentsTool) Description() string {
	return "Fetch recent incident history for a service to identify recurring patterns"
}

func (t *IncidentsTool) Run(ctx context.Context, params map[string]interface{}) (string, error) {
	service, _ := params["service"].(string)

	filter := bson.M{}
	if service != "" {
		filter["service"] = service
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(5)

	cur, err := t.db.Collection("incidents").Find(ctx, filter, opts)
	if err != nil {
		return "", fmt.Errorf("incident query failed: %w", err)
	}
	defer cur.Close(ctx)

	var incidents []models.Incident
	if err = cur.All(ctx, &incidents); err != nil {
		return "", err
	}

	if len(incidents) == 0 {
		return fmt.Sprintf("No historical incidents found for service '%s'.", service), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Historical incidents for '%s' (last 5):\n", service))
	for _, inc := range incidents {
		status := "active"
		if inc.Resolved {
			status = "resolved"
		}
		age := time.Since(inc.StartTime).Round(time.Minute)
		sb.WriteString(fmt.Sprintf("  [%s] %s — %s — %s ago\n",
			inc.Severity, inc.Cause, status, age))
		if inc.Description != "" {
			sb.WriteString(fmt.Sprintf("    Detail: %s\n", inc.Description))
		}
	}

	// Count recurring patterns
	causeCount := map[string]int{}
	for _, inc := range incidents {
		causeCount[inc.Cause]++
	}
	sb.WriteString("\nRecurring patterns:\n")
	for cause, count := range causeCount {
		if count > 1 {
			sb.WriteString(fmt.Sprintf("  ⚠️  '%s' recurred %dx — possible systemic issue\n", cause, count))
		}
	}

	return sb.String(), nil
}

// CreateIncident inserts a new agent-created incident into MongoDB
func (t *IncidentsTool) CreateIncident(ctx context.Context, service, env, cause, severity, description string) (string, error) {
	now := time.Now()
	inc := models.Incident{
		ID:          primitive.NewObjectID(),
		Title:       fmt.Sprintf("Agent: %s on %s", cause, service),
		Severity:    severity,
		Cause:       cause,
		Description: description,
		Service:     service,
		Services:    []string{service},
		Environment: env,
		Resolved:    false,
		Status:      "active",
		StartTime:   now,
		Links: &models.IncidentLinks{
			RCA: "/api/v1/rca",
		},
	}

	_, err := t.db.Collection("incidents").InsertOne(ctx, inc)
	if err != nil {
		return "", fmt.Errorf("failed to create incident: %w", err)
	}
	return fmt.Sprintf("Incident created: %s (id: %s)", description, inc.ID.Hex()), nil
}
