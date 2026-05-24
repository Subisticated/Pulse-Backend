package handlers

import (
	"net/http"
	"strconv"

	"pulse-backend/internal/services"

	"github.com/gin-gonic/gin"
)

// ChaosHandler serves POST /chaos/:scenario and GET /chaos/status
type ChaosHandler struct {
	svc *services.ChaosService
}

// NewChaosHandler constructs a ChaosHandler
func NewChaosHandler(svc *services.ChaosService) *ChaosHandler {
	return &ChaosHandler{svc: svc}
}

// ActivateChaos godoc
// POST /chaos/:scenario
// Optional body: { "duration": 60 }
func (h *ChaosHandler) ActivateChaos(c *gin.Context) {
	scenario := c.Param("scenario")
	if scenario == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "MISSING_SCENARIO", "message": "scenario name required"},
		})
		return
	}

	var body struct {
		Duration int `json:"duration"`
	}
	_ = c.ShouldBindJSON(&body) // optional body
	if body.Duration <= 0 {
		body.Duration = 60
	}

	// Allow override from query param too
	if d := c.Query("duration"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			body.Duration = parsed
		}
	}

	mode := h.svc.Activate(scenario, body.Duration)
	c.JSON(http.StatusOK, mode)
}

// GetChaosStatus godoc
// GET /chaos/status
func (h *ChaosHandler) GetChaosStatus(c *gin.Context) {
	status := h.svc.Status()
	c.JSON(http.StatusOK, status)
}
