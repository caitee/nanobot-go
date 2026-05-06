package llm

import (
	"context"
	"fmt"
	"sync"
)

// Registry holds named providers keyed by their canonical API name. A second
// index keeps track of which sourceID registered each provider so callers can
// bulk-unregister (e.g., when a plugin is unloaded).
type Registry struct {
	mu        sync.RWMutex
	providers map[string]registryEntry
}

type registryEntry struct {
	provider Provider
	sourceID string
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]registryEntry{}}
}

// Register stores a provider under name. sourceID is optional; if set it can
// be passed to UnregisterSource to remove every provider registered by the
// same source in one call. Re-registering the same name replaces the entry.
func (r *Registry) Register(name string, p Provider, sourceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = registryEntry{provider: p, sourceID: sourceID}
}

// Unregister removes a single named provider.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.providers, name)
}

// UnregisterSource removes every provider registered with the given sourceID.
func (r *Registry) UnregisterSource(sourceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, e := range r.providers {
		if e.sourceID == sourceID {
			delete(r.providers, name)
		}
	}
}

// Get returns the provider registered under name.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return e.provider, nil
}

// List returns the names of every registered provider.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for name := range r.providers {
		out = append(out, name)
	}
	return out
}

// StreamFnFor returns a StreamFn bound to the provider registered under name.
// The returned function looks the provider up on every call so a re-register
// takes effect immediately.
func (r *Registry) StreamFnFor(name string) StreamFn {
	return func(ctx context.Context, model Model, c Context, opts StreamOptions) EventStream {
		p, err := r.Get(name)
		if err != nil {
			ch := make(chan StreamEvent, 1)
			ch <- StreamEvent{
				Kind:         StreamEventError,
				StopReason:   StopReasonError,
				ErrorMessage: err.Error(),
			}
			close(ch)
			return ch
		}
		return p.Stream(ctx, model, c, opts)
	}
}
