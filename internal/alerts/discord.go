package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

type discordPayload struct {
	Content  string         `json:"content,omitempty"`
	Embeds   []discordEmbed `json:"embeds,omitempty"`
}

type discordEmbed struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Color       int            `json:"color"` // decimal color code
	Fields      []embedField   `json:"fields,omitempty"`
	Timestamp   string         `json:"timestamp"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// SendDiscordAlert fires a rich embed alert to a Discord webhook for Critical incidents.
// It reads DISCORD_WEBHOOK from the environment and silently skips if unset.
func SendDiscordAlert(severity, cause, description, service, environment string) {
	webhookURL := os.Getenv("DISCORD_WEBHOOK")
	if webhookURL == "" {
		log.Debug().Msg("DISCORD_WEBHOOK not set — skipping alert")
		return
	}

	// Color codes: Critical = red (15548997), Medium = orange (16753920), Low = yellow (16776960)
	color := 15548997 // default Critical red
	title := "🚨 Critical Incident Detected"
	if severity == "Medium" {
		color = 16753920
		title = "⚠️ Medium Severity Incident"
	} else if severity == "Low" {
		color = 16776960
		title = "ℹ️ Low Severity Incident"
	}

	payload := discordPayload{
		Embeds: []discordEmbed{
			{
				Title:       title,
				Description: description,
				Color:       color,
				Fields: []embedField{
					{Name: "Service", Value: service, Inline: true},
					{Name: "Environment", Value: environment, Inline: true},
					{Name: "Cause", Value: cause, Inline: true},
					{Name: "Severity", Value: severity, Inline: true},
				},
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal Discord alert payload")
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Msg("Failed to send Discord alert")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		log.Warn().Int("status", resp.StatusCode).Msg(fmt.Sprintf("Discord webhook returned non-2xx: %d", resp.StatusCode))
		return
	}

	log.Info().Str("severity", severity).Str("service", service).Msg("Discord alert sent successfully")
}
