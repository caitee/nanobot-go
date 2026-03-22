package tools

import (
	"context"
	"fmt"
)

// SubagentSpawner is the interface for spawning subagents
type SubagentSpawner interface {
	Spawn(ctx context.Context, task string, label string, originChannel string, originChatID string, sessionKey string) string
	CancelBySession(sessionKey string) int
	GetRunningCount() int
}

type SpawnTool struct {
	manager SubagentSpawner
}

func NewSpawnTool(manager SubagentSpawner) *SpawnTool {
	return &SpawnTool{manager: manager}
}

func (t *SpawnTool) Name() string   { return "spawn" }
func (t *SpawnTool) Description() string { return "Spawn a subagent to execute a task in the background" }
func (t *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":  map[string]any{"type": "string", "description": "The task description for the subagent"},
			"label": map[string]any{"type": "string", "description": "Optional label for the subagent"},
		},
		"required": []any{"task"},
	}
}

func (t *SpawnTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	task, ok := params["task"].(string)
	if !ok || task == "" {
		return nil, fmt.Errorf("task is required")
	}

	label, _ := params["label"].(string)
	if t.manager == nil {
		return nil, fmt.Errorf("subagent manager not configured")
	}

	// Extract session info from context if available
	// For now, use empty defaults - would need to be passed through
	result := t.manager.Spawn(ctx, task, label, "cli", "direct", "")
	return result, nil
}
