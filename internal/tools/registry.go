package tools

import (
	"context"
	"fmt"
	"sync"
)

// ToolRegistry manages tool registration and execution
type ToolRegistry interface {
	Register(tool Tool)
	Unregister(name string)
	Get(name string) Tool
	GetDefinitions() []map[string]any
	Execute(ctx context.Context, name string, params map[string]any) (any, error)
}

type toolRegistry struct {
	tools map[string]Tool
	mu    sync.RWMutex
}

// NewRegistry creates a new tool registry
func NewRegistry() ToolRegistry {
	return &toolRegistry{
		tools: make(map[string]Tool),
	}
}

func (r *toolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

func (r *toolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

func (r *toolRegistry) Get(name string) Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools[name]
}

func (r *toolRegistry) GetDefinitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]map[string]any, 0, len(r.tools))
	for _, tool := range r.tools {
		defs = append(defs, map[string]any{
			"name":        tool.Name(),
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
		})
	}
	return defs
}

func (r *toolRegistry) Execute(ctx context.Context, name string, params map[string]any) (any, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool.Execute(ctx, params)
}
