package handlers

import (
	"net/http"

	"pulse-backend/internal/services"

	"github.com/gin-gonic/gin"
)

// TopologyHandler serves GET /api/v1/topology
type TopologyHandler struct {
	svc *services.TopologyService
}

// NewTopologyHandler constructs a TopologyHandler
func NewTopologyHandler(svc *services.TopologyService) *TopologyHandler {
	return &TopologyHandler{svc: svc}
}

// GetTopology godoc
// GET /api/v1/topology
func (h *TopologyHandler) GetTopology(c *gin.Context) {
	result, err := h.svc.GetTopology(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "TOPOLOGY_ERROR",
				"message": "Failed to build topology graph",
			},
		})
		return
	}
	c.JSON(http.StatusOK, result)
}
