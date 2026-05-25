package agent

import (
	"context"
	"fmt"
	"time"

	"pulse-backend/internal/tools"
)

// Executor runs a sequence of tools, updating the session's action log after each step
type Executor struct {
	registry map[string]tools.Tool
}

func newExecutor(toolList []tools.Tool) *Executor {
	reg := make(map[string]tools.Tool, len(toolList))
	for _, t := range toolList {
		reg[t.Name()] = t
	}
	return &Executor{registry: reg}
}

// ExecuteStep runs a single named tool and returns the output string.
// It updates the AgentAction inside the session to reflect execution state.
func (e *Executor) ExecuteStep(
	ctx context.Context,
	session *AgentSession,
	toolName string,
	params map[string]interface{},
) string {
	tool, ok := e.registry[toolName]
	if !ok {
		return fmt.Sprintf("⚠️ Tool '%s' not found in registry", toolName)
	}

	// Record start
	now := time.Now()
	action := AgentAction{
		ID:        fmt.Sprintf("%s-%d", toolName, now.UnixMilli()),
		Tool:      toolName,
		Status:    "executing",
		Input:     fmt.Sprintf("%v", params),
		StartedAt: now,
	}
	session.Actions = append(session.Actions, action)
	idx := len(session.Actions) - 1

	// Execute
	output, err := tool.Run(ctx, params)
	done := time.Now()
	session.Actions[idx].EndedAt = &done

	if err != nil {
		session.Actions[idx].Status = "failed"
		session.Actions[idx].Err = err.Error()
		return fmt.Sprintf("❌ Tool '%s' failed: %s", toolName, err.Error())
	}

	session.Actions[idx].Status = "completed"
	session.Actions[idx].Output = output
	return output
}
