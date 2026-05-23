package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LogEvent represents an ingested HTTP request/response log
type LogEvent struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Endpoint    string             `bson:"endpoint" json:"endpoint" binding:"required"`
	Method      string             `bson:"method" json:"method" binding:"required"`
	Status      int                `bson:"status" json:"status" binding:"required"`
	Latency     int                `bson:"latency" json:"latency" binding:"required"` // In milliseconds
	Error       string             `bson:"error,omitempty" json:"error,omitempty"`
	Service     string             `bson:"service" json:"service" binding:"required"`
	Environment string             `bson:"environment" json:"environment" binding:"required"`
	Timestamp   time.Time          `bson:"timestamp" json:"timestamp"`
}
