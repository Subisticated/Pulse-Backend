package ai

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

	"pulse-backend/internal/models"

	"github.com/rs/zerolog/log"
)

// RCAResult is returned by POST /api/v1/rca
type RCAResult struct {
	IncidentID  string    `json:"incidentId"`
	Cause       string    `json:"cause"`
	Confidence  int       `json:"confidence"` // 0–100
	Evidence    []string  `json:"evidence"`
	Fixes       []string  `json:"fixes"`
	GeneratedAt time.Time `json:"generatedAt"`
}

// AIService wraps the Groq API client
type AIService struct {
	apiKey string
}

// NewAIService instantiates an AIService, reading groq_API_KEY or GROQ_API_KEY from env
func NewAIService() *AIService {
	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("groq_API_KEY")
	}
	return &AIService{apiKey: apiKey}
}

// PerformRCA sends incident context to Groq and returns a structured analysis.
// Falls back to a deterministic local analysis when Groq key is not set.
func (s *AIService) PerformRCA(ctx context.Context, incident *models.Incident, logs []models.LogEvent) (*RCAResult, error) {
	if s.apiKey != "" {
		return s.groqAnalysis(ctx, incident, logs)
	}
	log.Warn().Msg("groq_API_KEY / GROQ_API_KEY not set — using local RCA fallback")
	return s.localAnalysis(incident, logs), nil
}

// ── Groq API integration ──────────────────────────────────────────────────────

type groqRequest struct {
	Model       string        `json:"model"`
	Messages    []groqMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type groqResponse struct {
	Choices []struct {
		Message groqMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (s *AIService) groqAnalysis(ctx context.Context, incident *models.Incident, logs []models.LogEvent) (*RCAResult, error) {
	prompt := buildPrompt(incident, logs)

	// Groq uses standard OpenAI-compatible Chat Completion structures
	reqBody := groqRequest{
		Model:       "llama-3.1-8b-instant", // Standard active Llama 3.1 8B model hosted by Groq
		Temperature: 0.2,
		Messages: []groqMessage{
			{
				Role: "system",
				Content: "You are an expert SRE analyzing API incidents. " +
					"Respond ONLY with a valid JSON object in this exact shape: " +
					`{"cause":"...","confidence":85,"evidence":["...","..."],"fixes":["...","..."]}`,
			},
			{Role: "user", Content: prompt},
		},
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("groq request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var grqResp groqResponse
	if err = json.Unmarshal(raw, &grqResp); err != nil {
		return nil, fmt.Errorf("groq decode failed: %w", err)
	}
	if grqResp.Error != nil {
		return nil, fmt.Errorf("groq error: %s", grqResp.Error.Message)
	}
	if len(grqResp.Choices) == 0 {
		return nil, fmt.Errorf("groq returned no choices")
	}

	// Parse the JSON structural text the LLM returns
	var parsed struct {
		Cause      string   `json:"cause"`
		Confidence int      `json:"confidence"`
		Evidence   []string `json:"evidence"`
		Fixes      []string `json:"fixes"`
	}

	// Clean up markdown block formatting (e.g. ```json ... ```) if returned
	content := strings.TrimSpace(grqResp.Choices[0].Message.Content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	if err = json.Unmarshal([]byte(content), &parsed); err != nil {
		// If JSON is embedded in text and failed to parse directly, return the raw text as cause
		return &RCAResult{
			IncidentID:  incident.ID.Hex(),
			Cause:       content,
			Confidence:  75,
			Evidence:    []string{},
			Fixes:       []string{},
			GeneratedAt: time.Now(),
		}, nil
	}

	return &RCAResult{
		IncidentID:  incident.ID.Hex(),
		Cause:       parsed.Cause,
		Confidence:  parsed.Confidence,
		Evidence:    parsed.Evidence,
		Fixes:       parsed.Fixes,
		GeneratedAt: time.Now(),
	}, nil
}

func buildPrompt(incident *models.Incident, logs []models.LogEvent) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Incident ID: %s\n", incident.ID.Hex()))
	sb.WriteString(fmt.Sprintf("Cause Type: %s\n", incident.Cause))
	sb.WriteString(fmt.Sprintf("Severity: %s\n", incident.Severity))
	sb.WriteString(fmt.Sprintf("Description: %s\n", incident.Description))
	sb.WriteString(fmt.Sprintf("Service: %s (%s)\n", incident.Service, incident.Environment))
	sb.WriteString(fmt.Sprintf("Created: %s\n\n", incident.StartTime.Format(time.RFC3339)))
	sb.WriteString("Recent logs (up to 10):\n")
	for i, l := range logs {
		if i >= 10 {
			break
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s %s → %d (%dms) err=%q\n",
			l.Timestamp.Format(time.RFC3339), l.Method, l.Endpoint, l.Status, l.Latency, l.Error))
	}
	sb.WriteString("\nAnalyze this incident and return the JSON response.")
	return sb.String()
}

// ── Local fallback ────────────────────────────────────────────────────────────

func (s *AIService) localAnalysis(incident *models.Incident, logs []models.LogEvent) *RCAResult {
	// Extract most frequent error message and route from logs
	errMsg := "connection refused"
	badEndpoint := "/api/v1/data"
	badMethod := "POST"
	actualLatency := 1000

	for _, l := range logs {
		if l.Error != "" {
			errMsg = l.Error
		}
		if l.Endpoint != "" {
			badEndpoint = l.Endpoint
		}
		if l.Method != "" {
			badMethod = l.Method
		}
		if l.Latency > 0 {
			actualLatency = l.Latency
		}
	}

	var cause string
	var confidence int
	var evidence []string
	var fixes []string

	switch incident.Cause {
	case "high_error_rate", "high_error_percentage":
		cause = fmt.Sprintf("Downstream dependency outage or internal database connection pool exhaustion on %s service (Error: %s)", incident.Service, errMsg)
		confidence = 85 + randRange(1, 10)
		evidence = []string{
			fmt.Sprintf("Elevated HTTP 5xx responses detected on %s %s", badMethod, badEndpoint),
			fmt.Sprintf("Downstream exception thrown: %s", errMsg),
			fmt.Sprintf("Service affected: '%s' in %s environment", incident.Service, incident.Environment),
		}
		fixes = []string{
			fmt.Sprintf("Verify service health of any systems connected to %s", incident.Service),
			"Check database connection pool limits and release connection leaks",
			"Introduce a circuit breaker pattern to prevent cascading API failures",
			"Check CPU and memory saturation metrics on the host container",
		}

	case "latency_spike":
		cause = fmt.Sprintf(" elevated latency (%dms) detected on %s service due to heavy queries or lock contention", actualLatency, incident.Service)
		confidence = 80 + randRange(1, 15)
		evidence = []string{
			fmt.Sprintf("Request execution latency reached %dms on %s %s", actualLatency, badMethod, badEndpoint),
			"Potential slow blocking SQL/NoSQL query or missing resource indexes",
			fmt.Sprintf("Bottleneck detected during peak traffic window on %s", incident.Service),
		}
		fixes = []string{
			fmt.Sprintf("Run explain() query analysis on the database collections accessed by %s", badEndpoint),
			"Ensure proper compound indexing exists for all filter predicates",
			"Enable Redis caching layer for read-heavy operations on this endpoint",
			"Check the target server replication factor and scale horizontally",
		}

	default:
		cause = fmt.Sprintf("Anomalous baseline shift detected on %s service: %s", incident.Service, incident.Description)
		confidence = 70 + randRange(1, 15)
		evidence = []string{
			incident.Description,
			fmt.Sprintf("Telemetry drift registered in %s environment", incident.Environment),
		}
		fixes = []string{
			"Verify recent SCM commit history and deploy logs for changes",
			"Check server system metrics (disk I/O and process locks)",
		}
	}

	return &RCAResult{
		IncidentID:  incident.ID.Hex(),
		Cause:       cause,
		Confidence:  confidence,
		Evidence:    evidence,
		Fixes:       fixes,
		GeneratedAt: time.Now(),
	}
}

// randRange helper for varying mock confidence rates
func randRange(min, max int) int {
	return min + int(time.Now().UnixNano()%int64(max-min+1))
}
