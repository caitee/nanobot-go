package tools

import "context"

type MCPTool struct{}

func NewMCPTool() *MCPTool { return &MCPTool{} }
func (t *MCPTool) Name() string   { return "mcp" }
func (t *MCPTool) Description() string { return "MCP protocol tool" }
func (t *MCPTool) Parameters() map[string]any { return map[string]any{} }
func (t *MCPTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	return "mcp not implemented", nil
}
