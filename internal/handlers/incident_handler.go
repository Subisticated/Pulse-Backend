package handlers

import (
	"net/http"

	"pulse-backend/internal/ai"
	"pulse-backend/internal/models"
	"pulse-backend/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// IncidentHandler handles incident listing, resolving, and RCA
type IncidentHandler struct {
	incidentService *services.IncidentService
	aiService       *ai.AIService
}

// NewIncidentHandler constructs an IncidentHandler
func NewIncidentHandler(incSrv *services.IncidentService, aiSrv *ai.AIService) *IncidentHandler {
	return &IncidentHandler{incidentService: incSrv, aiService: aiSrv}
}

// GetIncidents godoc
// GET /api/v1/incidents
// Optional query: ?resolved=true|false
func (h *IncidentHandler) GetIncidents(c *gin.Context) {
	resolved := c.Query("resolved") // "true" | "false" | ""

	incidents, err := h.incidentService.GetIncidents(c.Request.Context(), resolved)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch incidents"})
		return
	}
	c.JSON(http.StatusOK, incidents)
}

// ResolveIncident godoc
// PATCH /api/v1/incidents/:id/resolve
func (h *IncidentHandler) ResolveIncident(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "incident id is required"})
		return
	}

	updated, err := h.incidentService.ResolveIncident(c.Request.Context(), id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("ResolveIncident failed")
		c.JSON(http.StatusNotFound, gin.H{"error": "Incident not found or already resolved"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// RCARequest is the POST /api/v1/rca body
type RCARequest struct {
	IncidentID string `json:"incidentId" binding:"required"`
}

// PerformRCA fetches the incident + context logs and calls the AI service
func (h *IncidentHandler) PerformRCA(c *gin.Context) {
	var req RCARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "incidentId is required"})
		return
	}

	ctx := c.Request.Context()

	incident, err := h.incidentService.GetIncidentByID(ctx, req.IncidentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Incident not found"})
		return
	}

	recentLogs, err := h.incidentService.GetRecentLogsForService(ctx, incident.Service, incident.Environment, 10)
	if err != nil {
		log.Warn().Err(err).Msg("Could not fetch context logs for RCA; proceeding without them")
		recentLogs = []models.LogEvent{}
	}

	result, err := h.aiService.PerformRCA(ctx, incident, recentLogs)
	if err != nil {
		log.Error().Err(err).Msg("RCA failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "RCA analysis failed"})
		return
	}
	c.JSON(http.StatusOK, result)
}
