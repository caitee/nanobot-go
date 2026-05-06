package runtime

import (
	"context"
	stderrors "errors"
	"testing"

	"nanobot-go/internal/errors"
	"nanobot-go/internal/llm"
	"nanobot-go/internal/tool"
)

// TestErrorPropagationToAgentState tests that errors are properly stored in agent state
func TestErrorPropagationToAgentState(t *testing.T) {
	fs := &fakeStream{}
	fs.add([]llm.StreamEvent{
		{
			Kind:         llm.StreamEventError,
			StopReason:   llm.StopReasonError,
			ErrorMessage: "API key missing for provider anthropic",
		},
	}...)

	a := newAgent(t, Options{StreamFn: fs.streamFn()})

	err := a.Prompt(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Check that error is stored in agent state
	snapshot := a.Snapshot()
	if snapshot.ErrorMessage == "" {
		t.Error("expected error message in agent state")
	}

	// Check that the last message is an assistant message with error
	msgs := a.State().Messages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}

	lastMsg := msgs[len(msgs)-1]
	if lastMsg.AgentRole() != string(llm.RoleAssistant) {
		t.Errorf("last message role = %s, want assistant", lastMsg.AgentRole())
	}

	// Verify the error is structured
	var structuredErr *errors.Error
	if !stderrors.As(err, &structuredErr) {
		t.Fatalf("expected structured error, got %T", err)
	}

	if structuredErr.Code != errors.CodeProviderAPIKeyMissing {
		t.Errorf("code = %s, want %s", structuredErr.Code, errors.CodeProviderAPIKeyMissing)
	}
}

// TestErrorFormattingIntegration tests that structured errors can be formatted for users
func TestErrorFormattingIntegration(t *testing.T) {
	fs := &fakeStream{}
	fs.add([]llm.StreamEvent{
		{
			Kind:         llm.StreamEventError,
			StopReason:   llm.StopReasonError,
			ErrorMessage: "API key missing for provider anthropic",
		},
	}...)

	a := newAgent(t, Options{StreamFn: fs.streamFn()})

	err := a.Prompt(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Test that FormatUserMessage works with the error
	userMsg := errors.FormatUserMessage(err)
	if userMsg == "" {
		t.Error("expected non-empty user message")
	}

	// Should contain provider name
	if !contains(userMsg, "anthropic") {
		t.Errorf("expected user message to contain 'anthropic', got: %s", userMsg)
	}
}

// TestShouldStopAfterErrorMapping tests that shouldStopAfter errors are mapped
func TestShouldStopAfterErrorMapping(t *testing.T) {
	ft := &fakeTool{
		name:   "test_tool",
		result: &tool.Result{Content: []llm.Content{llm.TextContent{Text: "ok"}}},
	}

	fs := &fakeStream{}
	// First turn: tool use
	fs.add(assistantToolUse(llm.ToolCallContent{
		ID: "tc-1", Name: "test_tool", Arguments: map[string]any{},
	})...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
		ShouldStopAfter: func(ctx context.Context, c ShouldStopContext) (bool, error) {
			return false, stderrors.New("shouldStopAfter failed")
		},
	})

	err := a.Prompt(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from shouldStopAfter")
	}

	// The error should be wrapped as a runtime error
	var structuredErr *errors.Error
	if !stderrors.As(err, &structuredErr) {
		t.Fatalf("expected structured error, got %T: %v", err, err)
	}

	if structuredErr.Code != errors.CodeRuntimeInternalError {
		t.Errorf("code = %s, want %s", structuredErr.Code, errors.CodeRuntimeInternalError)
	}
	if structuredErr.Category != errors.CategoryRuntime {
		t.Errorf("category = %s, want %s", structuredErr.Category, errors.CategoryRuntime)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
