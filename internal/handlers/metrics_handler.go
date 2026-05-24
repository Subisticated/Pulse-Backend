package handlers

import (
	"net/http"
	"time"

	"pulse-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// MetricsHandler serves GET /api/v1/metrics
type MetricsHandler struct {
	service *services.MetricsService
}

// NewMetricsHandler constructs a MetricsHandler
func NewMetricsHandler(svc *services.MetricsService) *MetricsHandler {
	return &MetricsHandler{service: svc}
}

// GetMetrics godoc
// GET /api/v1/metrics
// Optional query params: ?start=ISO&end=ISO
func (h *MetricsHandler) GetMetrics(c *gin.Context) {
	var start, end time.Time

	if s := c.Query("start"); s != "" {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			start = parsed
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"code": "INVALID_PARAM", "message": "start must be ISO 8601 e.g. 2026-05-24T10:00:00Z"},
			})
			return
		}
	}
	if e := c.Query("end"); e != "" {
		if parsed, err := time.Parse(time.RFC3339, e); err == nil {
			end = parsed
		} else {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"code": "INVALID_PARAM", "message": "end must be ISO 8601 e.g. 2026-05-24T10:05:00Z"},
			})
			return
		}
	}

	metrics, err := h.service.GetMetrics(c.Request.Context(), start, end)
	if err != nil {
		log.Error().Err(err).Msg("GetMetrics failed")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "METRICS_ERROR", "message": "Failed to compute metrics"},
		})
		return
	}
	c.JSON(http.StatusOK, metrics)
}
