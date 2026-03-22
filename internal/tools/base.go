package tools

import "context"

// Tool defines the interface for agent tools
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any // JSON Schema
    Execute(ctx context.Context, params map[string]any) (any, error)
}
