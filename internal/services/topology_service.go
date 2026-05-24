package services

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// TopologyNode represents a single service in the dependency graph
type TopologyNode struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	Status string                 `json:"status"` // healthy | degraded | critical | unknown
	Meta   map[string]interface{} `json:"meta,omitempty"`
}

// TopologyEdge represents a directional dependency between two services
type TopologyEdge struct {
	From        string  `json:"from"`
	To          string  `json:"to"`
	Type        string  `json:"type"` // http | grpc | queue
	AvgLatencyMs float64 `json:"avgLatencyMs"`
}

// TopologyResponse is the full graph returned by GET /api/v1/topology
type TopologyResponse struct {
	Services []TopologyNode `json:"services"`
	Edges    []TopologyEdge `json:"edges"`
}

// TopologyService derives a live service graph from log and incident data
type TopologyService struct {
	db *mongo.Database
}

// NewTopologyService instantiates a TopologyService
func NewTopologyService(db *mongo.Database) *TopologyService {
	return &TopologyService{db: db}
}

// GetTopology builds the service dependency graph from the last 5 minutes of logs + active incidents
func (s *TopologyService) GetTopology(ctx context.Context) (*TopologyResponse, error) {
	col := s.db.Collection("logs")
	since := time.Now().Add(-5 * time.Minute)

	// Group services with their avg latency and error counts
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.D{{Key: "timestamp", Value: bson.D{{Key: "$gte", Value: since}}}}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$service"},
			{Key: "avgLatency", Value: bson.D{{Key: "$avg", Value: "$latency"}}},
			{Key: "total", Value: bson.D{{Key: "$sum", Value: 1}}},
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
		log.Error().Err(err).Msg("Topology aggregation failed")
		return nil, err
	}
	defer cur.Close(ctx)

	var rows []bson.M
	if err = cur.All(ctx, &rows); err != nil {
		return nil, err
	}

	// Fetch active incidents to overlay "critical" / "degraded" status
	criticalServices := map[string]bool{}
	incCol := s.db.Collection("incidents")
	incCur, err := incCol.Find(ctx, bson.M{"resolved": false})
	if err == nil {
		defer incCur.Close(ctx)
		var incs []bson.M
		if err = incCur.All(ctx, &incs); err == nil {
			for _, inc := range incs {
				if svc, ok := inc["service"].(string); ok {
					criticalServices[svc] = true
				}
			}
		}
	}

	nodes := make([]TopologyNode, 0, len(rows))
	serviceAvgLatency := map[string]float64{}

	for _, row := range rows {
		name, _ := row["_id"].(string)
		if name == "" {
			continue
		}
		total := toInt64(row["total"])
		errors := toInt64(row["errors"])
		avgLat := toFloat64(row["avgLatency"])
		serviceAvgLatency[name] = avgLat

		var status string
		if criticalServices[name] {
			status = "critical"
		} else if total > 0 && float64(errors)/float64(total) > 0.05 {
			status = "degraded"
		} else {
			status = "healthy"
		}

		nodes = append(nodes, TopologyNode{
			ID:     name,
			Name:   capitalize(name),
			Status: status,
			Meta: map[string]interface{}{
				"avgLatencyMs": roundTwo(avgLat),
				"totalRequests": total,
				"errorCount":    errors,
			},
		})
	}

	// Build edges: pair services that frequently appear within same time window
	// For hackathon: derive edges from incidents referencing multiple services
	edges := buildEdges(rows, serviceAvgLatency)

	return &TopologyResponse{
		Services: nodes,
		Edges:    edges,
	}, nil
}

func buildEdges(rows []bson.M, latencies map[string]float64) []TopologyEdge {
	// Simple heuristic: build a mesh from discovered services.
	// Real tracing would use span parent-child relationships.
	// For demo: common patterns like checkout→payments, auth→users etc.
	knownDeps := map[string]string{
		"checkout":     "payments",
		"payments":     "auth",
		"notification": "checkout",
		"inventory":    "checkout",
	}

	edges := make([]TopologyEdge, 0)
	serviceSet := map[string]bool{}
	for _, row := range rows {
		if name, ok := row["_id"].(string); ok && name != "" {
			serviceSet[name] = true
		}
	}

	for from, to := range knownDeps {
		if serviceSet[from] && serviceSet[to] {
			avgLat := latencies[to]
			edges = append(edges, TopologyEdge{
				From:        from,
				To:          to,
				Type:        "http",
				AvgLatencyMs: roundTwo(avgLat),
			})
		}
	}
	return edges
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	b := []byte(s)
	if b[0] >= 'a' && b[0] <= 'z' {
		b[0] -= 32
	}
	return string(b)
}
