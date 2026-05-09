package runtime

import (
	"context"
	stderrors "errors"
	"testing"

	"ori/internal/errors"
	"ori/internal/llm"
	"ori/internal/tool"
)

// TestProviderErrorMapping tests that provider errors are mapped to structured errors
func TestProviderErrorMapping(t *testing.T) {
	tests := []struct {
		name          string
		streamEvents  []llm.StreamEvent
		wantCode      errors.Code
		wantCategory  errors.Category
		wantInMessage string
	}{
		{
			name: "provider API key missing",
			streamEvents: []llm.StreamEvent{
				{
					Kind:         llm.StreamEventError,
					StopReason:   llm.StopReasonError,
					ErrorMessage: "API key missing for provider anthropic",
				},
			},
			wantCode:      errors.CodeProviderAPIKeyMissing,
			wantCategory:  errors.CategoryProvider,
			wantInMessage: "API key",
		},
		{
			name: "provider request failed",
			streamEvents: []llm.StreamEvent{
				{
					Kind:         llm.StreamEventError,
					StopReason:   llm.StopReasonError,
					ErrorMessage: "HTTP 500: internal server error",
				},
			},
			wantCode:      errors.CodeProviderRequestFailed,
			wantCategory:  errors.CategoryProvider,
			wantInMessage: "request failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &fakeStream{}
			fs.add(tt.streamEvents...)

			a := newAgent(t, Options{StreamFn: fs.streamFn()})

			err := a.Prompt(context.Background(), "test")
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// Check that error is structured
			var structuredErr *errors.Error
			if !stderrors.As(err, &structuredErr) {
				t.Fatalf("expected structured error, got %T: %v", err, err)
			}

			if structuredErr.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", structuredErr.Code, tt.wantCode)
			}
			if structuredErr.Category != tt.wantCategory {
				t.Errorf("category = %s, want %s", structuredErr.Category, tt.wantCategory)
			}
		})
	}
}

// TestToolErrorMapping tests that tool errors are mapped to structured errors
func TestToolErrorMapping(t *testing.T) {
	toolErr := stderrors.New("tool execution failed")
	ft := &fakeTool{
		name: "failing_tool",
		fail: toolErr,
	}

	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{
		ID: "tc-1", Name: "failing_tool", Arguments: map[string]any{},
	})...)
	fs.add(assistantDone("done")...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
	})

	err := a.Prompt(context.Background(), "test")
	// Tool errors should not bubble up as agent errors
	// They should be captured in tool result messages
	if err != nil {
		t.Fatalf("tool errors should not bubble up: %v", err)
	}

	// Check that the tool result message contains error
	msgs := a.State().Messages()
	var foundToolResult bool
	for _, msg := range msgs {
		if msg.AgentRole() == string(llm.RoleToolResult) {
			foundToolResult = true
			break
		}
	}
	if !foundToolResult {
		t.Fatal("expected tool result message in transcript")
	}
}

// TestRuntimeInternalErrorMapping tests runtime internal errors
func TestRuntimeInternalErrorMapping(t *testing.T) {
	// Test transformContext hook error
	a := newAgent(t, Options{
		StreamFn: (&fakeStream{}).streamFn(),
		TransformContext: func(ctx context.Context, msgs []AgentMessage) ([]AgentMessage, error) {
			return nil, stderrors.New("transform failed")
		},
	})

	err := a.Prompt(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from transformContext")
	}

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

// TestConvertToLLMErrorMapping tests convertToLLM errors
func TestConvertToLLMErrorMapping(t *testing.T) {
	fs := &fakeStream{}
	fs.add(assistantDone("ok")...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		ConvertToLLM: func(msgs []AgentMessage) ([]llm.Message, error) {
			return nil, stderrors.New("conversion failed")
		},
	})

	err := a.Prompt(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from convertToLLM")
	}

	var structuredErr *errors.Error
	if !stderrors.As(err, &structuredErr) {
		t.Fatalf("expected structured error, got %T: %v", err, err)
	}

	if structuredErr.Code != errors.CodeRuntimeInternalError {
		t.Errorf("code = %s, want %s", structuredErr.Code, errors.CodeRuntimeInternalError)
	}
}

// TestGetAPIKeyErrorMapping tests getAPIKey hook errors
func TestGetAPIKeyErrorMapping(t *testing.T) {
	fs := &fakeStream{}
	fs.add(assistantDone("ok")...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		GetAPIKey: func(ctx context.Context, provider string) (string, error) {
			return "", stderrors.New("API key not found")
		},
	})

	err := a.Prompt(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from getAPIKey")
	}

	var structuredErr *errors.Error
	if !stderrors.As(err, &structuredErr) {
		t.Fatalf("expected structured error, got %T: %v", err, err)
	}

	// getAPIKey errors should be mapped to provider API key missing
	if structuredErr.Code != errors.CodeProviderAPIKeyMissing {
		t.Errorf("code = %s, want %s", structuredErr.Code, errors.CodeProviderAPIKeyMissing)
	}
	if structuredErr.Category != errors.CategoryProvider {
		t.Errorf("category = %s, want %s", structuredErr.Category, errors.CategoryProvider)
	}
}
