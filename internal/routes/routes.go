package routes

import (
	"net/http"

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

// GetQueue returns the currently active LogQueue instance for graceful shutdown draining
func GetQueue() *queue.LogQueue {
	return activeQueue
}

// SetupRouter wires up all middleware, dependencies, and routes.
// Runs Gin in ReleaseMode to eliminate debug logging overhead.
func SetupRouter(db *mongo.Database, hub *wsocket.Hub) *gin.Engine {
	// ── Production mode ───────────────────────────────────────────────────────
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())

	// CORS
	corsConfig := cors.DefaultConfig()
	corsConfig.AllowAllOrigins = true
	corsConfig.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization", "Accept"}
	corsConfig.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	r.Use(cors.New(corsConfig))

	// ── Dependency injection ──────────────────────────────────────────────────
	aiSvc := ai.NewAIService()
	det := detector.NewIncidentDetector(db, hub)

	ingestionSvc := services.NewIngestionService(db, det)
	metricsSvc := services.NewMetricsService(db)
	incidentSvc := services.NewIncidentService(db)

	// Save reference to active queue
	activeQueue = ingestionSvc.Queue

	// Handlers — LogHandler now takes the queue directly
	logH := handlers.NewLogHandler(ingestionSvc.Queue)
	metricsH := handlers.NewMetricsHandler(metricsSvc)
	incidentH := handlers.NewIncidentHandler(incidentSvc, aiSvc)
	wsH := handlers.NewWSHandler(hub)

	// ── Routes ───────────────────────────────────────────────────────────────
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "pulse-backend"})
	})

	// WebSocket endpoint
	r.GET("/ws", wsH.ServeWS)

	// API v1
	v1 := r.Group("/api/v1")
	{
		v1.POST("/logs", logH.IngestLog)
		v1.GET("/logs/queue/stats", logH.QueueStats) // observability
		v1.GET("/metrics", metricsH.GetMetrics)
		v1.GET("/incidents", incidentH.GetIncidents)
		v1.PATCH("/incidents/:id/resolve", incidentH.ResolveIncident)
		v1.POST("/rca", incidentH.PerformRCA)
	}

	return r
}
