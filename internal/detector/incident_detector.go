package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"pulse-backend/internal/alerts"
	"pulse-backend/internal/models"
	"pulse-backend/internal/wsocket"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// Rule thresholds
const (
	errorCountThreshold    = 5               // >5 failures within window
	errorWindowDuration    = 5 * time.Minute // sliding window
	latencyThresholdMs     = 1000            // >1000ms average latency
	errorRateThreshold     = 10.0            // >10% error rate
)

// IncidentDetector performs rule-based anomaly checks on ingested logs
type IncidentDetector struct {
	db  *mongo.Database
	hub *wsocket.Hub
}

// NewIncidentDetector creates a new IncidentDetector
func NewIncidentDetector(db *mongo.Database, hub *wsocket.Hub) *IncidentDetector {
	return &IncidentDetector{db: db, hub: hub}
}

// wsEvent is broadcast to all WebSocket clients when a new incident is created
type wsEvent struct {
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Cause    string `json:"cause"`
	Service  string `json:"service"`
}

// AnalyzeLog evaluates an ingested log against all detection rules
func (d *IncidentDetector) AnalyzeLog(ctx context.Context, event *models.LogEvent) {
	// Rule 1: >5 status>=500 errors within the last 5 minutes
	d.checkErrorCountRule(ctx, event)

	// Rule 2: current log has latency >1000ms
	if event.Latency > latencyThresholdMs {
		d.createIncidentIfNew(ctx, event, "latency_spike", "Medium",
			fmt.Sprintf("Latency spike on %s %s: %dms (threshold: %dms)",
				event.Method, event.Endpoint, event.Latency, latencyThresholdMs))
	}

	// Rule 3: error rate >10% in last 5 minutes
	d.checkErrorRateRule(ctx, event)
}

// checkErrorCountRule triggers an incident if >5 HTTP 5xx errors occur within 5 minutes
func (d *IncidentDetector) checkErrorCountRule(ctx context.Context, event *models.LogEvent) {
	if event.Status < 500 {
		return
	}

	logs := d.db.Collection("logs")
	windowStart := time.Now().Add(-errorWindowDuration)

	count, err := logs.CountDocuments(ctx, bson.M{
		"service":     event.Service,
		"environment": event.Environment,
		"status":      bson.M{"$gte": 500},
		"timestamp":   bson.M{"$gte": windowStart},
	})
	if err != nil {
		log.Error().Err(err).Msg("Error rate count query failed")
		return
	}

	if count > errorCountThreshold {
		d.createIncidentIfNew(ctx, event, "high_error_rate", "Critical",
			fmt.Sprintf("%d HTTP 5xx errors in the last 5 minutes on service '%s' (threshold: >%d)",
				count, event.Service, errorCountThreshold))
	}
}

// checkErrorRateRule triggers an incident if the error rate > 10% in the last 5 minutes
func (d *IncidentDetector) checkErrorRateRule(ctx context.Context, event *models.LogEvent) {
	logs := d.db.Collection("logs")
	windowStart := time.Now().Add(-errorWindowDuration)
	windowFilter := bson.M{
		"service":     event.Service,
		"environment": event.Environment,
		"timestamp":   bson.M{"$gte": windowStart},
	}

	total, err := logs.CountDocuments(ctx, windowFilter)
	if err != nil || total == 0 {
		return
	}

	windowFilter["status"] = bson.M{"$gte": 500}
	errors, err := logs.CountDocuments(ctx, windowFilter)
	if err != nil {
		return
	}

	rate := float64(errors) / float64(total) * 100
	if rate > errorRateThreshold {
		d.createIncidentIfNew(ctx, event, "high_error_percentage", "Critical",
			fmt.Sprintf("Error rate %.1f%% in last 5 minutes on service '%s' (threshold: >%.0f%%)",
				rate, event.Service, errorRateThreshold))
	}
}

// createIncidentIfNew inserts a new incident only if no active one of the same cause exists.
// After insert it broadcasts via WebSocket and fires Discord alerts for Critical severity.
func (d *IncidentDetector) createIncidentIfNew(ctx context.Context, event *models.LogEvent, cause, severity, description string) {
	incidents := d.db.Collection("incidents")

	// Deduplicate: skip if an active incident of same cause+service+env exists
	var existing models.Incident
	err := incidents.FindOne(ctx, bson.M{
		"service":     event.Service,
		"environment": event.Environment,
		"cause":       cause,
		"resolved":    false,
	}).Decode(&existing)
	if err == nil {
		return // already active
	}
	if err != mongo.ErrNoDocuments {
		log.Error().Err(err).Msg("Incident dedup query failed")
		return
	}

	newIncident := models.Incident{
		ID:          primitive.NewObjectID(),
		Severity:    severity,
		Cause:       cause,
		Description: description,
		Service:     event.Service,
		Environment: event.Environment,
		RelatedLogs: []primitive.ObjectID{event.ID},
		Resolved:    false,
		CreatedAt:   time.Now(),
	}

	if _, err = incidents.InsertOne(ctx, newIncident); err != nil {
		log.Error().Err(err).Msg("Failed to insert new incident")
		return
	}

	log.Warn().
		Str("severity", severity).
		Str("cause", cause).
		Str("service", event.Service).
		Msg("🚨 Incident created")

	// Broadcast WebSocket event
	if d.hub != nil {
		evt := wsEvent{Type: "incident", Severity: severity, Cause: cause, Service: event.Service}
		if payload, err := json.Marshal(evt); err == nil {
			d.hub.Broadcast(payload)
		}
	}

	// Discord alert for Critical incidents
	if severity == "Critical" {
		go alerts.SendDiscordAlert(severity, cause, description, event.Service, event.Environment)
	}
}
