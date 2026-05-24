package handlers

import (
	"net/http"
	"time"

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
// Query: ?status=active|resolved|all  ?limit=20  ?since=ISO
func (h *IncidentHandler) GetIncidents(c *gin.Context) {
	status := c.DefaultQuery("status", "all")  // active | resolved | all
	limitStr := c.DefaultQuery("limit", "50")
	sinceStr := c.Query("since")

	// Map status to resolved filter
	resolvedFilter := ""
	switch status {
	case "active":
		resolvedFilter = "false"
	case "resolved":
		resolvedFilter = "true"
	}

	var since *time.Time
	if sinceStr != "" {
		if t, err := time.Parse(time.RFC3339, sinceStr); err == nil {
			since = &t
		}
	}

	limit := int64(50)
	if _, err := parseLimit(limitStr, &limit); err != nil {
		limit = 50
	}

	incidents, err := h.incidentService.GetIncidents(c.Request.Context(), resolvedFilter, limit, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "INCIDENT_FETCH_ERROR", "message": "Failed to fetch incidents"},
		})
		return
	}
	c.JSON(http.StatusOK, incidents)
}

// ResolveIncident godoc
// PATCH /api/v1/incidents/:id/resolve
func (h *IncidentHandler) ResolveIncident(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "MISSING_ID", "message": "incident id is required"},
		})
		return
	}

	updated, err := h.incidentService.ResolveIncident(c.Request.Context(), id)
	if err != nil {
		log.Error().Err(err).Str("id", id).Msg("ResolveIncident failed")
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "NOT_FOUND", "message": "Incident not found or already resolved"},
		})
		return
	}
	c.JSON(http.StatusOK, updated)
}

// RCARequest is the POST /api/v1/rca body
type RCARequest struct {
	IncidentID string `json:"incidentId" binding:"required"`
	Requester  string `json:"requester"` // "ui" | "sdk" — optional, for audit
}

// PerformRCA fetches the incident + context logs and calls the AI service
func (h *IncidentHandler) PerformRCA(c *gin.Context) {
	var req RCARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "MISSING_FIELD", "message": "incidentId is required"},
		})
		return
	}

	ctx := c.Request.Context()

	incident, err := h.incidentService.GetIncidentByID(ctx, req.IncidentID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "NOT_FOUND", "message": "Incident not found"},
		})
		return
	}

	recentLogs, err := h.incidentService.GetRecentLogsForService(ctx, incident.Service, incident.Environment, 10)
	if err != nil {
		log.Warn().Err(err).Msg("Could not fetch context logs for RCA")
		recentLogs = []models.LogEvent{}
	}

	result, err := h.aiService.PerformRCA(ctx, incident, recentLogs)
	if err != nil {
		log.Error().Err(err).Msg("RCA failed")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "RCA_FAILED", "message": "AI RCA analysis failed"},
		})
		return
	}

	// Dashboard-compatible RCA response shape
	c.JSON(http.StatusOK, gin.H{
		"incidentId": result.IncidentID,
		"rca": gin.H{
			"summary":           result.Cause,
			"probableRootCause": result.Cause,
			"evidence":          result.Evidence,
			"suggestedFix":      joinStrings(result.Fixes),
			"confidence":        float64(result.Confidence) / 100.0,
		},
		"generatedAt": result.GeneratedAt,
	})
}

// parseLimit safely parses a string limit into int64
func parseLimit(s string, out *int64) (int64, error) {
	var n int64
	_, err := parseIntStr(s, &n)
	if err == nil && n > 0 {
		*out = n
	}
	return *out, err
}

func parseIntStr(s string, out *int64) (int64, error) {
	n := int64(0)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, nil
		}
		n = n*10 + int64(c-'0')
	}
	*out = n
	return n, nil
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += "; "
		}
		result += s
	}
	return result
}
