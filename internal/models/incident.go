package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Incident represents a detected anomaly or service outage
type Incident struct {
	ID          primitive.ObjectID   `bson:"_id,omitempty"       json:"id,omitempty"`
	Severity    string               `bson:"severity"            json:"severity"`    // Low, Medium, Critical
	Cause       string               `bson:"cause"               json:"cause"`       // high_error_rate, latency_spike, high_error_percentage
	Description string               `bson:"description"         json:"description"`
	Service     string               `bson:"service"             json:"service"`
	Environment string               `bson:"environment"         json:"environment"`
	RelatedLogs []primitive.ObjectID `bson:"related_logs"        json:"related_logs"`
	Resolved    bool                 `bson:"resolved"            json:"resolved"`
	CreatedAt   time.Time            `bson:"created_at"          json:"createdAt"`
	ResolvedAt  *time.Time           `bson:"resolved_at,omitempty" json:"resolvedAt,omitempty"`
}
