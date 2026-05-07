package main

import (
	"strings"
	"testing"
	"time"

	appcore "nanobot-go/internal/app"
	"nanobot-go/internal/llm"
	"nanobot-go/internal/runtime"
)

func newTestModel() *interactiveModel {
	return &interactiveModel{active: true}
}

func TestView_RendersLiveToolCall(t *testing.T) {
	m := newTestModel()

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Now(),
		Data: runtime.ToolStartData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Args:       map[string]any{"path": "/tmp/demo.md"},
		},
	})

	view := m.View()
	if !strings.Contains(view, "read_file") {
		t.Fatalf("expected live View to show tool name; got:\n%s", view)
	}
	if !strings.Contains(view, "/tmp/demo.md") {
		t.Fatalf("expected live View to show tool args; got:\n%s", view)
	}
}

func TestHandleRuntimeEvent_RendersToolCallsInFinalOutput(t *testing.T) {
	m := newTestModel()

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	startAt := time.Now()
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: startAt,
		Data: runtime.ToolStartData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Args:       map[string]any{"path": "/tmp/demo.md"},
		},
	})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: startAt.Add(25 * time.Millisecond),
		Data: runtime.ToolEndData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Result:     []llm.Content{llm.TextContent{Text: "1\thello world"}},
			IsError:    false,
		},
	})

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	if m.currentRound == nil {
		m.currentRound = &thinkingRound{}
	}
	m.currentRound.reasoning = "I have the file contents now."

	text := "📄 demo.md 内容:\n\nhello world"
	out := m.formatFinalMessage(text, "I have the file contents now.")

	if !strings.Contains(out, "read_file") {
		t.Fatalf("expected final output to include tool name; got:\n%s", out)
	}
	if !strings.Contains(out, "Args:") {
		t.Fatalf("expected final output to include tool args; got:\n%s", out)
	}
	if !strings.Contains(out, "Result:") {
		t.Fatalf("expected final output to include tool result; got:\n%s", out)
	}
}

func TestAgentEnd_PrintsFinalOutputWithToolCallsFromSameTurn(t *testing.T) {
	m := newTestModel()
	var printed string
	m.printAboveFn = func(s string) { printed = s }

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	startAt := time.Now()
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: startAt,
		Data: runtime.ToolStartData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Args:       map[string]any{"path": "/tmp/demo.md"},
		},
	})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: startAt.Add(25 * time.Millisecond),
		Data: runtime.ToolEndData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Result:     []llm.Content{llm.TextContent{Text: "1\thello world"}},
			IsError:    false,
		},
	})

	assistant := runtime.WrapLLM(llm.AssistantMessage{
		Content: []llm.Content{
			llm.TextContent{Text: "📄 demo.md 内容:\n\nhello world"},
			llm.ThinkingContent{Thinking: "final reasoning"},
		},
		StopReason: llm.StopReasonStop,
		Timestamp:  time.Now(),
	})
	cmd := m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventAgentEnd,
		Timestamp: time.Now(),
		Data:      runtime.AgentEndData{Messages: []runtime.AgentMessage{assistant}},
	})
	if cmd == nil {
		t.Fatal("expected finalize command on agent end")
	}
	cmd()

	if printed == "" {
		t.Fatal("expected finalize to print output above the TUI, got empty string")
	}
	if !strings.Contains(printed, "read_file") {
		t.Fatalf("expected final printed output to include tool name; got:\n%s", printed)
	}
	if !strings.Contains(printed, "Args:") {
		t.Fatalf("expected final printed output to include tool args; got:\n%s", printed)
	}
	if !strings.Contains(printed, "Result:") {
		t.Fatalf("expected final printed output to include tool result; got:\n%s", printed)
	}
}

func TestAgentEnd_PrintsFinalOutputWithToolCallsFromPreviousTurn(t *testing.T) {
	m := newTestModel()
	var printed string
	m.printAboveFn = func(s string) { printed = s }

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	startAt := time.Now()
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: startAt,
		Data: runtime.ToolStartData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Args:       map[string]any{"path": "/tmp/demo.md"},
		},
	})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: startAt.Add(10 * time.Millisecond),
		Data: runtime.ToolEndData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Result:     []llm.Content{llm.TextContent{Text: "1\thello world"}},
			IsError:    false,
		},
	})

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	if m.currentRound == nil {
		m.currentRound = &thinkingRound{}
	}
	m.currentRound.reasoning = "wrapping up"

	assistant := runtime.WrapLLM(llm.AssistantMessage{
		Content: []llm.Content{
			llm.TextContent{Text: "📄 demo.md 内容:\n\nhello world"},
			llm.ThinkingContent{Thinking: "wrapping up"},
		},
		StopReason: llm.StopReasonStop,
		Timestamp:  time.Now(),
	})
	cmd := m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventAgentEnd,
		Timestamp: time.Now(),
		Data:      runtime.AgentEndData{Messages: []runtime.AgentMessage{assistant}},
	})
	if cmd == nil {
		t.Fatal("expected finalize command on agent end")
	}
	cmd()

	if !strings.Contains(printed, "read_file") {
		t.Fatalf("expected final printed output to include read_file from a prior turn; got:\n%s", printed)
	}
	if !strings.Contains(printed, "hello world") {
		t.Fatalf("expected final printed output to include assistant answer; got:\n%s", printed)
	}
}

func TestExtractFinalAssistant_ReturnsTextAndThinking(t *testing.T) {
	messages := []runtime.AgentMessage{
		runtime.WrapLLM(llm.AssistantMessage{
			Content: []llm.Content{
				llm.TextContent{Text: "answer"},
				llm.ThinkingContent{Thinking: "reasoning"},
			},
			StopReason: llm.StopReasonStop,
			Timestamp:  time.Now(),
		}),
	}
	text, reasoning := appcore.ExtractFinalAssistant(messages)
	if text != "answer" {
		t.Fatalf("expected text 'answer', got %q", text)
	}
	if reasoning != "reasoning" {
		t.Fatalf("expected reasoning 'reasoning', got %q", reasoning)
	}
}
