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

func TestTranscriptRendererUserLinesFitNarrowWidth(t *testing.T) {
	now := time.Unix(105, 0)
	var tr transcript
	tr.appendUserBlock("user-1", strings.Repeat("hello ", 20), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 24, now: now}))
	assertTranscriptRendererLinesFit(t, out, 24)
}

func TestTranscriptRendererCommandLinesFitNarrowWidth(t *testing.T) {
	now := time.Unix(106, 0)
	var tr transcript
	tr.appendCommandBlock(
		"cmd-1",
		"/status-with-a-very-long-name",
		strings.Repeat("plain ", 20),
		"**"+strings.Repeat("markdown ", 20)+"**",
		"ready-with-a-very-long-status",
		now,
	)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 32, now: now}))
	assertTranscriptRendererLinesFit(t, out, 32)
}

func TestTranscriptRendererSystemLinesFitNarrowWidth(t *testing.T) {
	now := time.Unix(107, 0)
	var tr transcript
	tr.appendSystemBlock("sys-1", systemLevelWarning, strings.Repeat("system message ", 20), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 28, now: now}))
	assertTranscriptRendererLinesFit(t, out, 28)
}

func TestTranscriptRendererAssistantMarkdownAndFinalFallbackFitNarrowWidth(t *testing.T) {
	now := time.Unix(108, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendTextDelta("**"+strings.Repeat("assistant markdown ", 20)+"**", now)
	fallback := tr.appendAssistantBlock("asst-2", now.Add(time.Second))
	fallback.finalText = strings.Repeat("fallback final ", 20)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 34, now: now}))
	assertTranscriptRendererLinesFit(t, out, 34)
}

func TestTranscriptRendererReasoningLinesFitNarrowWidth(t *testing.T) {
	now := time.Unix(109, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendReasoningDelta(strings.Join([]string{
		strings.Repeat("hidden ", 20),
		strings.Repeat("visible one ", 20),
		strings.Repeat("visible two ", 20),
		strings.Repeat("visible three ", 20),
	}, "\n"), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 36, now: now}))
	assertTranscriptRendererLinesFit(t, out, 36)
}

func TestTranscriptRendererAssistantStatusAndOrphanMarker(t *testing.T) {
	now := time.Unix(110, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.status = assistantStatusRunningTools
	asst.appendToolStart("", "shell", nil, now, true)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: now}))
	for _, want := range []string{"running tools", "(orphan)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered transcript to contain %q, got:\n%s", want, out)
		}
	}
}

func TestTranscriptRendererSkipsEmptyUserAndSystemBlocks(t *testing.T) {
	now := time.Unix(111, 0)
	var tr transcript
	tr.appendUserBlock("user-1", "", now)
	tr.appendSystemBlock("sys-1", systemLevelInfo, "", now.Add(time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: now}))
	if strings.TrimSpace(out) != "" {
		t.Fatalf("expected empty user/system blocks to be omitted, got:\n%s", out)
	}
}

func TestTranscriptRendererReasoningUsesLiveOnlyForActiveAssistant(t *testing.T) {
	now := time.Unix(112, 0)
	var tr transcript
	completed := tr.appendAssistantBlock("asst-completed", now)
	completed.appendReasoningDelta(strings.Join([]string{
		"ch1",
		"ch2",
		"ch3",
		"cv4",
		"cv5",
		"cv6",
	}, "\n"), now)
	completed.status = assistantStatusDone

	active := tr.appendAssistantBlock("asst-active", now.Add(time.Second))
	active.appendReasoningDelta(strings.Join([]string{
		"ah1",
		"av2",
		"av3",
		"av4",
		"av5",
		"av6",
	}, "\n"), now.Add(time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 100, now: now}))
	for _, hidden := range []string{"ch1", "ch2", "ch3", "ah1"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("expected reasoning output to hide %q, got:\n%s", hidden, out)
		}
	}
	for _, visible := range []string{"cv4", "cv5", "cv6", "av2", "av3", "av4", "av5", "av6"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("expected reasoning output to include %q, got:\n%s", visible, out)
		}
	}
}

func TestTranscriptRendererTerminalActiveAssistantUsesCompletedReasoning(t *testing.T) {
	now := time.Unix(113, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-done", now)
	asst.appendReasoningDelta(strings.Join([]string{
		"dh1",
		"dh2",
		"dh3",
		"dv4",
		"dv5",
		"dv6",
	}, "\n"), now)
	asst.status = assistantStatusDone
	tr.activeAssistantID = asst.id

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 100, now: now}))
	for _, hidden := range []string{"dh1", "dh2", "dh3"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("expected terminal active assistant to hide completed reasoning line %q, got:\n%s", hidden, out)
		}
	}
	for _, visible := range []string{"dv4", "dv5", "dv6"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("expected terminal active assistant to include completed reasoning line %q, got:\n%s", visible, out)
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

func assertTranscriptRendererLinesFit(t *testing.T, out string, width int) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("expected line to fit width %d, got width %d for line %q in:\n%s", width, got, line, out)
		}
	}
}
