package providers

import "testing"

func TestFinalizeMinimaxStreamToolCallsPrefersStreamedInputJSON(t *testing.T) {
	toolCalls := finalizeMinimaxStreamToolCalls(map[int]*minimaxStreamToolCall{
		1: {
			ID:        "toolu_123",
			Name:      "search",
			Arguments: map[string]any{},
			InputJSON: `{"query":"nanobot","limit":3}`,
		},
	}, []int{1})

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	args := toolCalls[0].Arguments
	if got := args["query"]; got != "nanobot" {
		t.Fatalf("expected query argument from streamed JSON, got %#v", got)
	}
	if got := args["limit"]; got != float64(3) {
		t.Fatalf("expected limit argument from streamed JSON, got %#v", got)
	}
}

func TestFinalizeMinimaxStreamToolCallsNilArguments(t *testing.T) {
	// When Arguments is nil and InputJSON is present, should parse InputJSON
	toolCalls := finalizeMinimaxStreamToolCalls(map[int]*minimaxStreamToolCall{
		0: {
			ID:        "toolu_456",
			Name:      "read_file",
			Arguments: nil,
			InputJSON: `{"path":"/tmp/test.txt"}`,
		},
	}, []int{0})

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if got := toolCalls[0].Arguments["path"]; got != "/tmp/test.txt" {
		t.Fatalf("expected path from InputJSON, got %#v", got)
	}
}

func TestFinalizeMinimaxStreamToolCallsNoInputJSON(t *testing.T) {
	// When InputJSON is empty, should use Arguments directly
	toolCalls := finalizeMinimaxStreamToolCalls(map[int]*minimaxStreamToolCall{
		0: {
			ID:        "toolu_789",
			Name:      "list_files",
			Arguments: map[string]any{"dir": "/home"},
			InputJSON: "",
		},
	}, []int{0})

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if got := toolCalls[0].Arguments["dir"]; got != "/home" {
		t.Fatalf("expected dir from Arguments, got %#v", got)
	}
}

func TestFinalizeMinimaxStreamToolCallsInvalidJSON(t *testing.T) {
	// When InputJSON is invalid, should fallback to Arguments
	toolCalls := finalizeMinimaxStreamToolCalls(map[int]*minimaxStreamToolCall{
		0: {
			ID:        "toolu_bad",
			Name:      "search",
			Arguments: map[string]any{"q": "fallback"},
			InputJSON: `{invalid json`,
		},
	}, []int{0})

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if got := toolCalls[0].Arguments["q"]; got != "fallback" {
		t.Fatalf("expected fallback to Arguments, got %#v", got)
	}
}

func TestFinalizeMinimaxStreamToolCallsMultiple(t *testing.T) {
	// Multiple tool calls should all be parsed correctly
	toolCalls := finalizeMinimaxStreamToolCalls(map[int]*minimaxStreamToolCall{
		0: {
			ID:        "toolu_a",
			Name:      "read_file",
			Arguments: map[string]any{},
			InputJSON: `{"path":"a.txt"}`,
		},
		1: {
			ID:        "toolu_b",
			Name:      "write_file",
			Arguments: nil,
			InputJSON: `{"path":"b.txt","content":"hello"}`,
		},
	}, []int{0, 1})

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	if got := toolCalls[0].Arguments["path"]; got != "a.txt" {
		t.Fatalf("expected path a.txt, got %#v", got)
	}
	if got := toolCalls[1].Arguments["content"]; got != "hello" {
		t.Fatalf("expected content hello, got %#v", got)
	}
}
