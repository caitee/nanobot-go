package providers

import (
    "fmt"
    "sync"
)

// Registry manages LLM provider instances
type Registry struct {
    providers map[string]LLMProvider
    mu        sync.RWMutex
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
    return &Registry{
        providers: make(map[string]LLMProvider),
    }
}

// Register adds a provider to the registry
func (r *Registry) Register(name string, p LLMProvider) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.providers[name] = p
}

// Get retrieves a provider by name
func (r *Registry) Get(name string) (LLMProvider, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    p, ok := r.providers[name]
    if !ok {
        return nil, fmt.Errorf("provider not found: %s", name)
    }
    return p, nil
}

// List returns all registered provider names
func (r *Registry) List() []string {
    r.mu.RLock()
    defer r.mu.RUnlock()
    var names []string
    for name := range r.providers {
        names = append(names, name)
    }
    return names
}
