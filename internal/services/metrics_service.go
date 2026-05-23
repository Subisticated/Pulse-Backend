package services

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ServiceMetrics represents performance statistics for a specific service
type ServiceMetrics struct {
	Name          string  `json:"name"`
	TotalRequests int64   `json:"totalRequests"`
	AvgLatency    float64 `json:"avgLatency"` // in milliseconds
	ErrorRate     float64 `json:"errorRate"`  // percentage, e.g. 2.5
}

// MetricsResponse matches the complete dashboard requirements
type MetricsResponse struct {
	TotalRequests    int64            `json:"totalRequests"`
	ErrorRate        float64          `json:"errorRate"`        // percentage, e.g. 4.0
	AvgLatency       float64          `json:"avgLatency"`       // in milliseconds
	RequestsLastHour int64            `json:"requestsLastHour"`
	ErrorsLastHour   int64            `json:"errorsLastHour"`
	Services         []ServiceMetrics `json:"services"`
}

// MetricsService aggregates log data for performance insights
type MetricsService struct {
	db *mongo.Database
}

// NewMetricsService instantiates a MetricsService
func NewMetricsService(db *mongo.Database) *MetricsService {
	return &MetricsService{db: db}
}

// GetMetrics runs optimized MongoDB aggregations to fetch overall and service-level performance metrics
func (s *MetricsService) GetMetrics(ctx context.Context) (*MetricsResponse, error) {
	col := s.db.Collection("logs")

	// ── 1. Overall aggregation pipeline ───────────────────────────────────────
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "avgLatency", Value: bson.D{{Key: "$avg", Value: "$latency"}}},
			{Key: "errors", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$gte", Value: bson.A{"$status", 500}}},
					1, 0,
				}},
			}}}},
		}}},
	}

	cur, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		log.Error().Err(err).Msg("Metrics overall aggregation failed")
		return nil, err
	}
	defer cur.Close(ctx)

	var overall []bson.M
	if err = cur.All(ctx, &overall); err != nil {
		return nil, err
	}

	var totalRequests, totalErrors int64
	var avgLatency float64

	if len(overall) > 0 {
		totalRequests = toInt64(overall[0]["total"])
		avgLatency = toFloat64(overall[0]["avgLatency"])
		totalErrors = toInt64(overall[0]["errors"])
	}

	var errorRate float64
	if totalRequests > 0 {
		errorRate = float64(totalErrors) / float64(totalRequests) * 100
	}

	// ── 2. Service-level breakdown aggregation ─────────────────────────────────
	servicePipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$service"},
			{Key: "totalRequests", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "avgLatency", Value: bson.D{{Key: "$avg", Value: "$latency"}}},
			{Key: "errors", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$gte", Value: bson.A{"$status", 500}}},
					1, 0,
				}},
			}}}},
		}}},
	}

	srvCur, err := col.Aggregate(ctx, servicePipeline)
	var servicesList []ServiceMetrics
	if err == nil {
		defer srvCur.Close(ctx)
		var srvResults []bson.M
		if err = srvCur.All(ctx, &srvResults); err == nil {
			for _, item := range srvResults {
				name, ok := item["_id"].(string)
				if !ok || name == "" {
					name = "unknown"
				}
				sReqs := toInt64(item["totalRequests"])
				sLatency := toFloat64(item["avgLatency"])
				sErrors := toInt64(item["errors"])

				sErrRate := float64(0)
				if sReqs > 0 {
					sErrRate = float64(sErrors) / float64(sReqs) * 100
				}

				servicesList = append(servicesList, ServiceMetrics{
					Name:          name,
					TotalRequests: sReqs,
					AvgLatency:    roundTwo(sLatency),
					ErrorRate:     roundTwo(sErrRate),
				})
			}
		}
	} else {
		log.Warn().Err(err).Msg("Metrics service-level aggregation failed")
	}

	// ── 3. Last-hour stats (Optimized Count) ──────────────────────────────────
	hourAgo := time.Now().Add(-time.Hour)
	hourFilter := bson.M{"timestamp": bson.M{"$gte": hourAgo}}

	requestsLastHour, err := col.CountDocuments(ctx, hourFilter)
	if err != nil {
		log.Warn().Err(err).Msg("Could not count requests in last hour")
	}

	hourFilter["status"] = bson.M{"$gte": 500}
	errorsLastHour, err := col.CountDocuments(ctx, hourFilter)
	if err != nil {
		log.Warn().Err(err).Msg("Could not count errors in last hour")
	}

	return &MetricsResponse{
		TotalRequests:    totalRequests,
		ErrorRate:        roundTwo(errorRate),
		AvgLatency:       roundTwo(avgLatency),
		RequestsLastHour: requestsLastHour,
		ErrorsLastHour:   errorsLastHour,
		Services:         servicesList,
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int32:
		return int64(val)
	case int64:
		return val
	case float64:
		return int64(val)
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int32:
		return float64(val)
	case int64:
		return float64(val)
	}
	return 0
}

func roundTwo(f float64) float64 {
	return float64(int(f*100)) / 100
}
