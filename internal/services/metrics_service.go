package services

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TimePoint is a single data point in a time-series
type TimePoint struct {
	T     int64   `json:"t"`     // Unix epoch ms
	Value float64 `json:"value"`
}

// ServiceStat is a per-service performance summary inside the metrics response
type ServiceStat struct {
	ErrorRate    float64 `json:"errorRate"`
	AvgLatencyMs float64 `json:"avgLatencyMs"`
}

// MetricsResponse matches the complete dashboard requirements
type MetricsResponse struct {
	Start            time.Time              `json:"start"`
	End              time.Time              `json:"end"`
	RPS              float64                `json:"rps"`
	RPM              float64                `json:"rpm"`
	TotalRequests    int64                  `json:"totalRequests"`
	ErrorRate        float64                `json:"errorRate"`
	AvgLatencyMs     float64                `json:"avgLatencyMs"`
	P95LatencyMs     float64                `json:"p95LatencyMs"`
	RequestsLastHour int64                  `json:"requestsLastHour"`
	ErrorsLastHour   int64                  `json:"errorsLastHour"`
	LatencySeries    []TimePoint            `json:"latencySeries"`
	ErrorSeries      []TimePoint            `json:"errorSeries"`
	ByService        map[string]ServiceStat `json:"byService"`
}

// MetricsService aggregates log data for performance insights
type MetricsService struct {
	db *mongo.Database
}

// NewMetricsService instantiates a MetricsService
func NewMetricsService(db *mongo.Database) *MetricsService {
	return &MetricsService{db: db}
}

// GetMetrics runs optimized MongoDB aggregations. start/end are optional (defaults: last 5 minutes).
func (s *MetricsService) GetMetrics(ctx context.Context, start, end time.Time) (*MetricsResponse, error) {
	col := s.db.Collection("logs")

	if start.IsZero() {
		start = time.Now().Add(-5 * time.Minute)
	}
	if end.IsZero() {
		end = time.Now()
	}
	windowSec := end.Sub(start).Seconds()
	if windowSec <= 0 {
		windowSec = 300
	}

	matchStage := bson.D{{Key: "$match", Value: bson.D{
		{Key: "timestamp", Value: bson.D{
			{Key: "$gte", Value: start},
			{Key: "$lte", Value: end},
		}},
	}}}

	// ── 1. Overall stats ──────────────────────────────────────────────────────
	overallPipeline := mongo.Pipeline{
		matchStage,
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

	cur, err := col.Aggregate(ctx, overallPipeline)
	if err != nil {
		log.Error().Err(err).Msg("Metrics overall aggregation failed")
		return nil, err
	}
	var overall []bson.M
	if err = cur.All(ctx, &overall); err != nil {
		return nil, err
	}
	cur.Close(ctx)

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
	rps := float64(totalRequests) / windowSec
	rpm := rps * 60

	// ── 2. P95 latency (bucket approximation) ─────────────────────────────────
	p95 := s.calcP95(ctx, col, matchStage)

	// ── 3. Per-service breakdown ───────────────────────────────────────────────
	byService := s.calcByService(ctx, col, matchStage)

	// ── 4. Time-series (1-minute buckets) ─────────────────────────────────────
	latencySeries, errorSeries := s.calcTimeSeries(ctx, col, start, end)

	// ── 5. Last-hour counts ───────────────────────────────────────────────────
	hourAgo := time.Now().Add(-time.Hour)
	hourFilter := bson.M{"timestamp": bson.M{"$gte": hourAgo}}
	requestsLastHour, _ := col.CountDocuments(ctx, hourFilter)
	hourFilter["status"] = bson.M{"$gte": 500}
	errorsLastHour, _ := col.CountDocuments(ctx, hourFilter)

	return &MetricsResponse{
		Start:            start,
		End:              end,
		RPS:              roundTwo(rps),
		RPM:              roundTwo(rpm),
		TotalRequests:    totalRequests,
		ErrorRate:        roundTwo(errorRate),
		AvgLatencyMs:     roundTwo(avgLatency),
		P95LatencyMs:     roundTwo(p95),
		RequestsLastHour: requestsLastHour,
		ErrorsLastHour:   errorsLastHour,
		LatencySeries:    latencySeries,
		ErrorSeries:      errorSeries,
		ByService:        byService,
	}, nil
}

func (s *MetricsService) calcP95(ctx context.Context, col *mongo.Collection, match bson.D) float64 {
	pipeline := mongo.Pipeline{
		match,
		{{Key: "$sort", Value: bson.D{{Key: "latency", Value: 1}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: nil},
			{Key: "latencies", Value: bson.D{{Key: "$push", Value: "$latency"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$project", Value: bson.D{
			{Key: "p95idx", Value: bson.D{{Key: "$floor", Value: bson.D{
				{Key: "$multiply", Value: bson.A{0.95, "$count"}},
			}}}},
			{Key: "latencies", Value: 1},
			{Key: "count", Value: 1},
		}}},
	}

	cur, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		return 0
	}
	defer cur.Close(ctx)

	var rows []bson.M
	if err = cur.All(ctx, &rows); err != nil || len(rows) == 0 {
		return 0
	}

	row := rows[0]
	idx := int(toFloat64(row["p95idx"]))
	latencies, ok := row["latencies"].(bson.A)
	if !ok || idx >= len(latencies) {
		return 0
	}
	return toFloat64(latencies[idx])
}

func (s *MetricsService) calcByService(ctx context.Context, col *mongo.Collection, match bson.D) map[string]ServiceStat {
	pipeline := mongo.Pipeline{
		match,
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$service"},
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

	result := map[string]ServiceStat{}
	cur, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		return result
	}
	defer cur.Close(ctx)

	var rows []bson.M
	if err = cur.All(ctx, &rows); err != nil {
		return result
	}
	for _, row := range rows {
		name, _ := row["_id"].(string)
		if name == "" {
			continue
		}
		total := toInt64(row["total"])
		errors := toInt64(row["errors"])
		avg := toFloat64(row["avgLatency"])
		er := float64(0)
		if total > 0 {
			er = float64(errors) / float64(total) * 100
		}
		result[name] = ServiceStat{
			ErrorRate:    roundTwo(er),
			AvgLatencyMs: roundTwo(avg),
		}
	}
	return result
}

func (s *MetricsService) calcTimeSeries(ctx context.Context, col *mongo.Collection, start, end time.Time) ([]TimePoint, []TimePoint) {
	// 1-minute buckets across the window
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "timestamp", Value: bson.D{
				{Key: "$gte", Value: start},
				{Key: "$lte", Value: end},
			}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "$dateTrunc", Value: bson.D{
				{Key: "date", Value: "$timestamp"},
				{Key: "unit", Value: "minute"},
			}}}},
			{Key: "avgLatency", Value: bson.D{{Key: "$avg", Value: "$latency"}}},
			{Key: "errors", Value: bson.D{{Key: "$sum", Value: bson.D{
				{Key: "$cond", Value: bson.A{
					bson.D{{Key: "$gte", Value: bson.A{"$status", 500}}},
					1, 0,
				}},
			}}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	}

	cur, err := col.Aggregate(ctx, pipeline)
	latencySeries := []TimePoint{}
	errorSeries := []TimePoint{}
	if err != nil {
		return latencySeries, errorSeries
	}
	defer cur.Close(ctx)

	var rows []bson.M
	if err = cur.All(ctx, &rows); err != nil {
		return latencySeries, errorSeries
	}

	for _, row := range rows {
		t := int64(0)
		switch v := row["_id"].(type) {
		case time.Time:
			t = v.UnixMilli()
		}
		latencySeries = append(latencySeries, TimePoint{T: t, Value: roundTwo(toFloat64(row["avgLatency"]))})
		errorSeries = append(errorSeries, TimePoint{T: t, Value: float64(toInt64(row["errors"]))})
	}
	return latencySeries, errorSeries
}
