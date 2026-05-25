package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"pulse-backend/internal/wsocket"
)

// AlertTool sends alerts via WebSocket broadcast and optional Discord webhook
type AlertTool struct {
	hub *wsocket.Hub
}

func NewAlertTool(hub *wsocket.Hub) *AlertTool {
	return &AlertTool{hub: hub}
}

func (t *AlertTool) Name() string { return "send_alert" }
func (t *AlertTool) Description() string {
	return "Send an alert to the WebSocket dashboard and Discord webhook"
}

func (t *AlertTool) Run(ctx context.Context, params map[string]interface{}) (string, error) {
	service, _ := params["service"].(string)
	severity, _ := params["severity"].(string)
	message, _ := params["message"].(string)
	sessionID, _ := params["sessionId"].(string)

	if severity == "" {
		severity = "high"
	}

	// ── 1. WebSocket broadcast ──────────────────────────────────────────────
	wsPayload := map[string]interface{}{
		"type":      "agent_alert",
		"sessionId": sessionID,
		"payload": map[string]interface{}{
			"service":   service,
			"severity":  severity,
			"message":   message,
			"timestamp": time.Now().UTC(),
		},
	}
	if data, err := json.Marshal(wsPayload); err == nil && t.hub != nil {
		t.hub.Broadcast(data)
	}

	// ── 2. Discord webhook (optional) ────────────────────────────────────────
	webhookURL := os.Getenv("DISCORD_WEBHOOK")
	if webhookURL != "" {
		icon := "🔴"
		if severity == "medium" {
			icon = "🟡"
		}
		discordBody := map[string]interface{}{
			"content": fmt.Sprintf("%s **[Pulse Agent Alert]** `%s` — %s\n> %s",
				icon, service, severity, message),
		}
		bodyBytes, _ := json.Marshal(discordBody)
		req, _ := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}

	return fmt.Sprintf("Alert sent — service: %s | severity: %s | message: %s", service, severity, message), nil
}
