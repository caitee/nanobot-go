package main

import (
	"testing"
	"time"
)

func TestMergeFinalText(t *testing.T) {
	tests := []struct {
		name         string
		streamed     string
		final        string
		wantText     string
		wantConflict bool
	}{
		{name: "final empty keeps streamed", streamed: "hello", final: "", wantText: "hello"},
		{name: "streamed empty uses final", streamed: "", final: "hello", wantText: "hello"},
		{name: "final extends streamed", streamed: "hello", final: "hello world", wantText: "hello world"},
		{name: "streamed extends final", streamed: "hello world", final: "hello", wantText: "hello world"},
		{name: "mismatch prefers final", streamed: "draft", final: "final", wantText: "final", wantConflict: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotConflict := mergeFinalText(tt.streamed, tt.final)
			if gotText != tt.wantText || gotConflict != tt.wantConflict {
				t.Fatalf("mergeFinalText(%q, %q) = (%q, %v), want (%q, %v)",
					tt.streamed, tt.final, gotText, gotConflict, tt.wantText, tt.wantConflict)
			}
		})
	}
}

func TestTranscriptMergesAdjacentTextAndReasoningSegments(t *testing.T) {
	var tr transcript
	tr.appendUserBlock("u1", "hello", time.Unix(1, 0))
	asst := tr.appendAssistantBlock("a1", time.Unix(2, 0))
	if asst.status != assistantStatusWaiting {
		t.Fatalf("assistant status = %v, want waiting", asst.status)
	}
	start := time.Unix(3, 0)
	end := time.Unix(4, 0)

	asst.appendReasoningDelta("think", start)
	asst.appendReasoningDelta(" more", end)
	asst.appendTextDelta("answer", start)
	asst.appendTextDelta(" now", end)

	if len(tr.blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(tr.blocks))
	}
	if tr.activeAssistantID != "a1" {
		t.Fatalf("activeAssistantID = %q, want a1", tr.activeAssistantID)
	}
	if len(asst.segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindReasoning {
		t.Fatalf("segments[0].kind = %v, want reasoning", asst.segments[0].kind)
	}
	if asst.segments[1].kind != segmentKindText {
		t.Fatalf("segments[1].kind = %v, want text", asst.segments[1].kind)
	}
	if got := asst.segments[0].reasoning.text; got != "think more" {
		t.Fatalf("reasoning text = %q", got)
	}
	if got := asst.segments[1].text.text; got != "answer now" {
		t.Fatalf("text segment = %q", got)
	}
	if got := asst.segments[0].createdAt; !got.Equal(start) {
		t.Fatalf("reasoning createdAt = %v, want %v", got, start)
	}
	if got := asst.segments[0].updatedAt; !got.Equal(end) {
		t.Fatalf("reasoning updatedAt = %v, want %v", got, end)
	}
	if got := asst.segments[1].createdAt; !got.Equal(start) {
		t.Fatalf("text createdAt = %v, want %v", got, start)
	}
	if got := asst.segments[1].updatedAt; !got.Equal(end) {
		t.Fatalf("text updatedAt = %v, want %v", got, end)
	}
}

func TestAssistantUpsertsToolSegments(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	start := time.Unix(3, 0)
	end := start.Add(250 * time.Millisecond)

	tool := asst.upsertToolStart("call-1", "shell", map[string]any{"cmd": "date"}, start)
	tool.partial = "running"
	asst.finishTool("call-1", "shell", "done", false, end)

	if len(asst.segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindTool {
		t.Fatalf("segments[0].kind = %v, want tool", asst.segments[0].kind)
	}
	got := asst.segments[0].tool
	if got == nil || got.name != "shell" || got.status != toolStatusDone || got.result != "done" {
		t.Fatalf("tool segment not finished correctly: %+v", got)
	}
	if got.durationMs != 250 {
		t.Fatalf("durationMs = %d, want 250", got.durationMs)
	}
}

func TestAssistantCreatesOrphanToolWhenEndArrivesFirst(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	asst.finishTool("missing", "web", "late result", false, time.Unix(4, 0))

	if len(asst.segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindTool {
		t.Fatalf("segments[0].kind = %v, want tool", asst.segments[0].kind)
	}
	got := asst.segments[0].tool
	if got == nil || !got.orphan || got.status != toolStatusDone || got.result != "late result" {
		t.Fatalf("orphan tool not rendered into transcript state: %+v", got)
	}
}

func TestAssistantFinalTextPreservesToolOrdering(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	start := time.Unix(5, 0)
	toolStart := start.Add(time.Second)
	finalAt := start.Add(2 * time.Second)

	asst.appendTextDelta("first ", start)
	asst.upsertToolStart("call-1", "shell", map[string]any{"cmd": "date"}, toolStart)
	asst.appendTextDelta("second", toolStart)
	asst.setFinalText(finalSourceRuntime, "first second final", finalAt)

	if len(asst.segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindText {
		t.Fatalf("segments[0].kind = %v, want text", asst.segments[0].kind)
	}
	if asst.segments[1].kind != segmentKindTool {
		t.Fatalf("segments[1].kind = %v, want tool", asst.segments[1].kind)
	}
	if asst.segments[2].kind != segmentKindText {
		t.Fatalf("segments[2].kind = %v, want text", asst.segments[2].kind)
	}
	if got := asst.segments[0].text.text; got != "first " {
		t.Fatalf("first text segment = %q, want first ", got)
	}
	if got := asst.segments[2].text.text; got != "second final" {
		t.Fatalf("last text segment = %q, want final suffix", got)
	}
	if got := asst.segments[2].updatedAt; !got.Equal(finalAt) {
		t.Fatalf("last text updatedAt = %v, want %v", got, finalAt)
	}
	if got := asst.finalText; got != "first second final" {
		t.Fatalf("finalText = %q, want final text", got)
	}
	if got := asst.streamedText(); got != "first second final" {
		t.Fatalf("streamedText = %q, want final text", got)
	}
}

func TestAssistantCreatesOrphanToolWhenMismatchedIDUsesSameName(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	start := time.Unix(6, 0)
	end := start.Add(250 * time.Millisecond)

	asst.upsertToolStart("call-1", "shell", map[string]any{"cmd": "date"}, start)
	asst.finishTool("call-2", "shell", "late result", false, end)

	if len(asst.segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(asst.segments))
	}
	for i := range asst.segments {
		if asst.segments[i].kind != segmentKindTool {
			t.Fatalf("segments[%d].kind = %v, want tool", i, asst.segments[i].kind)
		}
	}
	first := asst.segments[0].tool
	if first == nil || first.id != "call-1" || first.status != toolStatusRunning || first.result != "" {
		t.Fatalf("first tool was overwritten: %+v", first)
	}
	second := asst.segments[1].tool
	if second == nil || second.id != "call-2" || !second.orphan || second.status != toolStatusDone || second.result != "late result" {
		t.Fatalf("mismatched-id orphan not created: %+v", second)
	}
}

func TestAssistantNoIDSameNameToolStartsCreateSeparateSegments(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	t1 := time.Unix(7, 0)
	t2 := t1.Add(time.Second)

	asst.upsertToolStart("", "shell", map[string]any{"cmd": "date"}, t1)
	asst.upsertToolStart("", "shell", map[string]any{"cmd": "pwd"}, t2)

	if len(asst.segments) != 2 {
		t.Fatalf("segments = %d, want 2", len(asst.segments))
	}
	for i := range asst.segments {
		if asst.segments[i].kind != segmentKindTool {
			t.Fatalf("segments[%d].kind = %v, want tool", i, asst.segments[i].kind)
		}
	}
	first := asst.segments[0].tool
	second := asst.segments[1].tool
	if first == nil || second == nil {
		t.Fatalf("tool segments = (%+v, %+v), want both non-nil", first, second)
	}
	if first.name != "shell" || second.name != "shell" {
		t.Fatalf("tool names = (%q, %q), want shell", first.name, second.name)
	}
	if first.args["cmd"] != "date" || second.args["cmd"] != "pwd" {
		t.Fatalf("tool args = (%v, %v), want distinct args", first.args, second.args)
	}
	if !first.startedAt.Equal(t1) || !second.startedAt.Equal(t2) {
		t.Fatalf("startedAt = (%v, %v), want (%v, %v)", first.startedAt, second.startedAt, t1, t2)
	}
}

func TestAssistantFinalTextConflictClearsStaleTextSegments(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	t1 := time.Unix(7, 0)
	t2 := t1.Add(time.Second)
	t3 := t1.Add(2 * time.Second)
	t4 := t1.Add(3 * time.Second)

	asst.appendTextDelta("draft ", t1)
	asst.upsertToolStart("call-1", "shell", map[string]any{"cmd": "date"}, t2)
	asst.appendTextDelta("old", t3)
	conflict := asst.setFinalText(finalSourceRuntime, "final", t4)

	if !conflict || !asst.finalConflict {
		t.Fatalf("conflict = %v, finalConflict = %v, want both true", conflict, asst.finalConflict)
	}
	if len(asst.segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(asst.segments))
	}
	if asst.segments[0].kind != segmentKindText {
		t.Fatalf("segments[0].kind = %v, want text", asst.segments[0].kind)
	}
	if asst.segments[1].kind != segmentKindTool {
		t.Fatalf("segments[1].kind = %v, want tool", asst.segments[1].kind)
	}
	if asst.segments[2].kind != segmentKindText {
		t.Fatalf("segments[2].kind = %v, want text", asst.segments[2].kind)
	}
	if got := asst.segments[0].text.text; got != "" {
		t.Fatalf("stale text segment = %q, want empty", got)
	}
	if got := asst.segments[2].text.text; got != "final" {
		t.Fatalf("last text segment = %q, want final", got)
	}
	if got := asst.finalText; got != "final" {
		t.Fatalf("finalText = %q, want final", got)
	}
	if got := asst.streamedText(); got != "final" {
		t.Fatalf("streamedText = %q, want final", got)
	}
}

func TestAssistantFinishToolKeepsTerminalStatus(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusDone}
	start := time.Unix(8, 0)
	end := start.Add(time.Second)

	asst.upsertToolStart("call-1", "shell", nil, start)
	asst.status = assistantStatusDone
	asst.finishTool("call-1", "shell", "done", false, end)

	if asst.status != assistantStatusDone {
		t.Fatalf("assistant status = %v, want done", asst.status)
	}
}

func TestAssistantLateMutationsKeepTerminalStatus(t *testing.T) {
	tests := []assistantStatus{
		assistantStatusDone,
		assistantStatusError,
		assistantStatusCancelled,
	}
	for _, status := range tests {
		t.Run("terminal", func(t *testing.T) {
			asst := &assistantBlock{status: status}
			start := time.Unix(9, 0)

			asst.appendReasoningDelta("late thought", start)
			if asst.status != status {
				t.Fatalf("after reasoning status = %v, want %v", asst.status, status)
			}
			asst.appendTextDelta("late text", start)
			if asst.status != status {
				t.Fatalf("after text status = %v, want %v", asst.status, status)
			}
			asst.upsertToolStart("call-1", "shell", nil, start)
			if asst.status != status {
				t.Fatalf("after tool start status = %v, want %v", asst.status, status)
			}
			asst.updateTool("call-1", "shell", "running", start)
			if asst.status != status {
				t.Fatalf("after tool update status = %v, want %v", asst.status, status)
			}
		})
	}
}
