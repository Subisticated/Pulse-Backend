package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Incident represents a detected anomaly or service outage
type Incident struct {
	ID          primitive.ObjectID   `bson:"_id,omitempty"         json:"id,omitempty"`
	Title       string               `bson:"title"                 json:"title"`       // Human-readable title for dashboard cards
	Severity    string               `bson:"severity"              json:"severity"`    // low, medium, critical
	Cause       string               `bson:"cause"                 json:"cause"`       // high_error_rate, latency_spike, high_error_percentage
	Description string               `bson:"description"           json:"detail"`      // mapped to "detail" for dashboard compat
	Service     string               `bson:"service"               json:"service"`
	Services    []string             `bson:"services"              json:"services"`    // all affected services (for dashboard coloring)
	Environment string               `bson:"environment"           json:"environment"`
	RelatedLogs []primitive.ObjectID `bson:"related_logs"          json:"related_logs"`
	Resolved    bool                 `bson:"resolved"              json:"resolved"`
	Status      string               `bson:"status"                json:"status"`       // "active" | "resolved"
	StartTime   time.Time            `bson:"created_at"            json:"startTime"`   // aliased to startTime for dashboard
	EndTime     *time.Time           `bson:"resolved_at,omitempty" json:"endTime"`     // null if still active
	Links       *IncidentLinks       `bson:"links,omitempty"       json:"links,omitempty"`
}

// IncidentLinks holds dashboard deep-link URLs for traces and RCA
type IncidentLinks struct {
	Trace string `bson:"trace" json:"trace,omitempty"`
	RCA   string `bson:"rca"   json:"rca,omitempty"`
}
