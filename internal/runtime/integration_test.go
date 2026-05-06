package runtime_test

import (
	"context"
	"testing"

	"nanobot-go/internal/llm"
	oldprov "nanobot-go/internal/providers"
	"nanobot-go/internal/runtime"
	"nanobot-go/internal/tool"
)

// fakeLegacy drives the llm bridge end-to-end via the legacy shape.
type fakeLegacy struct {
	scripts [][]oldprov.StreamResponse
}

func (f *fakeLegacy) Chat(ctx context.Context, messages []oldprov.Message, tools []oldprov.ToolDef, opts oldprov.ChatOptions) (*oldprov.LLMResponse, error) {
	return nil, nil
}

func (f *fakeLegacy) StreamGenerate(ctx context.Context, messages []oldprov.Message, tools []oldprov.ToolDef, opts oldprov.ChatOptions) <-chan oldprov.StreamResponse {
	ch := make(chan oldprov.StreamResponse, 8)
	go func() {
		defer close(ch)
		if len(f.scripts) == 0 {
			return
		}
		s := f.scripts[0]
		f.scripts = f.scripts[1:]
		for _, c := range s {
			ch <- c
		}
	}()
	return ch
}
func (f *fakeLegacy) GetDefaultModel() string { return "m" }

func TestIntegrationRuntimeOverLegacyProvider(t *testing.T) {
	legacy := &fakeLegacy{
		scripts: [][]oldprov.StreamResponse{
			{
				{Chunk: "hello "},
				{Chunk: "world"},
				{Done: true, Content: "hello world", FinishReason: "stop"},
			},
		},
	}
	provider := llm.FromLegacy(legacy)

	a, err := runtime.New(runtime.Options{
		Model:    llm.Model{ID: "m", Provider: "fake", API: "openai"},
		StreamFn: provider.Stream,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.Prompt(context.Background(), "ping"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	msgs := a.State().Messages()
	if len(msgs) != 2 {
		t.Fatalf("messages = %d; want 2", len(msgs))
	}
	// Assistant message should contain text "hello world"
	am, ok := runtime.Unwrap(msgs[1])
	if !ok {
		t.Fatalf("cannot unwrap assistant")
	}
	asst := am.(llm.AssistantMessage)
	var text string
	for _, c := range asst.Content {
		if tc, ok := c.(llm.TextContent); ok {
			text += tc.Text
		}
	}
	if text != "hello world" {
		t.Fatalf("text = %q", text)
	}
}

func TestIntegrationToolCallRoundtrip(t *testing.T) {
	legacy := &fakeLegacy{
		scripts: [][]oldprov.StreamResponse{
			// Turn 1: model requests tool
			{
				{
					Done: true,
					ToolCalls: []oldprov.ToolCall{
						{ID: "1", Name: "greet", Arguments: map[string]any{"name": "world"}},
					},
					FinishReason: "tool_use",
				},
			},
			// Turn 2: final answer
			{
				{Done: true, Content: "hello world", FinishReason: "stop"},
			},
		},
	}
	provider := llm.FromLegacy(legacy)

	greet := &stubTool{
		name: "greet",
		fn: func(args map[string]any) (*tool.Result, error) {
			n := args["name"].(string)
			return &tool.Result{Content: []llm.Content{llm.TextContent{Text: "hi " + n}}}, nil
		},
	}
	a, _ := runtime.New(runtime.Options{
		Model:    llm.Model{ID: "m", Provider: "fake", API: "openai"},
		StreamFn: provider.Stream,
		Tools:    []tool.AgentTool{greet},
	})
	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	// Expected transcript: user + assistant(tool call) + toolResult + assistant(final)
	msgs := a.State().Messages()
	if len(msgs) != 4 {
		t.Fatalf("messages = %d; want 4 (user/asst1/tool/asst2)", len(msgs))
	}
}

type stubTool struct {
	name string
	fn   func(map[string]any) (*tool.Result, error)
}

func (s *stubTool) Name() string                 { return s.name }
func (s *stubTool) Label() string                { return s.name }
func (s *stubTool) Description() string          { return s.name }
func (s *stubTool) Parameters() map[string]any   { return map[string]any{"type": "object"} }
func (s *stubTool) ExecutionMode() tool.ExecutionMode {
	return tool.ExecutionDefault
}
func (s *stubTool) PrepareArguments(r map[string]any) (map[string]any, error) {
	return r, nil
}
func (s *stubTool) Execute(ctx context.Context, id string, args map[string]any, u tool.UpdateFn) (*tool.Result, error) {
	return s.fn(args)
}
