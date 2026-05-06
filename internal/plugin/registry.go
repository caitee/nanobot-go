package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Registry manages plugin registration and lifecycle.
type Registry struct {
	plugins  map[string]Plugin
	metadata map[string]Metadata
	order    []string
	mu       sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		plugins:  make(map[string]Plugin),
		metadata: make(map[string]Metadata),
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

	// 尝试获取 plugin 的元数据
	meta := Metadata{
		Name: p.Name(),
		Type: p.Type(),
	}
	if mp, ok := p.(MetadataProvider); ok {
		meta = mp.GetMetadata()
	}
	r.metadata[p.Name()] = meta

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

// GetMetadata 获取指定 plugin 的元数据
func (r *Registry) GetMetadata(name string) (Metadata, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	meta, ok := r.metadata[name]
	return meta, ok
}

// ListMetadata 列出所有 plugin 的元数据
func (r *Registry) ListMetadata() []Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Metadata, 0, len(r.order))
	for _, name := range r.order {
		if meta, ok := r.metadata[name]; ok {
			result = append(result, meta)
		}
	}
	return result
}

// GetDependencies 获取指定 plugin 的依赖列表
func (r *Registry) GetDependencies(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if meta, ok := r.metadata[name]; ok {
		deps := make([]string, len(meta.Dependencies))
		copy(deps, meta.Dependencies)
		return deps
	}
	return nil
}

// Start 启动指定的 plugin（如果它实现了 Lifecycle 接口）
func (r *Registry) Start(ctx context.Context, name string) error {
	r.mu.RLock()
	p, ok := r.plugins[name]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}

	if lc, ok := p.(Lifecycle); ok {
		slog.Info("starting plugin", "name", name)
		return lc.Start(ctx)
	}

	return nil
}

// Stop 停止指定的 plugin（如果它实现了 Lifecycle 接口）
func (r *Registry) Stop(ctx context.Context, name string) error {
	r.mu.RLock()
	p, ok := r.plugins[name]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}

	if lc, ok := p.(Lifecycle); ok {
		slog.Info("stopping plugin", "name", name)
		return lc.Stop(ctx)
	}

	return nil
}

// Unload 卸载指定的 plugin
func (r *Registry) Unload(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.plugins[name]
	if !ok {
		return fmt.Errorf("plugin not found: %s", name)
	}

	// 检查 plugin 是否可以被卸载
	if meta, ok := r.metadata[name]; ok {
		if !meta.Removable {
			return fmt.Errorf("plugin %s is not removable", name)
		}
	}

	// 先关闭 plugin
	if err := p.Close(); err != nil {
		return fmt.Errorf("failed to close plugin %s: %w", name, err)
	}

	// 从 registry 中移除
	delete(r.plugins, name)
	delete(r.metadata, name)

	// 从 order 中移除
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}

	slog.Info("plugin unloaded", "name", name)
	return nil
}
