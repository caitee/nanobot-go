package llm

import (
	"context"
	"errors"
	"testing"

	oldprov "nanobot-go/internal/providers"
)

// fakeLegacy scripts a streaming response.
type fakeLegacy struct {
	defaultModel string
	chunks       []oldprov.StreamResponse
	streamErr    error
}

func (f *fakeLegacy) Chat(ctx context.Context, messages []oldprov.Message, tools []oldprov.ToolDef, opts oldprov.ChatOptions) (*oldprov.LLMResponse, error) {
	return nil, errors.New("not used in tests")
}

func (f *fakeLegacy) StreamGenerate(ctx context.Context, messages []oldprov.Message, tools []oldprov.ToolDef, opts oldprov.ChatOptions) <-chan oldprov.StreamResponse {
	ch := make(chan oldprov.StreamResponse, len(f.chunks)+1)
	go func() {
		defer close(ch)
		if f.streamErr != nil {
			ch <- oldprov.StreamResponse{Error: f.streamErr}
			return
		}
		for _, c := range f.chunks {
			ch <- c
		}
	}()
	return ch
}

func (f *fakeLegacy) GetDefaultModel() string { return f.defaultModel }

func drain(s EventStream) []StreamEvent {
	var out []StreamEvent
	for ev := range s {
		out = append(out, ev)
	}
	return out
}

func TestBridgeForwardsTextAndDone(t *testing.T) {
	legacy := &fakeLegacy{
		defaultModel: "m",
		chunks: []oldprov.StreamResponse{
			{Chunk: "he"},
			{Chunk: "llo"},
			{Done: true, Content: "hello", FinishReason: "stop"},
		},
	}
	p := FromLegacy(legacy)
	model := Model{ID: "m", Provider: "fake", API: "openai"}

	events := drain(p.Stream(context.Background(), model, Context{}, StreamOptions{}))

	if events[0].Kind != StreamEventStart {
		t.Fatalf("first event = %v", events[0].Kind)
	}
	last := events[len(events)-1]
	if last.Kind != StreamEventDone {
		t.Fatalf("last event = %v", last.Kind)
	}
	if last.Message == nil || last.Message.StopReason != StopReasonStop {
		t.Fatalf("unexpected final: %+v", last.Message)
	}

	var deltas []string
	for _, ev := range events {
		if ev.Kind == StreamEventTextDelta {
			deltas = append(deltas, ev.Delta)
		}
	}
	if len(deltas) != 2 || deltas[0] != "he" || deltas[1] != "llo" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestBridgeForwardsThinking(t *testing.T) {
	legacy := &fakeLegacy{
		chunks: []oldprov.StreamResponse{
			{Chunk: "thinking...", IsReasoning: true},
			{Chunk: "answer", IsReasoning: false},
			{Done: true, Content: "answer", ReasoningContent: "thinking..."},
		},
	}
	p := FromLegacy(legacy)
	events := drain(p.Stream(context.Background(), Model{}, Context{}, StreamOptions{}))

	var thinkingDeltas, textDeltas int
	for _, ev := range events {
		switch ev.Kind {
		case StreamEventThinkingDelta:
			thinkingDeltas++
		case StreamEventTextDelta:
			textDeltas++
		}
	}
	if thinkingDeltas != 1 || textDeltas != 1 {
		t.Fatalf("thinking=%d text=%d", thinkingDeltas, textDeltas)
	}

	done := events[len(events)-1]
	if done.Kind != StreamEventDone {
		t.Fatalf("last kind = %v", done.Kind)
	}
	var hasThinking, hasText bool
	for _, c := range done.Message.Content {
		if _, ok := c.(ThinkingContent); ok {
			hasThinking = true
		}
		if _, ok := c.(TextContent); ok {
			hasText = true
		}
	}
	if !hasThinking || !hasText {
		t.Fatalf("final content missing blocks: %+v", done.Message.Content)
	}
}

func TestBridgeForwardsToolCall(t *testing.T) {
	legacy := &fakeLegacy{
		chunks: []oldprov.StreamResponse{
			{
				Done: true,
				ToolCalls: []oldprov.ToolCall{
					{ID: "1", Name: "x", Arguments: map[string]any{"k": "v"}},
				},
				FinishReason: "tool_use",
			},
		},
	}
	p := FromLegacy(legacy)
	events := drain(p.Stream(context.Background(), Model{}, Context{}, StreamOptions{}))

	done := events[len(events)-1]
	if done.Kind != StreamEventDone {
		t.Fatalf("last = %v", done.Kind)
	}
	if done.Message.StopReason != StopReasonToolUse {
		t.Fatalf("stop = %v", done.Message.StopReason)
	}
	found := false
	for _, c := range done.Message.Content {
		if tc, ok := c.(ToolCallContent); ok && tc.Name == "x" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool call missing in final: %+v", done.Message.Content)
	}
}

func TestBridgeErrorPath(t *testing.T) {
	legacy := &fakeLegacy{streamErr: errors.New("boom")}
	p := FromLegacy(legacy)
	events := drain(p.Stream(context.Background(), Model{}, Context{}, StreamOptions{}))

	last := events[len(events)-1]
	if last.Kind != StreamEventError {
		t.Fatalf("last = %v", last.Kind)
	}
	if last.ErrorMessage != "boom" {
		t.Fatalf("err msg = %q", last.ErrorMessage)
	}
}

func TestRegistryRegisterGetUnregister(t *testing.T) {
	r := NewRegistry()
	legacy := &fakeLegacy{chunks: []oldprov.StreamResponse{{Done: true, FinishReason: "stop"}}}
	r.Register("openai", FromLegacy(legacy), "plugin-A")
	r.Register("anthropic", FromLegacy(legacy), "plugin-B")

	if _, err := r.Get("openai"); err != nil {
		t.Fatalf("openai missing: %v", err)
	}

	r.UnregisterSource("plugin-A")
	if _, err := r.Get("openai"); err == nil {
		t.Fatalf("openai should be gone")
	}
	if _, err := r.Get("anthropic"); err != nil {
		t.Fatalf("anthropic should remain: %v", err)
	}
}

func TestRegistryStreamFnForMissing(t *testing.T) {
	r := NewRegistry()
	fn := r.StreamFnFor("ghost")
	events := drain(fn(context.Background(), Model{}, Context{}, StreamOptions{}))
	if len(events) != 1 || events[0].Kind != StreamEventError {
		t.Fatalf("expected single error event, got %+v", events)
	}
}

func TestMessageToLegacyRoundtripText(t *testing.T) {
	m := UserMessage{Content: []Content{TextContent{Text: "hi"}}}
	leg := messageToLegacy(m)
	if leg.Role != "user" || leg.Content != "hi" {
		t.Fatalf("unexpected: %+v", leg)
	}
}

func TestMessageToLegacyAssistantToolCall(t *testing.T) {
	m := AssistantMessage{
		Content: []Content{
			TextContent{Text: "let me call"},
			ToolCallContent{ID: "1", Name: "x", Arguments: map[string]any{"a": 1}},
		},
	}
	leg := messageToLegacy(m)
	if leg.Content != "let me call" {
		t.Fatalf("text lost: %v", leg.Content)
	}
	if len(leg.ToolCalls) != 1 || leg.ToolCalls[0].Name != "x" {
		t.Fatalf("tool call lost: %+v", leg.ToolCalls)
	}
}
