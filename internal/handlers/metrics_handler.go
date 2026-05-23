package handlers

import (
	"net/http"

	"pulse-backend/internal/services"

	"github.com/gin-gonic/gin"
)

// MetricsHandler processes GET /api/v1/metrics
type MetricsHandler struct {
	service *services.MetricsService
}

// NewMetricsHandler constructs a MetricsHandler
func NewMetricsHandler(srv *services.MetricsService) *MetricsHandler {
	return &MetricsHandler{service: srv}
}

// GetMetrics returns aggregated performance statistics from MongoDB
func (h *MetricsHandler) GetMetrics(c *gin.Context) {
	metrics, err := h.service.GetMetrics(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to compute metrics"})
		return
	}
	c.JSON(http.StatusOK, metrics)
}
