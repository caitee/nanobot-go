package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Registry manages plugin registration and lifecycle.
type Registry struct {
	plugins map[string]Plugin
	order   []string
	mu      sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]Plugin),
	}
}

func (r *Registry) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.plugins[p.Name()]; exists {
		return fmt.Errorf("plugin already registered: %s", p.Name())
	}

	r.plugins[p.Name()] = p
	r.order = append(r.order, p.Name())
	slog.Debug("plugin registered", "name", p.Name(), "type", p.Type())
	return nil
}

func (r *Registry) Get(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

func (r *Registry) GetByType(t Type) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, name := range r.order {
		if p, ok := r.plugins[name]; ok && p.Type() == t {
			result = append(result, p)
		}
	}
	return result
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

func (r *Registry) InitAll(ctx context.Context, app AppContext) error {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	plugins := make(map[string]Plugin, len(r.plugins))
	for k, v := range r.plugins {
		plugins[k] = v
	}
	r.mu.RUnlock()

	for _, name := range order {
		p := plugins[name]
		slog.Info("initializing plugin", "name", name, "type", p.Type())
		if err := p.Init(ctx, app); err != nil {
			return fmt.Errorf("failed to init plugin %s: %w", name, err)
		}
	}
	return nil
}

func (r *Registry) CloseAll() error {
	r.mu.RLock()
	order := make([]string, len(r.order))
	copy(order, r.order)
	plugins := make(map[string]Plugin, len(r.plugins))
	for k, v := range r.plugins {
		plugins[k] = v
	}
	r.mu.RUnlock()

	var firstErr error
	for i := len(order) - 1; i >= 0; i-- {
		name := order[i]
		p := plugins[name]
		slog.Info("closing plugin", "name", name)
		if err := p.Close(); err != nil {
			slog.Error("failed to close plugin", "name", name, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
