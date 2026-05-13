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
	if !strings.Contains(out, "Args:") {
		t.Fatalf("expected flushed output to include tool args; got:\n%s", out)
	}
	if !strings.Contains(out, "Result:") {
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
	if !strings.Contains(printed, "Args:") {
		t.Fatalf("expected final printed output to include tool args; got:\n%s", printed)
	}
	if !strings.Contains(printed, "Result:") {
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
	if m.status != "using tools" {
		t.Fatalf("expected status %q while tool is running, got %q", "using tools", m.status)
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
	if strings.Contains(view, "using tools") {
		t.Fatalf("expected status line not to show 'using tools' after tool ended; got:\n%s", view)
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
	if m.status != "using tools" {
		t.Fatalf("expected status to stay %q while another tool is still running, got %q", "using tools", m.status)
	}
}
