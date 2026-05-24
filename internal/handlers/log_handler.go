package handlers

import (
	"net/http"
	"time"

	"pulse-backend/internal/models"
	"pulse-backend/internal/queue"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LogHandler processes incoming log ingestion requests via the async queue
type LogHandler struct {
	queue *queue.LogQueue
}

// NewLogHandler constructs a LogHandler
func NewLogHandler(q *queue.LogQueue) *LogHandler {
	return &LogHandler{queue: q}
}

// IngestLog validates the payload, assigns an ID, and enqueues for async batch write.
// Returns HTTP 202 Accepted immediately — does not wait for DB write.
func (h *LogHandler) IngestLog(c *gin.Context) {
	var payload models.LogEvent

	if err := c.ShouldBindJSON(&payload); err != nil {
		log.Warn().Err(err).Msg("Invalid log payload")
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Bad Request",
			"details": err.Error(),
		})
		return
	}

	// Pre-assign ID and timestamp on the hot path
	payload.ID = primitive.NewObjectID()
	payload.Timestamp = time.Now()

	// Normalize latency/latencyMs fields so both client and SDK conventions work perfectly
	if payload.Latency == 0 && payload.LatencyMs > 0 {
		payload.Latency = payload.LatencyMs
	} else if payload.LatencyMs == 0 && payload.Latency > 0 {
		payload.LatencyMs = payload.Latency
	}

	if !h.queue.Enqueue(payload) {
		// Queue full — shed load gracefully
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Server busy — retry shortly",
		})
		return
	}

	// Return immediately — DB write happens asynchronously in batch worker
	c.JSON(http.StatusAccepted, gin.H{
		"id":        payload.ID.Hex(),
		"timestamp": payload.Timestamp,
		"queued":    true,
	})
}

// QueueStats godoc
// GET /api/v1/queue/stats
// Returns current queue depth for observability
func (h *LogHandler) QueueStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"queue_depth": h.queue.Len(),
	})
}
