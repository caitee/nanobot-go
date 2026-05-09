package tool

import (
	"context"

	"ori/internal/llm"
)

// ExecutionMode controls whether a batch of tool calls runs sequentially or
// concurrently. An empty string means "use the agent default".
type ExecutionMode string

const (
	ExecutionDefault    ExecutionMode = ""
	ExecutionSequential ExecutionMode = "sequential"
	ExecutionParallel   ExecutionMode = "parallel"
)

// Result is what Execute returns. Content is sent back to the model; Details
// is opaque structured data for logs/UI; Terminate hints the agent loop to
// stop after the current tool batch when every batch entry agrees.
type Result struct {
	Content   []llm.Content
	Details   any
	Terminate bool
}

// UpdateFn receives partial results while a tool runs. Providing updates is
// optional; callers may pass nil and tools must handle that.
type UpdateFn func(partial Result)

// AgentTool is the runtime-facing tool interface. It is deliberately richer
// than llm.Tool (which only carries schema metadata visible to the model).
type AgentTool interface {
	Name() string
	Label() string
	Description() string
	Parameters() map[string]any
	ExecutionMode() ExecutionMode
	// PrepareArguments lets a tool reshape raw tool-call arguments before
	// schema validation. Returning the raw map unchanged is the default.
	PrepareArguments(raw map[string]any) (map[string]any, error)
	Execute(ctx context.Context, callID string, args map[string]any, update UpdateFn) (*Result, error)
}

// Definition is a convenience struct for listing tools to the LLM.
func Definition(t AgentTool) llm.Tool {
	return llm.Tool{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
	}
}
