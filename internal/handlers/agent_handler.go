package handlers

import (
	"net/http"

	"pulse-backend/internal/agent"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

// AgentHandler exposes the autonomous SRE agent via HTTP
type AgentHandler struct {
	agent *agent.PulseAgent
}

// NewAgentHandler constructs an AgentHandler
func NewAgentHandler(a *agent.PulseAgent) *AgentHandler {
	return &AgentHandler{agent: a}
}

// ── POST /agent/analyze ───────────────────────────────────────────────────────
// Triggers a new autonomous investigation for a given incident ID.

type analyzeRequest struct {
	IncidentID string `json:"incidentId" binding:"required"`
}

func (h *AgentHandler) Analyze(c *gin.Context) {
	var req analyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"code": "MISSING_FIELD", "message": "incidentId is required"},
		})
		return
	}

	session, err := h.agent.Investigate(c.Request.Context(), req.IncidentID)
	if err != nil {
		log.Error().Err(err).Str("incidentId", req.IncidentID).Msg("Agent investigation failed to start")
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{"code": "INCIDENT_NOT_FOUND", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"sessionId":  session.ID,
		"incidentId": session.IncidentID,
		"state":      session.State,
		"message":    "Autonomous investigation started — connect to /ws for real-time updates",
	})
}

// ── GET /agent/status ────────────────────────────────────────────────────────
// Returns the current state of the latest (or specified) agent session.

func (h *AgentHandler) Status(c *gin.Context) {
	incidentID := c.Query("incidentId")

	var s *agent.AgentSession
	if incidentID != "" {
		var ok bool
		s, ok = h.agent.GetSession(incidentID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{"code": "SESSION_NOT_FOUND", "message": "No active session for this incident"},
			})
			return
		}
	} else {
		s = h.agent.LatestSession()
		if s == nil {
			c.JSON(http.StatusOK, gin.H{
				"state":   "idle",
				"message": "No investigations have been run yet. POST /agent/analyze or POST /agent/demo to start.",
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"sessionId":  s.ID,
		"incidentId": s.IncidentID,
		"state":      s.State,
		"startedAt":  s.StartedAt,
		"updatedAt":  s.UpdatedAt,
		"reasoning":  s.Reasoning,
	})
}

// ── GET /agent/thoughts ──────────────────────────────────────────────────────
// Returns the real-time reasoning trace of the latest (or specified) session.

func (h *AgentHandler) Thoughts(c *gin.Context) {
	incidentID := c.Query("incidentId")

	var s *agent.AgentSession
	if incidentID != "" {
		var ok bool
		s, ok = h.agent.GetSession(incidentID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
	} else {
		s = h.agent.LatestSession()
		if s == nil {
			c.JSON(http.StatusOK, gin.H{"thoughts": []string{}})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"sessionId":  s.ID,
		"incidentId": s.IncidentID,
		"state":      s.State,
		"thoughts":   s.Thoughts,
	})
}

// ── GET /agent/actions ───────────────────────────────────────────────────────
// Returns all tool executions and their results for the latest (or specified) session.

func (h *AgentHandler) Actions(c *gin.Context) {
	incidentID := c.Query("incidentId")

	var s *agent.AgentSession
	if incidentID != "" {
		var ok bool
		s, ok = h.agent.GetSession(incidentID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
	} else {
		s = h.agent.LatestSession()
		if s == nil {
			c.JSON(http.StatusOK, gin.H{"actions": []interface{}{}})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"sessionId":  s.ID,
		"incidentId": s.IncidentID,
		"state":      s.State,
		"actions":    s.Actions,
	})
}

// ── GET /agent/sessions ──────────────────────────────────────────────────────
// Returns a summary of all investigation sessions.

func (h *AgentHandler) Sessions(c *gin.Context) {
	all := h.agent.AllSessions()

	type summary struct {
		SessionID  string            `json:"sessionId"`
		IncidentID string            `json:"incidentId"`
		State      agent.AgentState  `json:"state"`
		StartedAt  interface{}       `json:"startedAt"`
		Thoughts   int               `json:"thoughtCount"`
		Actions    int               `json:"actionCount"`
	}

	result := make([]summary, 0, len(all))
	for _, s := range all {
		result = append(result, summary{
			SessionID:  s.ID,
			IncidentID: s.IncidentID,
			State:      s.State,
			StartedAt:  s.StartedAt,
			Thoughts:   len(s.Thoughts),
			Actions:    len(s.Actions),
		})
	}

	c.JSON(http.StatusOK, gin.H{"sessions": result, "total": len(result)})
}

// ── POST /agent/demo ─────────────────────────────────────────────────────────
// Runs the full 90-second autonomous demo: inject failure → detect → reason → act → recover.

func (h *AgentHandler) Demo(c *gin.Context) {
	session, err := h.agent.RunDemo(c.Request.Context())
	if err != nil {
		log.Error().Err(err).Msg("Agent demo failed to start")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"code": "DEMO_FAILED", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"sessionId":   session.ID,
		"incidentId":  session.IncidentID,
		"state":       session.State,
		"message":     "🎭 Demo started — autonomous agent is investigating. Connect to /ws for live updates.",
		"pollUrl":     "/agent/thoughts?incidentId=" + session.IncidentID,
		"recoveryIn":  "40 seconds — healthy traffic will be injected automatically",
		"timelineNote": "Full loop: Detect → Reason → Alert → Monitor → Recover (~90s)",
	})
}
