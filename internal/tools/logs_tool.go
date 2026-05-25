package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// LogsTool fetches recent failing logs from MongoDB for a given service
type LogsTool struct {
	db *mongo.Database
}

func NewLogsTool(db *mongo.Database) *LogsTool {
	return &LogsTool{db: db}
}

func (t *LogsTool) Name() string { return "fetch_logs" }
func (t *LogsTool) Description() string {
	return "Fetch recent error logs for a service to identify failure patterns"
}

func (t *LogsTool) Run(ctx context.Context, params map[string]interface{}) (string, error) {
	service, _ := params["service"].(string)
	since := time.Now().Add(-10 * time.Minute)

	filter := bson.M{"timestamp": bson.M{"$gte": since}}
	if service != "" {
		filter["service"] = service
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(20)

	cur, err := t.db.Collection("logs").Find(ctx, filter, opts)
	if err != nil {
		return "", fmt.Errorf("logs query failed: %w", err)
	}
	defer cur.Close(ctx)

	var rows []bson.M
	if err = cur.All(ctx, &rows); err != nil {
		return "", err
	}

	if len(rows) == 0 {
		return "No recent logs found for this service.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Recent logs for service '%s' (last 10 min):\n", service))
	for _, row := range rows {
		method, _ := row["method"].(string)
		endpoint, _ := row["endpoint"].(string)
		errMsg, _ := row["error"].(string)
		latency := toInt(row["latency"])
		status := toInt(row["status"])

		line := fmt.Sprintf("  [%s] %s %s → %d (%dms)", statusIcon(status), method, endpoint, status, latency)
		if errMsg != "" {
			line += fmt.Sprintf(" | err=%q", errMsg)
		}
		sb.WriteString(line + "\n")
	}

	// Summary stats
	var errors, total int
	for _, row := range rows {
		total++
		if toInt(row["status"]) >= 500 {
			errors++
		}
	}
	sb.WriteString(fmt.Sprintf("\nSummary: %d total | %d errors (%.1f%% error rate)\n", total, errors, float64(errors)/float64(total)*100))

	return sb.String(), nil
}

func statusIcon(s int) string {
	if s >= 500 {
		return "🔴"
	} else if s >= 400 {
		return "🟡"
	}
	return "🟢"
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int32:
		return int(val)
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}
