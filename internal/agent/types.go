package agent

import "time"

// ── State machine ─────────────────────────────────────────────────────────────

// AgentState is the execution phase of an investigation session
type AgentState string

const (
	StateIdle      AgentState = "idle"
	StatePending   AgentState = "pending"
	StateExecuting AgentState = "executing"
	StateCompleted AgentState = "completed"
	StateFailed    AgentState = "failed"
)

// ── Per-action record ─────────────────────────────────────────────────────────

// AgentAction tracks a single tool invocation within a session
type AgentAction struct {
	ID        string     `json:"id"`
	Tool      string     `json:"tool"`
	Status    string     `json:"status"` // pending | executing | completed | failed
	Input     string     `json:"input,omitempty"`
	Output    string     `json:"output,omitempty"`
	Err       string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// ── AI reasoning output ───────────────────────────────────────────────────────

// AgentReasoning is the structured output from the Groq reasoning step
type AgentReasoning struct {
	Cause      string   `json:"cause"`
	Confidence float64  `json:"confidence"` // 0.0–1.0
	Severity   string   `json:"severity"`   // low | medium | high | critical
	Action     string   `json:"action"`     // monitor | alert | escalate | rollback
	Reasoning  string   `json:"reasoning"`
	Tools      []string `json:"tools"` // ordered tool list suggested by AI
}

// ── Session (one investigation) ───────────────────────────────────────────────

// AgentSession is the complete state of one autonomous SRE investigation
type AgentSession struct {
	ID         string          `json:"id"`
	IncidentID string          `json:"incidentId"`
	State      AgentState      `json:"state"`
	Thoughts   []string        `json:"thoughts"`
	Actions    []AgentAction   `json:"actions"`
	Reasoning  *AgentReasoning `json:"reasoning,omitempty"`
	StartedAt  time.Time       `json:"startedAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// ── WebSocket event envelope ──────────────────────────────────────────────────

// AgentWSEvent is broadcast to all WebSocket clients during agent execution
type AgentWSEvent struct {
	Type      string      `json:"type"` // agent_thought | agent_action | agent_complete | agent_recovery
	SessionID string      `json:"sessionId"`
	Payload   interface{} `json:"payload"`
}
