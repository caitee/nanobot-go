package main

import (
	"fmt"
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
	for _, want := range []string{"› hello", "ori", "thinking", "answer", "/status", "ready", "session switched"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected rendered transcript to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "you") {
		t.Fatalf("did not expect user block label in transcript, got:\n%s", out)
	}
	if userIdx, answerIdx := strings.Index(out, "hello"), strings.Index(out, "answer"); userIdx < 0 || answerIdx < 0 || userIdx > answerIdx {
		t.Fatalf("expected user content before assistant text, got:\n%s", out)
	}
}

func TestTranscriptRendererPadsTranscriptContent(t *testing.T) {
	now := time.Unix(100, 0)
	var tr transcript
	tr.appendUserBlock("user-1", "hello", now)
	asst := tr.appendAssistantBlock("asst-1", now.Add(time.Second))
	asst.appendTextDelta("answer", now.Add(2*time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 40, now: now}))
	if !strings.Contains(out, "\n› hello") && !strings.HasPrefix(out, "› hello") {
		t.Fatalf("expected user line to have no global left padding, got:\n%s", out)
	}
	if strings.Contains(out, " › hello") {
		t.Fatalf("expected user line padding to stay minimal, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if got := lipgloss.Width(line); got > 40 {
			t.Fatalf("expected padded line to fit width 40, got width %d for line %q in:\n%s", got, line, out)
		}
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
	if strings.Contains(out, "**bold**") {
		t.Fatalf("expected markdown command output not to render raw markers as plain text, got:\n%s", out)
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

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    40,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("expected line to fit width 40, got width %d for line %q in:\n%s", width, line, out)
		}
	}
}

func TestTranscriptRendererNormalToolShowsCompactResultPreview(t *testing.T) {
	now := time.Unix(122, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.upsertToolStart("call-1", "list_dir", map[string]any{"path": "."}, now)
	asst.finishTool("call-1", "list_dir", "alpha\nbeta", false, now.Add(3*time.Millisecond))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(3 * time.Millisecond),
		viewMode: transcriptViewNormal,
	}))
	for _, want := range []string{"✓ list_dir .", "3ms", "10 chars"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal tool summary to contain %q, got:\n%s", want, out)
		}
	}
	for _, want := range []string{"Result", "alpha"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal tool summary to include compact result preview %q, got:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"│ path", "beta"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal tool summary should hide %q, got:\n%s", hidden, out)
		}
	}
}

func TestTranscriptRendererDetailToolKeepsArgumentsAndResultPreview(t *testing.T) {
	now := time.Unix(123, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.upsertToolStart("call-1", "list_dir", map[string]any{"path": "."}, now)
	asst.finishTool("call-1", "list_dir", "alpha\nbeta", false, now.Add(3*time.Millisecond))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(3 * time.Millisecond),
		viewMode: transcriptViewDetail,
	}))
	for _, want := range []string{"✓ list_dir", "│ path", "Result", "alpha", "beta"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected detail tool block to contain %q, got:\n%s", want, out)
		}
	}
}

func TestTranscriptRendererNormalToolErrorShowsOnePreviewLine(t *testing.T) {
	now := time.Unix(124, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.upsertToolStart("call-1", "read_file", map[string]any{"path": "/tmp/missing"}, now)
	asst.finishTool("call-1", "read_file", "first error line\nsecond error line", true, now.Add(time.Millisecond))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(time.Millisecond),
		viewMode: transcriptViewNormal,
	}))
	for _, want := range []string{"✗ read_file /tmp/missing", "Error", "first error line"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal error summary to contain %q, got:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"second error line", "... 1 more lines"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal error summary should hide %q, got:\n%s", hidden, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Error") && lipgloss.Width(line) > 80 {
			t.Fatalf("normal error preview should fit width 80, got %d for %q in:\n%s", lipgloss.Width(line), line, out)
		}
	}
}

func TestTranscriptRendererNormalRunningToolShowsPartialSize(t *testing.T) {
	now := time.Unix(125, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	tool := asst.upsertToolStart("call-1", "shell", map[string]any{"command": "printf hi"}, now)
	tool.partial = "partial output"

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(1200 * time.Millisecond),
		viewMode: transcriptViewNormal,
	}))
	for _, want := range []string{"● shell", "running", "1.2s", "14 chars"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal running summary to contain %q, got:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"Preview", "partial output"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal running summary should hide %q, got:\n%s", hidden, out)
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

func TestTranscriptRendererUserPlainTextWrapPreservesContent(t *testing.T) {
	now := time.Unix(114, 0)
	var tr transcript
	tr.appendUserBlock("user-1", "alpha SENTINEL omega", now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 8, now: now}))
	assertTranscriptRendererLinesFit(t, out, 8)
	for _, want := range []string{"alpha", "SENTINEL", "omega"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected wrapped user output to preserve %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "...") {
		t.Fatalf("expected wrapped user body not to use truncation ellipses, got:\n%s", out)
	}
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

func TestTranscriptRendererCommandPlainTextWrapPreservesContent(t *testing.T) {
	now := time.Unix(115, 0)
	var tr transcript
	tr.appendCommandBlock("cmd-1", "/wrap", "alpha SENTINEL omega", "", "ready", now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 8, now: now}))
	assertTranscriptRendererLinesFit(t, out, 8)
	for _, want := range []string{"alpha", "SENTINEL", "omega"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected wrapped command output to preserve %q, got:\n%s", want, out)
		}
	}
	body := strings.Join(strings.Split(out, "\n")[1:], "\n")
	if strings.Contains(body, "...") {
		t.Fatalf("expected wrapped command body not to use truncation ellipses, got:\n%s", out)
	}
}

func TestTranscriptRendererPlainTextWrapHandlesCJKWidth(t *testing.T) {
	now := time.Unix(116, 0)
	var tr transcript
	tr.appendUserBlock("user-1", "你好世界你好世界", now)
	tr.appendCommandBlock("cmd-1", "/cjk", "再见世界再见世界", "", "", now.Add(time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 6, now: now}))
	assertTranscriptRendererLinesFit(t, out, 6)
	if strings.Contains(out, "...") {
		t.Fatalf("expected CJK wrapped body not to use truncation ellipses, got:\n%s", out)
	}
}

func TestTranscriptRendererSystemLinesFitNarrowWidth(t *testing.T) {
	now := time.Unix(107, 0)
	var tr transcript
	tr.appendSystemBlock("sys-1", systemLevelWarning, strings.Repeat("system message ", 20), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 28, now: now}))
	assertTranscriptRendererLinesFit(t, out, 28)
}

func TestTranscriptRendererToolTinyWidthsFit(t *testing.T) {
	now := time.Unix(117, 0)
	for _, width := range []int{1, 2, 6, 7, 8, 10} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			var tr transcript
			asst := tr.appendAssistantBlock("asst-1", now)
			tool := asst.upsertToolStart("call-1", "shell", map[string]any{
				"argument": "value",
			}, now)
			tool.partial = "partial preview"
			tool.result = "result preview"
			tool.status = toolStatusDone
			tool.durationMs = 1

			out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
				width:    width,
				now:      now,
				viewMode: transcriptViewDetail,
			}))
			assertTranscriptRendererLinesFit(t, out, width)
		})
	}
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

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
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

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
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

func TestTranscriptRendererNormalReasoningIsHeaderOnly(t *testing.T) {
	now := time.Unix(120, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendReasoningDelta(strings.Join([]string{
		"hidden one",
		"hidden two",
		"tail one",
		"tail two",
		"tail three",
	}, "\n"), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewNormal,
	}))
	if !strings.Contains(out, "thinking · 5 lines summarized") {
		t.Fatalf("expected reasoning header in normal mode, got:\n%s", out)
	}
	for _, hidden := range []string{"hidden one", "hidden two", "tail one", "tail two", "tail three"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal mode should hide reasoning body %q, got:\n%s", hidden, out)
		}
	}
}

func TestTranscriptRendererNormalPreservesReasoningBoundariesAcrossToolSegments(t *testing.T) {
	now := time.Unix(124, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendReasoningDelta("plan docs\nchoose files", now)
	asst.appendToolStart("call-1", "list_dir", map[string]any{"path": "docs"}, now.Add(time.Second), false)
	asst.finishTool("call-1", "list_dir", "ARCHITECTURE.md\nTUI-GUIDE.md", false, now.Add(2*time.Second))
	asst.appendReasoningDelta("read docs", now.Add(3*time.Second))
	asst.appendToolStart("call-2", "read_file", map[string]any{"path": "docs/TUI-GUIDE.md"}, now.Add(4*time.Second), false)
	asst.finishTool("call-2", "read_file", "guide", false, now.Add(5*time.Second))
	asst.appendReasoningDelta("compose final", now.Add(6*time.Second))
	asst.appendTextDelta("docs summary", now.Add(7*time.Second))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewNormal,
	}))
	if got := strings.Count(out, "thinking ·"); got != 3 {
		t.Fatalf("normal mode should preserve reasoning segment boundaries, got %d thinking summaries:\n%s", got, out)
	}
	for _, want := range []string{
		"thinking · 2 lines summarized",
		"thinking · 1 lines summarized",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("normal mode should keep per-segment reasoning summary %q, got:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"plan docs", "choose files", "read docs", "compose final"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal mode should hide reasoning body %q, got:\n%s", hidden, out)
		}
	}
	for _, visible := range []string{"list_dir docs", "ARCHITECTURE.md", "read_file docs/TUI-GUIDE.md", "guide", "docs summary"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("normal mode should keep non-reasoning segment %q, got:\n%s", visible, out)
		}
	}
	assertInOrder(t, out,
		"thinking · 2 lines summarized",
		"list_dir docs",
		"Result: ARCHITECTURE.md",
		"thinking · 1 lines summarized",
		"read_file docs/TUI-GUIDE.md",
		"Result: guide",
		"thinking · 1 lines summarized",
		"docs summary",
	)
}

func assertInOrder(t *testing.T, out string, parts ...string) {
	t.Helper()
	offset := 0
	for _, part := range parts {
		idx := strings.Index(out[offset:], part)
		if idx < 0 {
			t.Fatalf("expected %q after offset %d, got:\n%s", part, offset, out)
		}
		offset += idx + len(part)
	}
}

func TestTranscriptRendererDetailReasoningKeepsTailSummary(t *testing.T) {
	now := time.Unix(121, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendReasoningDelta(strings.Join([]string{
		"hidden one",
		"hidden two",
		"tail one",
		"tail two",
		"tail three",
	}, "\n"), now)
	asst.status = assistantStatusDone

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
	if strings.Contains(out, "hidden one") || strings.Contains(out, "hidden two") {
		t.Fatalf("detail mode should still hide older reasoning lines, got:\n%s", out)
	}
	for _, visible := range []string{"tail one", "tail two", "tail three"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("detail mode should include reasoning tail %q, got:\n%s", visible, out)
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

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
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

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
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
