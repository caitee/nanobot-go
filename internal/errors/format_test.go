package errors

import (
	stderrors "errors"
	"strings"
	"testing"
)

func TestFormatUserMessageForAPIKeyMissing(t *testing.T) {
	err := Wrap(stderrors.New("missing env var"), CategoryProvider, SeverityError, CodeProviderAPIKeyMissing, "provider API key missing", map[string]any{"provider": "openai"}, false)
	msg := FormatUserMessage(err)
	if !strings.Contains(msg, "API key") {
		t.Fatalf("expected API key guidance, got %q", msg)
	}
	if !strings.Contains(msg, "openai") {
		t.Fatalf("expected provider name in message, got %q", msg)
	}
}

func TestFormatUserMessageForContextOverflow(t *testing.T) {
	err := Wrap(stderrors.New("too many tokens"), CategoryRuntime, SeverityError, CodeRuntimeContextOverflow, "context window exceeded", nil, true)
	msg := FormatUserMessage(err)
	if !strings.Contains(msg, "context") {
		t.Fatalf("expected context guidance, got %q", msg)
	}
	if !strings.Contains(msg, "shorter") {
		t.Fatalf("expected recovery guidance, got %q", msg)
	}
}

func TestFormatUserMessageForToolExecutionTimeout(t *testing.T) {
	err := Wrap(stderrors.New("deadline exceeded"), CategoryTool, SeverityWarning, CodeToolExecutionTimeout, "tool execution timed out", map[string]any{"tool": "web_search"}, true)
	msg := FormatUserMessage(err)
	if !strings.Contains(msg, "web_search") {
		t.Fatalf("expected tool name in message, got %q", msg)
	}
	if !strings.Contains(msg, "timed out") {
		t.Fatalf("expected timeout guidance, got %q", msg)
	}
}

func TestFormatUserMessageFallsBackToStructuredMessage(t *testing.T) {
	err := Wrap(stderrors.New("boom"), CategoryPlugin, SeverityError, CodePluginLoadFailed, "plugin failed to load", nil, false)
	msg := FormatUserMessage(err)
	if msg != "plugin failed to load" {
		t.Fatalf("FormatUserMessage() = %q", msg)
	}
}

func TestFormatUserMessageFallsBackToGenericErrorText(t *testing.T) {
	err := stderrors.New("plain failure")
	msg := FormatUserMessage(err)
	if msg != "plain failure" {
		t.Fatalf("FormatUserMessage() = %q", msg)
	}
}
