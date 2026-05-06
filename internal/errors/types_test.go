package errors

import (
	stderrors "errors"
	"testing"
)

func TestNewBuildsStructuredError(t *testing.T) {
	err := New(CategoryProvider, SeverityError, CodeProviderAPIKeyMissing, "provider API key missing")
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Category != CategoryProvider {
		t.Fatalf("Category = %q", err.Category)
	}
	if err.Severity != SeverityError {
		t.Fatalf("Severity = %q", err.Severity)
	}
	if err.Code != CodeProviderAPIKeyMissing {
		t.Fatalf("Code = %q", err.Code)
	}
	if err.Message != "provider API key missing" {
		t.Fatalf("Message = %q", err.Message)
	}
	if err.Cause != nil {
		t.Fatalf("Cause = %v; want nil", err.Cause)
	}
	if err.Context != nil {
		t.Fatalf("Context = %v; want nil", err.Context)
	}
	if err.Recoverable {
		t.Fatalf("Recoverable = true; want false")
	}
	if err.Error() != "provider API key missing" {
		t.Fatalf("Error() = %q", err.Error())
	}
}

func TestWrapKeepsCauseAndMetadata(t *testing.T) {
	cause := stderrors.New("deadline exceeded")
	ctx := map[string]any{
		"tool":   "web_search",
		"timeout": 30,
	}

	err := Wrap(cause, CategoryTool, SeverityWarning, CodeToolExecutionTimeout, "tool execution timed out", ctx, true)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Cause != cause {
		t.Fatalf("Cause = %v; want %v", err.Cause, cause)
	}
	if !stderrors.Is(err, cause) {
		t.Fatalf("expected wrapped cause to be discoverable with errors.Is")
	}
	if got := err.Unwrap(); got != cause {
		t.Fatalf("Unwrap() = %v; want %v", got, cause)
	}
	if err.Category != CategoryTool {
		t.Fatalf("Category = %q", err.Category)
	}
	if err.Severity != SeverityWarning {
		t.Fatalf("Severity = %q", err.Severity)
	}
	if err.Code != CodeToolExecutionTimeout {
		t.Fatalf("Code = %q", err.Code)
	}
	if err.Message != "tool execution timed out" {
		t.Fatalf("Message = %q", err.Message)
	}
	if err.Context["tool"] != "web_search" {
		t.Fatalf("Context[tool] = %v", err.Context["tool"])
	}
	if err.Context["timeout"] != 30 {
		t.Fatalf("Context[timeout] = %v", err.Context["timeout"])
	}
	if !err.Recoverable {
		t.Fatalf("Recoverable = false; want true")
	}
}

func TestDetectionHelpersInspectWrappedStructuredErrors(t *testing.T) {
	apiKeyErr := Wrap(stderrors.New("missing env"), CategoryConfig, SeverityError, CodeProviderAPIKeyMissing, "missing API key", nil, false)
	if !IsAPIKeyMissing(apiKeyErr) {
		t.Fatalf("expected IsAPIKeyMissing to match structured error")
	}
	if IsContextOverflow(apiKeyErr) {
		t.Fatalf("did not expect IsContextOverflow to match API key error")
	}

	ctxOverflowErr := Wrap(stderrors.New("too many tokens"), CategoryRuntime, SeverityError, CodeRuntimeContextOverflow, "context window exceeded", map[string]any{"tokens": 200000}, true)
	wrapped := stderrors.New("outer wrapper: " + ctxOverflowErr.Error())
	if IsContextOverflow(wrapped) {
		t.Fatalf("plain string wrapper should not match without unwrap chain")
	}

	chain := Wrap(ctxOverflowErr, CategoryRuntime, SeverityError, CodeRuntimeContextOverflow, "context still exceeded", nil, true)
	if !IsContextOverflow(chain) {
		t.Fatalf("expected IsContextOverflow to match wrapped structured error")
	}

	toolTimeoutErr := Wrap(stderrors.New("context deadline exceeded"), CategoryTool, SeverityWarning, CodeToolExecutionTimeout, "tool timed out", nil, true)
	if !IsToolExecutionTimeout(toolTimeoutErr) {
		t.Fatalf("expected IsToolExecutionTimeout to match structured error")
	}
}
