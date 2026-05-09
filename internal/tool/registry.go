package tool

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"ori/internal/llm"
)

// Registry keeps AgentTool implementations addressable by name.
type Registry interface {
	Register(t AgentTool)
	Unregister(name string)
	Get(name string) (AgentTool, bool)
	Has(name string) bool
	All() []AgentTool
	Definitions() []llm.Tool
	// Execute validates args and dispatches to the named tool. It does NOT
	// emit agent events — the runtime loop drives those.
	Execute(ctx context.Context, name, callID string, args map[string]any, update UpdateFn) (*Result, error)
}

type registry struct {
	mu    sync.RWMutex
	tools map[string]AgentTool
}

// NewRegistry returns a concurrency-safe Registry.
func NewRegistry() Registry {
	return &registry{tools: map[string]AgentTool{}}
}

func (r *registry) Register(t AgentTool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Name()] = t
}

func (r *registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

func (r *registry) Get(name string) (AgentTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *registry) Has(name string) bool {
	_, ok := r.Get(name)
	return ok
}

func (r *registry) All() []AgentTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentTool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

func (r *registry) Definitions() []llm.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]llm.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, Definition(t))
	}
	return out
}

func (r *registry) Execute(
	ctx context.Context, name, callID string, args map[string]any, update UpdateFn,
) (*Result, error) {
	t, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	prepared, err := t.PrepareArguments(args)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	prepared = CastParams(prepared, t.Parameters())
	if errs := ValidateParams(prepared, t.Parameters()); len(errs) > 0 {
		return nil, fmt.Errorf("%s: validation failed: %s", name, strings.Join(errs, "; "))
	}
	return t.Execute(ctx, callID, prepared, update)
}
