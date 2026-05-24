package routes

import (
	"net/http"
	"time"

	"pulse-backend/internal/ai"
	"pulse-backend/internal/detector"
	"pulse-backend/internal/handlers"
	"pulse-backend/internal/middleware"
	"pulse-backend/internal/queue"
	"pulse-backend/internal/services"
	"pulse-backend/internal/wsocket"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

var activeQueue *queue.LogQueue

// GetQueue returns the active LogQueue for graceful shutdown draining
func GetQueue() *queue.LogQueue {
	return activeQueue
}

// startTime records when the server started (for health uptime)
var startTime = time.Now()

// SetupRouter wires up all middleware, dependencies, and routes.
func SetupRouter(db *mongo.Database, hub *wsocket.Hub) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())

	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization", "Accept"}
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	r.Use(cors.New(corsConfig))

	// ── Services ──────────────────────────────────────────────────────────────
	aiSvc := ai.NewAIService()
	det := detector.NewIncidentDetector(db, hub)
	ingestionSvc := services.NewIngestionService(db, det)
	metricsSvc := services.NewMetricsService(db)
	incidentSvc := services.NewIncidentService(db)
	topologySvc := services.NewTopologyService(db)
	chaosSvc := services.NewChaosService()
	activeQueue = ingestionSvc.Queue

	// ── Handlers ──────────────────────────────────────────────────────────────
	logH := handlers.NewLogHandler(ingestionSvc.Queue)
	metricsH := handlers.NewMetricsHandler(metricsSvc)
	incidentH := handlers.NewIncidentHandler(incidentSvc, aiSvc)
	topologyH := handlers.NewTopologyHandler(topologySvc)
	chaosH := handlers.NewChaosHandler(chaosSvc)
	wsH := handlers.NewWSHandler(hub)

	// ── Health ────────────────────────────────────────────────────────────────
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "pulse-backend",
			"version": "2.0.0",
			"uptime":  int(time.Since(startTime).Seconds()),
		})
	})
	r.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"version": "2.0.0",
			"uptime":  int(time.Since(startTime).Seconds()),
		})
	})

	// ── WebSocket ─────────────────────────────────────────────────────────────
	r.GET("/ws", wsH.ServeWS)

	// ── API v1 ────────────────────────────────────────────────────────────────
	v1 := r.Group("/api/v1")
	{
		// Telemetry ingestion (SDK endpoint)
		v1.POST("/logs", logH.IngestLog)
		v1.GET("/logs/queue/stats", logH.QueueStats)

		// Dashboard metrics
		v1.GET("/metrics", metricsH.GetMetrics)

		// Incidents
		v1.GET("/incidents", incidentH.GetIncidents)
		v1.PATCH("/incidents/:id/resolve", incidentH.ResolveIncident)

		// AI RCA
		v1.POST("/rca", incidentH.PerformRCA)

		// Service topology graph
		v1.GET("/topology", topologyH.GetTopology)
	}

	// ── Chaos (demo-only, no /api/ prefix per spec) ───────────────────────────
	r.POST("/chaos/:scenario", chaosH.ActivateChaos)
	r.GET("/chaos/status", chaosH.GetChaosStatus)

	return r
}
