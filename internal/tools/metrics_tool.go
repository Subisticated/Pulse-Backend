package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// MetricsTool fetches a real-time metrics snapshot for a service
type MetricsTool struct {
	db *mongo.Database
}

func NewMetricsTool(db *mongo.Database) *MetricsTool {
	return &MetricsTool{db: db}
}

func (t *MetricsTool) Name() string { return "fetch_metrics" }
func (t *MetricsTool) Description() string {
	return "Fetch latency, error rate, and traffic metrics for a service in the last 5 minutes"
}

func (t *MetricsTool) Run(ctx context.Context, params map[string]interface{}) (string, error) {
	service, _ := params["service"].(string)
	since := time.Now().Add(-5 * time.Minute)

	matchFilter := bson.D{{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}}}
	if service != "" {
		matchFilter = append(matchFilter, bson.E{Key: "service", Value: service})
	}

	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchFilter}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "errors", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$gte", Value: bson.A{"$status", 500}}},
					1, 0,
				}},
			}}}},
			{Key: "avgLatency", Value: bson.D{{Key: "$avg", Value: "$latency"}}},
			{Key: "maxLatency", Value: bson.D{{Key: "$max", Value: "$latency"}}},
		}}},
	}

	cur, err := t.db.Collection("logs").Aggregate(ctx, pipeline)
	if err != nil {
		return "", fmt.Errorf("metrics aggregation failed: %w", err)
	}
	defer cur.Close(ctx)

	var rows []bson.M
	if err = cur.All(ctx, &rows); err != nil || len(rows) == 0 {
		return fmt.Sprintf("No metrics data for service '%s' in the last 5 minutes.", service), nil
	}

	row := rows[0]
	total := toInt64M(row["total"])
	errors := toInt64M(row["errors"])
	avgLat := toFloatM(row["avgLatency"])
	maxLat := toFloatM(row["maxLatency"])

	errorRate := float64(0)
	if total > 0 {
		errorRate = float64(errors) / float64(total) * 100
	}
	rps := float64(total) / 300.0 // 5 minutes window

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Metrics for '%s' (last 5 min):\n", service))
	sb.WriteString(fmt.Sprintf("  Total requests : %d\n", total))
	sb.WriteString(fmt.Sprintf("  Error count    : %d\n", errors))
	sb.WriteString(fmt.Sprintf("  Error rate     : %.1f%%\n", errorRate))
	sb.WriteString(fmt.Sprintf("  Avg latency    : %.0fms\n", avgLat))
	sb.WriteString(fmt.Sprintf("  Max latency    : %.0fms\n", maxLat))
	sb.WriteString(fmt.Sprintf("  RPS            : %.2f\n", rps))

	// Health assessment
	if errorRate > 10 {
		sb.WriteString("  ⚠️  Health: CRITICAL — error rate exceeds 10%\n")
	} else if errorRate > 5 {
		sb.WriteString("  ⚠️  Health: DEGRADED — error rate exceeds 5%\n")
	} else if avgLat > 1000 {
		sb.WriteString("  ⚠️  Health: DEGRADED — latency exceeds 1000ms\n")
	} else {
		sb.WriteString("  ✅  Health: HEALTHY\n")
	}

	return sb.String(), nil
}

func toInt64M(v interface{}) int64 {
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

func toFloatM(v interface{}) float64 {
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
