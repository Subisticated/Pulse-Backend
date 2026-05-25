package agent

// Planner maps an AgentReasoning decision to an ordered list of tool names to execute.
// It merges the AI's suggested tool list with mandatory steps for the chosen action.

// actionToolMap defines the canonical tool sequence for each action type
var actionToolMap = map[string][]string{
	"monitor":   {"fetch_metrics", "monitor_recovery"},
	"alert":     {"fetch_logs", "fetch_metrics", "send_alert", "monitor_recovery"},
	"escalate":  {"fetch_logs", "fetch_metrics", "fetch_incidents", "send_alert", "monitor_recovery"},
	"rollback":  {"fetch_logs", "fetch_incidents", "send_alert"},
}

// Plan returns the tool execution sequence for a given reasoning result.
// If the AI provided a valid tools list, it is used directly; otherwise
// the action-based default is applied.
func Plan(reasoning *AgentReasoning) []string {
	if reasoning == nil {
		return actionToolMap["monitor"]
	}

	// Prefer AI-suggested tool list when it is non-empty and valid
	if len(reasoning.Tools) > 0 && allKnownTools(reasoning.Tools) {
		return dedup(reasoning.Tools)
	}

	// Fall back to canonical sequence for the chosen action
	if seq, ok := actionToolMap[reasoning.Action]; ok {
		return seq
	}

	return actionToolMap["alert"] // safe default
}

// ── Helpers ───────────────────────────────────────────────────────────────────

var knownTools = map[string]bool{
	"fetch_logs":      true,
	"fetch_metrics":   true,
	"fetch_incidents": true,
	"send_alert":      true,
	"monitor_recovery": true,
}

func allKnownTools(tools []string) bool {
	for _, t := range tools {
		if !knownTools[t] {
			return false
		}
	}
	return true
}

func dedup(tools []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(tools))
	for _, t := range tools {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}
