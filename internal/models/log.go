package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LogEvent stores a single API request telemetry entry
type LogEvent struct {
	ID          primitive.ObjectID     `bson:"_id,omitempty"  json:"id,omitempty"`
	Endpoint    string                 `bson:"endpoint"       json:"endpoint"    binding:"required"`
	Method      string                 `bson:"method"         json:"method"      binding:"required"`
	Status      int                    `bson:"status"         json:"status"      binding:"required"`
	Latency     int                    `bson:"latency"        json:"latency"`    // ms — primary field
	LatencyMs   int                    `bson:"latency_ms"     json:"latencyMs"`  // SDK alias (latencyMs)
	Error       string                 `bson:"error"          json:"error"`
	Service     string                 `bson:"service"        json:"service"`
	Environment string                 `bson:"environment"    json:"environment"`
	TraceID     string                 `bson:"trace_id"       json:"traceId"`    // optional distributed trace
	Meta        map[string]interface{} `bson:"meta"           json:"meta"`       // arbitrary SDK extra fields
	Timestamp   time.Time              `bson:"timestamp"      json:"timestamp"`
}
