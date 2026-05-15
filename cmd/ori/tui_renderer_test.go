package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestTranscriptRendererOrdersBlocks(t *testing.T) {
	now := time.Unix(100, 0)
	var tr transcript
	tr.appendUserBlock("user-1", "hello", now)
	asst := tr.appendAssistantBlock("asst-1", now.Add(time.Second))
	asst.appendReasoningDelta("first\nsecond\nthird\nfourth", now.Add(2*time.Second))
	asst.appendTextDelta("answer", now.Add(3*time.Second))
	tr.appendCommandBlock("cmd-1", "/status", "ready", "", "ready", now.Add(4*time.Second))
	tr.appendSystemBlock("sys-1", systemLevelInfo, "session switched", now.Add(5*time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: now}))
	for _, want := range []string{"you", "hello", "ori", "thinking", "answer", "/status", "ready", "session switched"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered transcript to contain %q, got:\n%s", want, out)
		}
	}
	if userIdx, answerIdx := strings.Index(out, "hello"), strings.Index(out, "answer"); userIdx < 0 || answerIdx < 0 || userIdx > answerIdx {
		t.Fatalf("expected user content before assistant text, got:\n%s", out)
	}
}

func TestTranscriptRendererKeepsCommandTextPlain(t *testing.T) {
	now := time.Unix(101, 0)
	var tr transcript
	tr.appendCommandBlock("cmd-1", "/skills", "**not markdown**", "", "ready", now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: now}))
	if !strings.Contains(out, "**not markdown**") {
		t.Fatalf("expected command text to remain plain, got:\n%s", out)
	}
}

func TestTranscriptRendererUsesMarkdownFieldForMarkdownCommands(t *testing.T) {
	now := time.Unix(102, 0)
	var tr transcript
	tr.appendCommandBlock("cmd-1", "/help", "", "**bold**", "ready", now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: now}))
	if !strings.Contains(out, "bold") {
		t.Fatalf("expected markdown command output to include rendered text, got:\n%s", out)
	}
}

func TestTranscriptRendererToolLinesFitNarrowWidth(t *testing.T) {
	now := time.Unix(103, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	tool := asst.upsertToolStart("call-1", "shell", map[string]any{
		"extremely_long_argument_key": strings.Repeat("argument ", 20),
	}, now)
	tool.result = strings.Repeat("result ", 20)
	tool.status = toolStatusDone
	tool.endedAt = now.Add(time.Second)
	tool.durationMs = 1000

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 40, now: now}))
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("expected line to fit width 40, got width %d for line %q in:\n%s", width, line, out)
		}
	}
}

func TestTranscriptRendererFinalConflictUsesFinalTextOnce(t *testing.T) {
	now := time.Unix(104, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendTextDelta("draft ", now)
	asst.upsertToolStart("call-1", "shell", map[string]any{"cmd": "date"}, now.Add(time.Second))
	asst.appendTextDelta("old", now.Add(2*time.Second))
	asst.setFinalText(finalSourceRuntime, "final", now.Add(3*time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: now}))
	if strings.Contains(out, "draft") || strings.Contains(out, "old") {
		t.Fatalf("expected stale conflict text to stay hidden, got:\n%s", out)
	}
	if count := strings.Count(out, "final"); count != 1 {
		t.Fatalf("expected final text once, got %d occurrences in:\n%s", count, out)
	}
}
