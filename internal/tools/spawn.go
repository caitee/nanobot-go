package tools

import "context"

type SpawnTool struct{}

func NewSpawnTool() *SpawnTool { return &SpawnTool{} }
func (t *SpawnTool) Name() string   { return "spawn" }
func (t *SpawnTool) Description() string { return "Spawn a subagent" }
func (t *SpawnTool) Parameters() map[string]any { return map[string]any{} }
func (t *SpawnTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	return "spawn not implemented", nil
}
