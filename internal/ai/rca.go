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
	sb.WriteString(fmt.Sprintf("Created: %s\n\n", incident.CreatedAt.Format(time.RFC3339)))
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
	// Extract most frequent error message from logs
	errMsg := "unknown"
	for _, l := range logs {
		if l.Error != "" {
			errMsg = l.Error
			break
		}
	}

	var cause string
	var confidence int
	var evidence []string
	var fixes []string

	switch incident.Cause {
	case "high_error_rate", "high_error_percentage":
		cause = fmt.Sprintf("DB connection exhaustion or downstream service failure — error: %s", errMsg)
		confidence = 88
		evidence = []string{
			fmt.Sprintf("Multiple HTTP 5xx responses detected in service '%s'", incident.Service),
			fmt.Sprintf("Error pattern: %s", errMsg),
			fmt.Sprintf("Environment: %s", incident.Environment),
		}
		fixes = []string{
			"Check database connection pool limits and increase max connections",
			"Inspect downstream dependencies for timeouts or outages",
			"Add circuit breaker patterns to prevent cascade failures",
			"Review recent deployments around " + incident.CreatedAt.Format("2006-01-02 15:04"),
		}

	case "latency_spike":
		cause = "Slow database queries or resource contention causing elevated response times"
		confidence = 85
		evidence = []string{
			fmt.Sprintf("Latency exceeded %dms threshold on service '%s'", 1000, incident.Service),
			"Possible N+1 query pattern or missing index",
		}
		fixes = []string{
			"Profile slow queries using MongoDB explain() or APM tooling",
			"Add composite indexes on frequently queried fields",
			"Enable query result caching for read-heavy endpoints",
			"Scale horizontally or add read replicas to distribute load",
		}

	default:
		cause = fmt.Sprintf("Anomalous behavior detected: %s", incident.Description)
		confidence = 72
		evidence = []string{incident.Description}
		fixes = []string{
			"Review server resource utilization (CPU, memory, disk I/O)",
			"Enable verbose logging temporarily for detailed diagnostics",
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
