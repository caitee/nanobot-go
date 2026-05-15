package main

import (
	"testing"
	"time"

	"ori/internal/llm"
	"ori/internal/runtime"
)

func TestReducerBuildsAssistantSegmentsFromRuntimeEvents(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))

	m.reduceRuntimeEvent(runtime.Event{Kind: runtime.EventAgentStart, Timestamp: time.Unix(2, 0)})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(3, 0),
		Data: runtime.MessageUpdateData{
			StreamEvent: llm.StreamEvent{Kind: llm.StreamEventThinkingDelta, Delta: "thinking"},
		},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(4, 0),
		Data: runtime.MessageUpdateData{
			StreamEvent: llm.StreamEvent{Kind: llm.StreamEventTextDelta, Delta: "answer"},
		},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Unix(5, 0),
		Data: runtime.ToolStartData{
			ToolCallID: "tool-1",
			ToolName:   "shell",
			Args:       map[string]any{"cmd": "date"},
		},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: time.Unix(6, 0),
		Data: runtime.ToolEndData{
			ToolCallID: "tool-1",
			ToolName:   "shell",
			Result:     []llm.Content{llm.TextContent{Text: "ok"}},
		},
	})

	asst := m.transcript.activeAssistant()
	if asst == nil {
		t.Fatal("expected active assistant")
	}
	if len(asst.segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindReasoning {
		t.Fatalf("segments[0].kind = %v, want reasoning", asst.segments[0].kind)
	}
	if asst.segments[1].kind != segmentKindText {
		t.Fatalf("segments[1].kind = %v, want text", asst.segments[1].kind)
	}
	if asst.segments[2].kind != segmentKindTool {
		t.Fatalf("segments[2].kind = %v, want tool", asst.segments[2].kind)
	}
	if got := asst.segments[0].reasoning.text; got != "thinking" {
		t.Fatalf("reasoning text = %q, want thinking", got)
	}
	if got := asst.segments[1].text.text; got != "answer" {
		t.Fatalf("text = %q, want answer", got)
	}
	if got := asst.segments[2].tool.result; got != "ok" {
		t.Fatalf("tool result = %q, want ok", got)
	}
	if got := asst.segments[2].tool.status; got != toolStatusDone {
		t.Fatalf("tool status = %v, want done", got)
	}
	if got := asst.status; got != assistantStatusThinking {
		t.Fatalf("assistant status = %v, want thinking", got)
	}
	if got := m.status; got != "thinking" {
		t.Fatalf("model status = %q, want thinking", got)
	}
}

func TestReducerFinalizesFromAgentEndAndIgnoresLaterFallback(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(2, 0),
		Data: runtime.MessageUpdateData{
			StreamEvent: llm.StreamEvent{Kind: llm.StreamEventTextDelta, Delta: "answer"},
		},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventAgentEnd,
		Timestamp: time.Unix(3, 0),
		Data: runtime.AgentEndData{Messages: []runtime.AgentMessage{
			runtime.WrapLLM(llm.AssistantMessage{
				Content: []llm.Content{llm.TextContent{Text: "answer final"}},
			}),
		}},
	})

	m.finalizeTranscriptFromOutbound("fallback duplicate", "", true)

	asst := requireActiveReducerAssistant(t, m)
	if got := asst.status; got != assistantStatusDone {
		t.Fatalf("assistant status = %v, want done", got)
	}
	if got := asst.finalSource; got != finalSourceRuntime {
		t.Fatalf("finalSource = %v, want runtime", got)
	}
	if got := asst.finalText; got != "answer final" {
		t.Fatalf("finalText = %q, want answer final", got)
	}
	if got := asst.streamedText(); got != "answer final" {
		t.Fatalf("streamedText = %q, want answer final", got)
	}
}

func TestReducerUsesOutboundFallbackWhenRuntimeFinalIsMissing(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(2, 0),
		Data: runtime.MessageUpdateData{
			StreamEvent: llm.StreamEvent{Kind: llm.StreamEventTextDelta, Delta: "partial"},
		},
	})

	m.finalizeTranscriptFromOutbound("partial final", "because", true)

	asst := requireActiveReducerAssistant(t, m)
	if got := asst.finalSource; got != finalSourceFallback {
		t.Fatalf("finalSource = %v, want fallback", got)
	}
	if got := finalReasoning(asst); got != "because" {
		t.Fatalf("final reasoning = %q, want because", got)
	}
	if got := asst.finalText; got != "partial final" {
		t.Fatalf("finalText = %q, want partial final", got)
	}
	if got := asst.streamedText(); got != "partial final" {
		t.Fatalf("streamedText = %q, want partial final", got)
	}
	if got := asst.status; got != assistantStatusDone {
		t.Fatalf("assistant status = %v, want done", got)
	}
}

func TestReducerCancelsActiveAssistantWithoutDroppingContent(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(2, 0),
		Data: runtime.MessageUpdateData{
			StreamEvent: llm.StreamEvent{Kind: llm.StreamEventTextDelta, Delta: "partial"},
		},
	})

	m.cancelActiveAssistant()

	asst := requireActiveReducerAssistant(t, m)
	if got := asst.status; got != assistantStatusCancelled {
		t.Fatalf("assistant status = %v, want cancelled", got)
	}
	if m.active {
		t.Fatal("expected model active=false")
	}
	if m.waiting {
		t.Fatal("expected model waiting=false")
	}
	if got := asst.streamedText(); got != "partial" {
		t.Fatalf("streamedText = %q, want partial", got)
	}
}

func TestReducerLateToolEndThenStartPreservesSettledTool(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: time.Unix(5, 0),
		Data: runtime.ToolEndData{
			ToolCallID: "tool-1",
			ToolName:   "shell",
			Result:     []llm.Content{llm.TextContent{Text: "ok"}},
		},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Unix(4, 0),
		Data: runtime.ToolStartData{
			ToolCallID: "tool-1",
			ToolName:   "shell",
			Args:       map[string]any{"cmd": "date"},
		},
	})

	asst := requireActiveReducerAssistant(t, m)
	if len(asst.segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindTool {
		t.Fatalf("segments[0].kind = %v, want tool", asst.segments[0].kind)
	}
	tool := asst.segments[0].tool
	if got := tool.status; got != toolStatusDone {
		t.Fatalf("tool status = %v, want done", got)
	}
	if got := tool.result; got != "ok" {
		t.Fatalf("tool result = %q, want ok", got)
	}
	if tool.orphan {
		t.Fatal("expected orphan=false after start")
	}
	if asst.hasRunningTool() {
		t.Fatal("expected no running tools")
	}
	if got := asst.status; got == assistantStatusRunningTools {
		t.Fatalf("assistant status = %v, should not be running tools", got)
	}
}

func requireActiveReducerAssistant(t *testing.T, m *interactiveModel) *assistantBlock {
	t.Helper()
	asst := m.transcript.activeAssistant()
	if asst == nil {
		t.Fatal("expected active assistant")
	}
	return asst
}

func finalReasoning(asst *assistantBlock) string {
	for i := len(asst.segments) - 1; i >= 0; i-- {
		segment := asst.segments[i]
		if segment.kind == segmentKindReasoning && segment.reasoning != nil {
			return segment.reasoning.text
		}
	}
	return ""
}
