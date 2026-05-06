package tool

import (
	"context"
	"fmt"

	"nanobot-go/internal/llm"
)

// Legacy mirrors the old internal/tools.Tool interface. Callers can pass any
// value that satisfies this interface (including legacy implementations that
// still live in the old package) to FromLegacy and get back a modern
// AgentTool without copying code.
//
// This is a transitional shim: it keeps the M5 migration mechanical — all
// legacy tools become AgentTools by wrapping — and is deleted together with
// the rest of internal/tools once every call site is migrated.
type Legacy interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (any, error)
}

// FromLegacy wraps a Legacy tool as an AgentTool. The wrapper converts the
// tool's return value into a single text content block and leaves execution
// mode at the default.
func FromLegacy(t Legacy, label string) AgentTool {
	if label == "" {
		label = t.Name()
	}
	return &legacyAdapter{legacy: t, label: label}
}

// UnwrapLegacy returns the underlying Legacy implementation if the given
// AgentTool is a FromLegacy wrapper. Returns nil, false otherwise.
func UnwrapLegacy(t AgentTool) (Legacy, bool) {
	if a, ok := t.(*legacyAdapter); ok {
		return a.legacy, true
	}
	return nil, false
}

type legacyAdapter struct {
	legacy Legacy
	label  string
}

// Unwrap returns the underlying Legacy implementation. Callers use this to
// reach methods that aren't part of the AgentTool surface (for example, to
// re-wire a spawner after registry construction).
func (a *legacyAdapter) Unwrap() Legacy { return a.legacy }

func (a *legacyAdapter) Name() string                 { return a.legacy.Name() }
func (a *legacyAdapter) Label() string                { return a.label }
func (a *legacyAdapter) Description() string          { return a.legacy.Description() }
func (a *legacyAdapter) Parameters() map[string]any   { return a.legacy.Parameters() }
func (a *legacyAdapter) ExecutionMode() ExecutionMode { return ExecutionDefault }
func (a *legacyAdapter) PrepareArguments(raw map[string]any) (map[string]any, error) {
	return raw, nil
}

func (a *legacyAdapter) Execute(
	ctx context.Context, callID string, args map[string]any, update UpdateFn,
) (*Result, error) {
	_ = callID
	_ = update
	out, err := a.legacy.Execute(ctx, args)
	if err != nil {
		return nil, err
	}
	return &Result{
		Content: []llm.Content{llm.TextContent{Text: legacyToString(out)}},
		Details: out,
	}, nil
}

func legacyToString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case fmt.Stringer:
		return x.String()
	}
	return fmt.Sprintf("%v", v)
}
