package main

import (
	"regexp"
	"strings"
	"testing"
	"time"

	appcore "ori/internal/app"
	"ori/internal/llm"
	"ori/internal/runtime"

	"github.com/charmbracelet/lipgloss"
)

func newTestModel() *interactiveModel {
	return &interactiveModel{active: true}
}

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainView(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
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

func TestViewCacheInvalidatesWhenDisplayedTextChanges(t *testing.T) {
	m := newTestModel()
	m.displayedText = "first response"

	view := plainView(m.View())
	if !strings.Contains(view, "first response") {
		t.Fatalf("expected initial View to show first response; got:\n%s", view)
	}

	m.displayedText = "second response"
	view = plainView(m.View())
	if strings.Contains(view, "first response") || !strings.Contains(view, "second response") {
		t.Fatalf("expected View cache to refresh after displayed text changed; got:\n%s", view)
	}
}

func TestCloseOpenMarkdownCompletesDanglingLinkDestination(t *testing.T) {
	got := closeOpenMarkdown("see [docs](https://example.com")
	if got != "see [docs](https://example.com)" {
		t.Fatalf("expected dangling link destination to be closed, got %q", got)
	}
}

func TestFormatFinalMessageSkipsAlreadyFlushedStreamPrefix(t *testing.T) {
	m := newTestModel()
	m.flushedText = "already printed"

	out := plainView(m.formatFinalMessage("already printed\n\nnew tail", ""))
	if strings.Contains(out, "already printed") {
		t.Fatalf("expected final output to omit already flushed prefix; got:\n%s", out)
	}
	if !strings.Contains(out, "new tail") {
		t.Fatalf("expected final output to include unflushed tail; got:\n%s", out)
	}
}

func TestMaybeFlushStreamWindowRecordsFlushedPrefix(t *testing.T) {
	t.Setenv("COLUMNS", "40")
	t.Setenv("LINES", "20")

	m := newTestModel()
	m.displayedText = strings.Join([]string{
		"already printed",
		"",
		"also printed",
		"",
		"tail line 1",
		"tail line 2",
		"tail line 3",
		"tail line 4",
		"tail line 5",
		"tail line 6",
		"tail line 7",
		"tail line 8",
		"tail line 9",
		"tail line 10",
		"tail line 11",
		"tail line 12",
		"tail line 13",
		"tail line 14",
		"tail line 15",
		"tail line 16",
	}, "\n")

	cmd := m.maybeFlushStreamWindow()
	if cmd == nil {
		t.Fatal("expected stream window flush command")
	}
	if !strings.Contains(m.flushedText, "already printed") || !strings.Contains(m.flushedText, "also printed") {
		t.Fatalf("expected flushedText to record flushed prefix, got %q", m.flushedText)
	}
	if strings.Contains(m.displayedText, "already printed") {
		t.Fatalf("expected displayedText to keep only unflushed tail, got %q", m.displayedText)
	}
}

func TestMaybeFlushStreamWindowSkipsWhenCurrentRoundIsUnflushed(t *testing.T) {
	t.Setenv("COLUMNS", "40")
	t.Setenv("LINES", "20")

	m := newTestModel()
	m.currentRound = &thinkingRound{reasoning: "internal reasoning"}
	m.displayedText = strings.Join([]string{
		"answer prefix",
		"",
		"answer middle",
		"",
		"tail line 1",
		"tail line 2",
		"tail line 3",
		"tail line 4",
		"tail line 5",
		"tail line 6",
		"tail line 7",
		"tail line 8",
		"tail line 9",
		"tail line 10",
		"tail line 11",
		"tail line 12",
		"tail line 13",
		"tail line 14",
		"tail line 15",
		"tail line 16",
	}, "\n")

	if cmd := m.maybeFlushStreamWindow(); cmd != nil {
		t.Fatal("expected no stream-window flush while current round is still unflushed")
	}
	if m.flushedText != "" {
		t.Fatalf("expected no flushed text while current round is unflushed, got %q", m.flushedText)
	}
}

func TestRenderRoundToolDetailLinesFitTerminalWidth(t *testing.T) {
	t.Setenv("COLUMNS", "80")

	m := newTestModel()
	entry := toolCallEntry{
		name:   "web",
		args:   strings.Repeat("argument ", 20),
		status: "done",
		result: strings.Repeat("result ", 20),
	}
	entry.displayArgs.set(entry.args)
	entry.displayResult.set(entry.result)

	out := plainView(m.renderRound(thinkingRound{toolCalls: []toolCallEntry{entry}}, true))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Args:") || strings.Contains(line, "Result:") {
			if width := lipgloss.Width(line); width > 80 {
				t.Fatalf("expected tool detail line to fit terminal width, got width %d for line %q", width, line)
			}
		}
	}
}

func TestHandleRuntimeEvent_TurnStartFlushesPreviousRound(t *testing.T) {
	m := newTestModel()
	var printed strings.Builder
	m.printAboveFn = func(s string) {
		printed.WriteString(s)
		printed.WriteString("\n")
	}

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

	// Second TurnStart should flush the previous round above the TUI.
	cmd := m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	if cmd == nil {
		t.Fatal("expected TurnStart to return a flush command for the previous round")
	}
	cmd()

	out := printed.String()
	if !strings.Contains(out, "read_file") {
		t.Fatalf("expected flushed output to include tool name; got:\n%s", out)
	}
	if !strings.Contains(out, "path") || !strings.Contains(out, "/tmp/demo.md") {
		t.Fatalf("expected flushed output to include structured tool args; got:\n%s", out)
	}
	if !strings.Contains(out, "Result") {
		t.Fatalf("expected flushed output to include tool result; got:\n%s", out)
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
	if !strings.Contains(printed, "path") || !strings.Contains(printed, "/tmp/demo.md") {
		t.Fatalf("expected final printed output to include structured tool args; got:\n%s", printed)
	}
	if !strings.Contains(printed, "Result") {
		t.Fatalf("expected final printed output to include tool result; got:\n%s", printed)
	}
}

func TestAgentEnd_PrintsFinalOutputWithToolCallsFromPreviousTurn(t *testing.T) {
	m := newTestModel()
	var printed strings.Builder
	m.printAboveFn = func(s string) {
		printed.WriteString(s)
		printed.WriteString("\n")
	}

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

	// Second TurnStart must flush the previous round above the TUI.
	flushCmd := m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	if flushCmd == nil {
		t.Fatal("expected TurnStart to return a flush command for the previous round")
	}
	flushCmd()

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

	all := printed.String()
	if !strings.Contains(all, "read_file") {
		t.Fatalf("expected cumulative printed output to include read_file from a prior turn; got:\n%s", all)
	}
	if !strings.Contains(all, "hello world") {
		t.Fatalf("expected cumulative printed output to include assistant answer; got:\n%s", all)
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

func TestHandleRuntimeEvent_ToolEndFallsBackToThinking(t *testing.T) {
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
	if m.status != "running tools" {
		t.Fatalf("expected status %q while tool is running, got %q", "running tools", m.status)
	}

	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: time.Now(),
		Data: runtime.ToolEndData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Result:     []llm.Content{llm.TextContent{Text: "ok"}},
		},
	})
	if m.status != "thinking" {
		t.Fatalf("expected status to fall back to %q after tool end, got %q", "thinking", m.status)
	}

	view := m.View()
	if strings.Contains(view, "running tools") {
		t.Fatalf("expected status line not to show 'running tools' after tool ended; got:\n%s", view)
	}
}

func TestHandleRuntimeEvent_ToolEndKeepsUsingToolsWhileOthersRun(t *testing.T) {
	m := newTestModel()

	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Now(),
		Data:      runtime.ToolStartData{ToolCallID: "call_1", ToolName: "read_file"},
	})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Now(),
		Data:      runtime.ToolStartData{ToolCallID: "call_2", ToolName: "web_search"},
	})

	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: time.Now(),
		Data: runtime.ToolEndData{
			ToolCallID: "call_1",
			ToolName:   "read_file",
			Result:     []llm.Content{llm.TextContent{Text: "ok"}},
		},
	})
	if m.status != "running tools" {
		t.Fatalf("expected status to stay %q while another tool is still running, got %q", "running tools", m.status)
	}
}

func TestRenderReasoningBlockSummarizesAcrossLiveCompletedAndFinal(t *testing.T) {
	reasoning := strings.Join([]string{
		"line 1 hidden",
		"line 2 hidden",
		"line 3 hidden",
		"line 4 visible in live only",
		"line 5 visible",
		"line 6 visible",
		"line 7 visible",
	}, "\n")

	m := newTestModel()
	live := plainView(m.renderRound(thinkingRound{reasoning: reasoning}, true))
	completed := plainView(m.renderCompletedRound(thinkingRound{reasoning: reasoning}))
	final := plainView(formatAssistantMessage([]thinkingRound{{reasoning: reasoning}}, "", reasoning))

	for name, out := range map[string]string{"live": live, "completed": completed, "final": final} {
		if !strings.Contains(out, "thinking · 7 lines summarized") {
			t.Fatalf("%s reasoning should include summary header, got:\n%s", name, out)
		}
		if strings.Contains(out, "line 1 hidden") || strings.Contains(out, "line 2 hidden") {
			t.Fatalf("%s reasoning should hide older lines, got:\n%s", name, out)
		}
	}
	if !strings.Contains(live, "line 4 visible in live only") {
		t.Fatalf("live reasoning should show last five lines, got:\n%s", live)
	}
	if strings.Contains(completed, "line 4 visible in live only") || strings.Contains(final, "line 4 visible in live only") {
		t.Fatalf("completed/final reasoning should show only last three lines, completed:\n%s\nfinal:\n%s", completed, final)
	}
}

func TestRenderToolCallUsesStableMultilineArguments(t *testing.T) {
	m := newTestModel()
	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Now(),
		Data: runtime.ToolStartData{
			ToolCallID: "call_1",
			ToolName:   "shell",
			Args:       map[string]any{"timeout": 5, "command": "go test ./cmd/ori"},
		},
	})

	view := plainView(m.View())
	commandIdx := strings.Index(view, "command")
	timeoutIdx := strings.Index(view, "timeout")
	if commandIdx < 0 || timeoutIdx < 0 {
		t.Fatalf("expected structured argument keys in view, got:\n%s", view)
	}
	if commandIdx > timeoutIdx {
		t.Fatalf("expected argument keys to be sorted, got:\n%s", view)
	}
	if strings.Contains(view, "Args:") {
		t.Fatalf("expected multiline argument block instead of Args line, got:\n%s", view)
	}
}

func TestRenderToolArgumentLinesFitNarrowTerminal(t *testing.T) {
	t.Setenv("COLUMNS", "32")

	m := newTestModel()
	entry := toolCallEntry{
		name:    "shell",
		status:  "running",
		argsMap: map[string]any{"extremely_long_argument_key": strings.Repeat("value ", 20)},
	}

	out := plainView(m.renderRound(thinkingRound{toolCalls: []toolCallEntry{entry}}, true))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "extremely") || strings.Contains(line, "...") {
			if width := lipgloss.Width(line); width > 32 {
				t.Fatalf("expected structured arg line to fit terminal width, got width %d for line %q", width, line)
			}
		}
	}
}

func TestRenderToolResultShowsPreviewAndHiddenLineCount(t *testing.T) {
	m := newTestModel()
	entry := toolCallEntry{
		name:   "read_file",
		status: "done",
		result: strings.Join([]string{
			"line 1",
			"line 2",
			"line 3",
			"line 4",
			"line 5 hidden",
			"line 6 hidden",
		}, "\n"),
	}
	entry.displayResult.set(entry.result)

	out := plainView(m.renderRound(thinkingRound{toolCalls: []toolCallEntry{entry}}, false))
	for _, want := range []string{"line 1", "line 2", "line 3", "line 4", "... 2 more lines"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected result preview to include %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line 5 hidden") || strings.Contains(out, "line 6 hidden") {
		t.Fatalf("expected long result preview to hide tail lines, got:\n%s", out)
	}
}

func TestHandleRuntimeEvent_ToolUpdateRendersRunningPreview(t *testing.T) {
	m := newTestModel()
	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Now(),
		Data:      runtime.ToolStartData{ToolCallID: "call_1", ToolName: "shell", Args: map[string]any{"command": "printf hi"}},
	})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecUpdate,
		Timestamp: time.Now(),
		Data: runtime.ToolUpdateData{
			ToolCallID: "call_1",
			ToolName:   "shell",
			Partial:    []llm.Content{llm.TextContent{Text: "partial output"}},
		},
	})

	view := plainView(m.View())
	if !strings.Contains(view, "partial output") {
		t.Fatalf("expected running tool preview to include partial output, got:\n%s", view)
	}
}

func TestRunningToolDoesNotReuseGlobalSpinnerFrame(t *testing.T) {
	m := newTestModel()
	m.spinnerIdx = 3
	m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	m.handleRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Now(),
		Data:      runtime.ToolStartData{ToolCallID: "call_1", ToolName: "read_file"},
	})

	view := plainView(m.View())
	toolLine := ""
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "read_file") {
			toolLine = line
			break
		}
	}
	if toolLine == "" {
		t.Fatalf("expected tool line in view, got:\n%s", view)
	}
	if strings.Contains(toolLine, spinnerFrames[m.spinnerIdx]) {
		t.Fatalf("expected running tool line not to reuse global spinner frame %q, got %q", spinnerFrames[m.spinnerIdx], toolLine)
	}
	if !strings.Contains(view, spinnerFrames[m.spinnerIdx]+" running tools") {
		t.Fatalf("expected bottom status to keep global spinner, got:\n%s", view)
	}
}
