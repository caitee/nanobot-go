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
	asst.setFinalText(finalSourceAgentEnd, "first second final", finalAt)

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
	if got := asst.segments[2].text.text; got != "first second final" {
		t.Fatalf("last text segment = %q, want final text", got)
	}
	if got := asst.segments[2].updatedAt; !got.Equal(finalAt) {
		t.Fatalf("last text updatedAt = %v, want %v", got, finalAt)
	}
	if got := asst.finalText; got != "first second final" {
		t.Fatalf("finalText = %q, want final text", got)
	}
}
