package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"pulse-backend/internal/models"
	"pulse-backend/internal/tools"
	"pulse-backend/internal/wsocket"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PulseAgent is the autonomous SRE investigation agent.
// It is safe for concurrent use — each investigation runs in its own goroutine.
type PulseAgent struct {
	db  *mongo.Database
	hub *wsocket.Hub

	reasoner *Reasoner
	executor *Executor

	// Tool instances (also exposed to executor registry)
	logsTool      *tools.LogsTool
	metricsTool   *tools.MetricsTool
	incidentsTool *tools.IncidentsTool
	alertTool     *tools.AlertTool
	recoveryTool  *tools.RecoveryTool

	// Session store (incidentID → latest session)
	sessions map[string]*AgentSession
	mu       sync.RWMutex
}

// NewPulseAgent constructs and wires all agent dependencies
func NewPulseAgent(db *mongo.Database, hub *wsocket.Hub) *PulseAgent {
	lt := tools.NewLogsTool(db)
	mt := tools.NewMetricsTool(db)
	it := tools.NewIncidentsTool(db)
	at := tools.NewAlertTool(hub)
	rt := tools.NewRecoveryTool(db)

	exec := newExecutor([]tools.Tool{lt, mt, it, at, rt})

	return &PulseAgent{
		db:            db,
		hub:           hub,
		reasoner:      newReasoner(),
		executor:      exec,
		logsTool:      lt,
		metricsTool:   mt,
		incidentsTool: it,
		alertTool:     at,
		recoveryTool:  rt,
		sessions:      make(map[string]*AgentSession),
	}
}

// ── Public API ────────────────────────────────────────────────────────────────

// Investigate starts an autonomous investigation for a given incident ID.
// The loop runs asynchronously; callers get the session back immediately.
func (a *PulseAgent) Investigate(ctx context.Context, incidentID string) (*AgentSession, error) {
	incident, err := a.fetchIncident(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("incident not found: %w", err)
	}

	session := &AgentSession{
		ID:         newID(),
		IncidentID: incidentID,
		State:      StatePending,
		Thoughts:   []string{},
		Actions:    []AgentAction{},
		StartedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	a.mu.Lock()
	a.sessions[incidentID] = session
	a.mu.Unlock()

	go a.runLoop(context.Background(), session, incident)

	return session, nil
}

// GetSession retrieves the session for a given incidentID
func (a *PulseAgent) GetSession(incidentID string) (*AgentSession, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s, ok := a.sessions[incidentID]
	return s, ok
}

// LatestSession returns the most recently created session
func (a *PulseAgent) LatestSession() *AgentSession {
	a.mu.RLock()
	defer a.mu.RUnlock()
	var latest *AgentSession
	for _, s := range a.sessions {
		if latest == nil || s.StartedAt.After(latest.StartedAt) {
			latest = s
		}
	}
	return latest
}

// AllSessions returns all sessions sorted newest first
func (a *PulseAgent) AllSessions() []*AgentSession {
	a.mu.RLock()
	defer a.mu.RUnlock()
	sessions := make([]*AgentSession, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// ── Demo mode ─────────────────────────────────────────────────────────────────

// RunDemo injects a synthetic failure scenario and triggers the full autonomous loop.
// Returns the session immediately; the 90-second demo runs in the background.
func (a *PulseAgent) RunDemo(ctx context.Context) (*AgentSession, error) {
	log.Info().Msg("🎭 Agent demo mode activated")

	// 1. Inject synthetic error logs
	service := "demo-shopfast"
	env := "production"
	now := time.Now()

	errorMessages := []string{
		"connection pool exhausted: too many connections",
		"timeout waiting for database connection",
		"upstream service unavailable",
		"TCP dial timeout: context deadline exceeded",
	}

	col := a.db.Collection("logs")
	for i := 0; i < 8; i++ {
		errMsg := errorMessages[i%len(errorMessages)]
		log := models.LogEvent{
			ID:          primitive.NewObjectID(),
			Endpoint:    "/api/checkout",
			Method:      "POST",
			Status:      500,
			Latency:     1100 + rand.Intn(800),
			LatencyMs:   1100 + rand.Intn(800),
			Error:       errMsg,
			Service:     service,
			Environment: env,
			Timestamp:   now.Add(-time.Duration(i) * 15 * time.Second),
		}
		_, _ = col.InsertOne(ctx, log)
	}

	// 2. Create synthetic incident
	incidentID := primitive.NewObjectID()
	inc := models.Incident{
		ID:          incidentID,
		Title:       "Agent Demo: Critical error storm on " + service,
		Severity:    "Critical",
		Cause:       "high_error_rate",
		Description: "8 HTTP 5xx errors detected in the last 5 minutes on " + service,
		Service:     service,
		Services:    []string{service},
		Environment: env,
		Resolved:    false,
		Status:      "active",
		StartTime:   now,
		Links:       &models.IncidentLinks{RCA: "/api/v1/rca"},
	}
	_, _ = a.db.Collection("incidents").InsertOne(ctx, &inc)

	// 3. Start autonomous investigation
	session, err := a.Investigate(ctx, incidentID.Hex())
	if err != nil {
		return nil, err
	}

	// 4. Schedule recovery injection after 40 seconds
	go func() {
		time.Sleep(40 * time.Second)
		a.injectRecoveryLogs(service, env)
	}()

	return session, nil
}

// injectRecoveryLogs writes healthy 200 OK logs to drive the error rate below threshold
func (a *PulseAgent) injectRecoveryLogs(service, env string) {
	col := a.db.Collection("logs")
	ctx := context.Background()
	for i := 0; i < 25; i++ {
		l := models.LogEvent{
			ID:          primitive.NewObjectID(),
			Endpoint:    "/api/checkout",
			Method:      "POST",
			Status:      200,
			Latency:     50 + rand.Intn(80),
			LatencyMs:   50 + rand.Intn(80),
			Service:     service,
			Environment: env,
			Timestamp:   time.Now(),
		}
		_, _ = col.InsertOne(ctx, l)
		time.Sleep(1 * time.Second)
	}
}

// ── Core agent loop ───────────────────────────────────────────────────────────

func (a *PulseAgent) runLoop(ctx context.Context, session *AgentSession, incident *models.Incident) {
	a.setState(session, StateExecuting)

	// ── Step 1: Observe ───────────────────────────────────────────────────
	a.think(session, "🔍 Starting autonomous SRE investigation...")
	a.think(session, fmt.Sprintf("📋 Incident: %s — Severity: %s", incident.Description, incident.Severity))

	params := map[string]interface{}{
		"service":    incident.Service,
		"incidentId": incident.ID.Hex(),
		"sessionId":  session.ID,
	}

	a.think(session, "📊 Fetching recent logs for error pattern analysis...")
	logsOut := a.executor.ExecuteStep(ctx, session, "fetch_logs", params)
	a.broadcastAction(session)

	a.think(session, "📈 Collecting current performance metrics...")
	metricsOut := a.executor.ExecuteStep(ctx, session, "fetch_metrics", params)
	a.broadcastAction(session)

	a.think(session, "🗂️  Scanning historical incident database for recurring patterns...")
	incidentsOut := a.executor.ExecuteStep(ctx, session, "fetch_incidents", params)
	a.broadcastAction(session)

	// ── Step 2: Think ─────────────────────────────────────────────────────
	a.think(session, "🧠 Reasoning over evidence with AI analysis...")

	incidentContext := fmt.Sprintf("Service: %s\nSeverity: %s\nCause: %s\nDescription: %s\nEnvironment: %s",
		incident.Service, incident.Severity, incident.Cause, incident.Description, incident.Environment)

	reasoning, err := a.reasoner.Reason(ctx, incidentContext, logsOut, metricsOut, incidentsOut)
	if err != nil {
		a.think(session, fmt.Sprintf("⚠️  AI reasoning error: %s — using local fallback", err.Error()))
	}

	a.mu.Lock()
	session.Reasoning = reasoning
	a.mu.Unlock()

	// ── Step 3: Plan ──────────────────────────────────────────────────────
	plan := Plan(reasoning)
	a.think(session, fmt.Sprintf("📌 Root cause identified: %s (confidence: %.0f%%)",
		reasoning.Cause, reasoning.Confidence*100))
	a.think(session, fmt.Sprintf("⚡ Chosen action: %s — executing plan: %v", reasoning.Action, plan))

	// ── Step 4: Execute remaining planned tools ───────────────────────────
	// (fetch_logs/metrics/incidents already run above; skip duplicates)
	alreadyRun := map[string]bool{
		"fetch_logs":      true,
		"fetch_metrics":   true,
		"fetch_incidents": true,
	}

	alertParams := map[string]interface{}{
		"service":   incident.Service,
		"severity":  reasoning.Severity,
		"sessionId": session.ID,
		"message": fmt.Sprintf("[Pulse Agent] %s — %s. Recommended action: %s",
			reasoning.Cause, reasoning.Reasoning, reasoning.Action),
	}

	for _, toolName := range plan {
		if alreadyRun[toolName] {
			continue
		}
		switch toolName {
		case "send_alert":
			a.think(session, "🚨 Sending alert to dashboard and notification channels...")
			a.executor.ExecuteStep(ctx, session, "send_alert", alertParams)
			a.broadcastAction(session)

		case "monitor_recovery":
			a.think(session, "⏳ Monitoring service recovery (polling every 10s, timeout: 3 min)...")
			recoveryOut := a.executor.ExecuteStep(ctx, session, "monitor_recovery", params)
			a.broadcastAction(session)
			a.think(session, recoveryOut)
		}
	}

	// ── Step 5: Evaluate ──────────────────────────────────────────────────
	a.think(session, "✅ Investigation complete. Final summary:")
	a.think(session, fmt.Sprintf("   Cause      : %s", reasoning.Cause))
	a.think(session, fmt.Sprintf("   Confidence : %.0f%%", reasoning.Confidence*100))
	a.think(session, fmt.Sprintf("   Severity   : %s", reasoning.Severity))
	a.think(session, fmt.Sprintf("   Action     : %s", reasoning.Action))
	a.think(session, "   "+reasoning.Reasoning)

	a.setState(session, StateCompleted)
	a.broadcastComplete(session)

	log.Info().
		Str("sessionId", session.ID).
		Str("incidentId", session.IncidentID).
		Str("action", reasoning.Action).
		Msg("🤖 Agent investigation completed")
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (a *PulseAgent) think(session *AgentSession, thought string) {
	a.mu.Lock()
	session.Thoughts = append(session.Thoughts, thought)
	session.UpdatedAt = time.Now()
	a.mu.Unlock()

	// Broadcast thought in real time
	evt := AgentWSEvent{
		Type:      "agent_thought",
		SessionID: session.ID,
		Payload:   map[string]interface{}{"thought": thought, "timestamp": time.Now().UTC()},
	}
	if data, err := json.Marshal(evt); err == nil && a.hub != nil {
		a.hub.Broadcast(data)
	}
}

func (a *PulseAgent) setState(session *AgentSession, state AgentState) {
	a.mu.Lock()
	session.State = state
	session.UpdatedAt = time.Now()
	a.mu.Unlock()
}

func (a *PulseAgent) broadcastAction(session *AgentSession) {
	a.mu.RLock()
	var lastAction *AgentAction
	if len(session.Actions) > 0 {
		cp := session.Actions[len(session.Actions)-1]
		lastAction = &cp
	}
	a.mu.RUnlock()

	if lastAction == nil || a.hub == nil {
		return
	}
	evt := AgentWSEvent{
		Type:      "agent_action",
		SessionID: session.ID,
		Payload:   lastAction,
	}
	if data, err := json.Marshal(evt); err == nil {
		a.hub.Broadcast(data)
	}
}

func (a *PulseAgent) broadcastComplete(session *AgentSession) {
	if a.hub == nil {
		return
	}
	a.mu.RLock()
	cp := *session
	a.mu.RUnlock()

	evt := AgentWSEvent{
		Type:      "agent_complete",
		SessionID: session.ID,
		Payload:   cp,
	}
	if data, err := json.Marshal(evt); err == nil {
		a.hub.Broadcast(data)
	}
}

func (a *PulseAgent) fetchIncident(ctx context.Context, idStr string) (*models.Incident, error) {
	id, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return nil, err
	}
	var inc models.Incident
	err = a.db.Collection("incidents").FindOne(ctx, bson.M{"_id": id},
		options.FindOne()).Decode(&inc)
	return &inc, err
}

func newID() string {
	return fmt.Sprintf("%d-%04d", time.Now().UnixMilli(), rand.Intn(9999))
}
