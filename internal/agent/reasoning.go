package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── Groq API types (mirrors ai/rca.go — kept local to avoid circular imports) ──

type groqMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqReq struct {
	Model       string    `json:"model"`
	Messages    []groqMsg `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type groqResp struct {
	Choices []struct {
		Message groqMsg `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ── Reasoner ─────────────────────────────────────────────────────────────────

// Reasoner calls Groq with rich SRE context and returns a structured decision
type Reasoner struct {
	apiKey string
}

func newReasoner() *Reasoner {
	key := os.Getenv("GROQ_API_KEY")
	if key == "" {
		key = os.Getenv("groq_API_KEY")
	}
	return &Reasoner{apiKey: key}
}

// buildSystemPrompt instructs the LLM to behave as an autonomous SRE agent
const systemPrompt = `You are an autonomous AI SRE agent for Pulse monitoring.
You receive real-time observability data and must diagnose infrastructure incidents.

Your response MUST be a single valid JSON object — no markdown, no explanations outside JSON:
{
  "cause": "one-line root cause",
  "confidence": 0.87,
  "severity": "critical",
  "action": "escalate",
  "reasoning": "two-sentence explanation of your decision",
  "tools": ["send_alert", "monitor_recovery"]
}

severity options: low | medium | high | critical
action options: monitor | alert | escalate | rollback
tools to suggest (ordered): fetch_logs | fetch_metrics | fetch_incidents | send_alert | monitor_recovery`

// Reason sends the full observability context to Groq and returns a structured decision
func (r *Reasoner) Reason(ctx context.Context, incidentDesc, logsData, metricsData, incidentsData string) (*AgentReasoning, error) {
	userPrompt := fmt.Sprintf(`INCIDENT UNDER INVESTIGATION:
%s

RECENT LOGS:
%s

CURRENT METRICS:
%s

HISTORICAL INCIDENTS:
%s

Analyze this incident and respond with the JSON decision.`, incidentDesc, logsData, metricsData, incidentsData)

	if r.apiKey != "" {
		result, err := r.callGroq(ctx, userPrompt)
		if err == nil {
			return result, nil
		}
		// fall through to local on Groq error
	}

	return r.localReason(incidentDesc, logsData, metricsData), nil
}

func (r *Reasoner) callGroq(ctx context.Context, userMsg string) (*AgentReasoning, error) {
	body, _ := json.Marshal(groqReq{
		Model:       "llama-3.1-8b-instant",
		Temperature: 0.15,
		Messages: []groqMsg{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
	})

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var gr groqResp
	if err = json.Unmarshal(raw, &gr); err != nil {
		return nil, err
	}
	if gr.Error != nil {
		return nil, fmt.Errorf("groq: %s", gr.Error.Message)
	}
	if len(gr.Choices) == 0 {
		return nil, fmt.Errorf("groq: no choices returned")
	}

	content := strings.TrimSpace(gr.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed AgentReasoning
	if err = json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("groq parse: %w", err)
	}
	return &parsed, nil
}

// localReason is the deterministic fallback when Groq is unavailable
func (r *Reasoner) localReason(incidentDesc, logsData, metricsData string) *AgentReasoning {
	reasoning := &AgentReasoning{
		Tools: []string{"send_alert", "monitor_recovery"},
	}

	desc := strings.ToLower(incidentDesc + logsData + metricsData)

	switch {
	case strings.Contains(desc, "latency") || strings.Contains(desc, "timeout"):
		reasoning.Cause = "Database query latency spike — possible connection pool exhaustion or missing index"
		reasoning.Confidence = 0.82
		reasoning.Severity = "high"
		reasoning.Action = "alert"
		reasoning.Reasoning = "Multiple high-latency requests observed. DB query performance or network saturation is the most probable cause."

	case strings.Contains(desc, "500") || strings.Contains(desc, "error"):
		reasoning.Cause = "Downstream service failure causing cascading HTTP 5xx errors"
		reasoning.Confidence = 0.88
		reasoning.Severity = "critical"
		reasoning.Action = "escalate"
		reasoning.Reasoning = "Elevated 5xx error rate detected across multiple requests. Service dependency failure or config regression likely."
		reasoning.Tools = []string{"fetch_incidents", "send_alert", "monitor_recovery"}

	default:
		reasoning.Cause = "Anomalous traffic pattern — baseline deviation detected"
		reasoning.Confidence = 0.70
		reasoning.Severity = "medium"
		reasoning.Action = "monitor"
		reasoning.Reasoning = "Metrics show deviation from baseline but root cause is unclear. Monitoring recommended."
		reasoning.Tools = []string{"fetch_metrics", "monitor_recovery"}
	}

	_ = time.Now() // mark as used
	return reasoning
}
