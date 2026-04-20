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
func (t *SpawnTool) Description() string { return "Spawn a background subagent to execute a complex or long-running task. Use when a task is independent, time-consuming, or can run in parallel. The subagent works autonomously and notifies when done. For quick tasks or tightly-coupled work, prefer direct execution." }
func (t *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":  map[string]any{"type": "string", "description": "Detailed task description for the subagent to execute"},
			"label": map[string]any{"type": "string", "description": "Optional short label to identify the task"},
		},
		"required": []any{"task"},
		"examples": []any{
			map[string]any{"task": "Analyze the codebase and identify potential refactoring opportunities in the main modules", "label": "code-review"},
			map[string]any{"task": "Search for information about Go best practices and summarize key findings", "label": "research"},
		},
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
