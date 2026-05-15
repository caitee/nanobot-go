package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	appcore "ori/internal/app"
	"ori/internal/bus"
	"ori/internal/config"
	"ori/internal/llm"
	"ori/internal/runtime"
	"ori/internal/session"
	"ori/internal/skills"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func newTestModel() *interactiveModel {
	ti := textinput.New()
	ti.Focus()
	ti.Prompt = "> "
	return &interactiveModel{active: true, textInput: ti}
}

func newCompletionTestDispatcher() *appcore.Dispatcher {
	d := appcore.NewDispatcher(appcore.DispatcherOptions{})
	for _, name := range []string{
		"skill:alpha",
		"skill:bravo",
		"skill:charlie",
		"skill:delta",
		"skill:echo",
		"skill:foxtrot",
		"skill:golf",
		"skill:hotel",
	} {
		d.RegisterSlashCommand(appcore.Command{
			Name:        name,
			Description: "Test completion",
			Scope:       appcore.CommandScopePrompt,
		})
	}
	return d
}

var ansiEscapeRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plainView(s string) string {
	return ansiEscapeRE.ReplaceAllString(s, "")
}

func TestSubmitPromptAppendsTranscriptBlocksWithoutPrintAbove(t *testing.T) {
	calledPrintAbove := false
	m := &interactiveModel{
		dispatcher:   appcore.NewDispatcher(appcore.DispatcherOptions{Bus: bus.New(1)}),
		printAboveFn: func(string) { calledPrintAbove = true },
		renderer:     transcriptRenderer{},
		focus:        focusInput,
	}
	m.initTranscriptViewport(80, 10)

	cmd := m.submitPrompt("hello", "hello")
	if cmd == nil {
		t.Fatal("expected submitPrompt to return spinner tick")
	}
	if _, ok := cmd().(spinnerTickMsg); !ok {
		t.Fatalf("expected submitPrompt to return spinner tick only")
	}
	if calledPrintAbove {
		t.Fatalf("prompt path printed above the TUI")
	}
	if len(m.transcript.blocks) != 2 {
		t.Fatalf("blocks = %d, want user + assistant", len(m.transcript.blocks))
	}
	if m.transcript.blocks[0].kind != blockKindUser || m.transcript.blocks[1].kind != blockKindAssistant {
		t.Fatalf("unexpected blocks: %+v", m.transcript.blocks)
	}
	if out := plainView(m.transcriptViewportText); !strings.Contains(out, "hello") {
		t.Fatalf("prompt was not rendered into transcript viewport:\n%s", out)
	}
}

func TestHandleRuntimeEventUsesTranscript(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	m.beginPromptForTranscript("hello")

	cmd := m.handleRuntimeEvent(runtime.Event{
		Kind: runtime.EventMessageUpdate,
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventTextDelta,
			Delta: "answer",
		}},
	})

	if cmd != nil {
		t.Fatalf("runtime text delta returned print command")
	}
	asst := m.transcript.activeAssistant()
	if asst == nil || len(asst.segments) != 1 || asst.segments[0].text.text != "answer" {
		t.Fatalf("text delta not captured in transcript: %+v", asst)
	}
	if out := plainView(m.transcriptViewportText); !strings.Contains(out, "answer") {
		t.Fatalf("runtime text delta was not rendered into transcript viewport:\n%s", out)
	}
}

func TestResponseMsgFinalizesTranscriptWithoutPrintAbove(t *testing.T) {
	calledPrintAbove := false
	m := &interactiveModel{
		printAboveFn: func(string) { calledPrintAbove = true },
		renderer:     transcriptRenderer{},
		focus:        focusInput,
	}
	m.initTranscriptViewport(80, 10)
	m.beginPromptForTranscript("hello")

	_, cmd := m.Update(responseMsg{content: "final", reasoning: "why", agentEventFinal: true, fallback: true})
	if cmd != nil {
		cmd()
	}

	if calledPrintAbove {
		t.Fatalf("response finalization printed above the TUI")
	}
	asst := m.transcript.activeAssistant()
	if asst == nil || asst.status != assistantStatusDone || asst.finalSource != finalSourceFallback {
		t.Fatalf("assistant not finalized from outbound fallback: %+v", asst)
	}
	if out := plainView(m.transcriptViewportText); !strings.Contains(out, "final") || !strings.Contains(out, "why") {
		t.Fatalf("final response was not rendered into transcript viewport:\n%s", out)
	}
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

func TestViewCacheInvalidatesWhenTranscriptViewportChanges(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(80, 10)
	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "first response", time.Unix(1, 0))
	m.refreshTranscriptViewport()

	view := plainView(m.View())
	if !strings.Contains(view, "first response") {
		t.Fatalf("expected initial View to show first response; got:\n%s", view)
	}

	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "second response", time.Unix(2, 0))
	m.refreshTranscriptViewport()
	view = plainView(m.View())
	if !strings.Contains(view, "second response") {
		t.Fatalf("expected View cache to refresh after transcript viewport changed; got:\n%s", view)
	}
}

func TestRefreshTranscriptViewportFollowsTailAtBottom(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 5)
	for i := 0; i < 12; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}

	m.refreshTranscriptViewport()

	if !m.viewport.AtBottom() {
		t.Fatalf("expected viewport to follow tail")
	}
	if !strings.Contains(plainView(m.viewport.View()), "line 11") {
		t.Fatalf("viewport did not include latest line:\n%s", plainView(m.viewport.View()))
	}
}

func TestRefreshTranscriptViewportPreservesScrollWhenAwayFromBottom(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 5)
	for i := 0; i < 12; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}
	m.refreshTranscriptViewport()
	m.viewport.GotoTop()
	before := m.viewport.YOffset

	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "new output", time.Unix(20, 0))
	m.refreshTranscriptViewport()

	if m.viewport.YOffset != before {
		t.Fatalf("viewport YOffset changed from %d to %d", before, m.viewport.YOffset)
	}
	if !m.hasNewTranscriptOutput {
		t.Fatalf("expected new output indicator when user is away from bottom")
	}
}

func TestViewportKeyRoutingDuringWaiting(t *testing.T) {
	m := &interactiveModel{waiting: true, focus: focusInput}
	m.initTranscriptViewport(40, 5)
	for i := 0; i < 20; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}
	m.refreshTranscriptViewport()
	before := m.viewport.YOffset
	if before == 0 {
		t.Fatalf("expected viewport to have scrollback")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})

	if m.focus != focusTranscript {
		t.Fatalf("expected PageUp to focus transcript while waiting, got %v", m.focus)
	}
	if m.viewport.YOffset >= before {
		t.Fatalf("expected PageUp to update viewport offset below %d, got %d", before, m.viewport.YOffset)
	}
}

func TestTranscriptViewportNewOutputMarkerClearsAtBottom(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 5)
	for i := 0; i < 16; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}
	m.refreshTranscriptViewport()
	m.viewport.GotoTop()
	m.focus = focusTranscript

	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "new output", time.Unix(20, 0))
	m.refreshTranscriptViewport()

	if out := plainView(m.View()); !strings.Contains(out, "new transcript output below") {
		t.Fatalf("expected new transcript marker, got:\n%s", out)
	}

	for i := 0; i < 20 && !m.viewport.AtBottom(); i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	}

	if !m.viewport.AtBottom() {
		t.Fatalf("expected viewport to reach bottom, offset=%d", m.viewport.YOffset)
	}
	if out := plainView(m.View()); strings.Contains(out, "new transcript output below") {
		t.Fatalf("expected new transcript marker to clear at bottom, got:\n%s", out)
	}
}

func TestTranscriptViewportResizePreservesTailFollow(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 10)
	for i := 0; i < 24; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}
	m.refreshTranscriptViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("expected initial viewport at bottom")
	}

	m.resizeTranscriptViewport(40, 5)

	if !m.viewport.AtBottom() {
		t.Fatalf("expected resize to preserve bottom-follow, offset=%d", m.viewport.YOffset)
	}
	if m.hasNewTranscriptOutput {
		t.Fatal("resize should not mark existing transcript content as new output")
	}
	if out := plainView(m.viewport.View()); !strings.Contains(out, "line 23") {
		t.Fatalf("expected resized viewport to keep latest line visible, got:\n%s", out)
	}
}

func TestTranscriptViewportWidthResizeReflowsWithoutNewOutput(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(24, 8)
	m.transcript.appendUserBlock(m.nextBlockID("user"), "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda", time.Unix(1, 0))
	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "latest tail", time.Unix(2, 0))
	m.refreshTranscriptViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("expected initial viewport at bottom")
	}
	narrowContent := plainView(m.transcriptViewportText)
	narrowView := plainView(m.viewport.View())

	m.resizeTranscriptViewport(70, 8)

	wideContent := plainView(m.transcriptViewportText)
	wideView := plainView(m.viewport.View())
	if wideContent == narrowContent {
		t.Fatalf("expected width resize to reflow transcript content; narrow:\n%s\nwide:\n%s", narrowContent, wideContent)
	}
	if wideView == narrowView {
		t.Fatalf("expected visible viewport to change after width resize; narrow:\n%s\nwide:\n%s", narrowView, wideView)
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("expected width resize to preserve bottom-follow, offset=%d", m.viewport.YOffset)
	}
	if !strings.Contains(wideView, "latest tail") {
		t.Fatalf("expected latest line to remain visible after width resize, got:\n%s", wideView)
	}
	if m.hasNewTranscriptOutput {
		t.Fatal("width reflow should not mark transcript output as new")
	}
}

func TestTranscriptViewportViewDoesNotRefreshContent(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 5)
	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "unrefreshed", time.Unix(1, 0))

	if out := plainView(m.View()); strings.Contains(out, "unrefreshed") {
		t.Fatalf("View should use existing viewport content instead of refreshing transcript, got:\n%s", out)
	}

	m.refreshTranscriptViewport()
	if out := plainView(m.View()); !strings.Contains(out, "unrefreshed") {
		t.Fatalf("explicit refresh should make transcript content visible, got:\n%s", out)
	}
}

func TestTranscriptViewportEscReturnsToInputFocus(t *testing.T) {
	m := &interactiveModel{focus: focusTranscript}
	m.initTranscriptViewport(40, 5)

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.focus != focusInput {
		t.Fatalf("expected Esc to return to input focus, got %v", m.focus)
	}
}

func TestTranscriptViewportQuitKeysDuringWaiting(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD} {
		t.Run(key.String(), func(t *testing.T) {
			m := &interactiveModel{focus: focusTranscript, waiting: true, done: make(chan struct{})}

			_, cmd := m.Update(tea.KeyMsg{Type: key})

			if !m.quitting {
				t.Fatalf("expected %s to quit while transcript-focused and waiting", key)
			}
			if cmd == nil {
				t.Fatalf("expected %s to return quit command", key)
			}
		})
	}
}

func TestManagementPanelKeysDuringWaiting(t *testing.T) {
	m := newSessionPanelTestModel(t)
	m.openManagementPanel(appcore.UIRequestSessions)
	m.waiting = true
	if got := len(m.managementPanelRows()); got < 2 {
		t.Fatalf("expected multiple panel rows, got %d", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})

	if m.panel == nil {
		t.Fatal("expected panel to stay open after Down")
	}
	if m.panel.selected != 1 {
		t.Fatalf("expected Down to move panel selection while waiting, got %d", m.panel.selected)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.panel != nil {
		t.Fatalf("expected Esc to close panel while waiting, got %+v", m.panel)
	}
	if m.focus != focusInput {
		t.Fatalf("expected Esc to return focus to input, got %v", m.focus)
	}
}

func TestManagementPanelEditEscKeepsOverlayFocus(t *testing.T) {
	m := &interactiveModel{
		focus: focusOverlay,
		panel: &managementPanel{
			kind:        appcore.UIRequestConfig,
			configDraft: map[string]string{},
			editingKey:  "model",
			editValue:   "draft",
		},
	}

	handled, cmd := m.handleManagementPanelKey(tea.KeyMsg{Type: tea.KeyEsc})

	if !handled {
		t.Fatal("expected edit Esc to be handled by panel")
	}
	if cmd != nil {
		t.Fatal("expected edit Esc to avoid command")
	}
	if m.panel == nil {
		t.Fatal("expected edit Esc to keep panel open")
	}
	if m.panel.editingKey != "" || m.panel.editValue != "" {
		t.Fatalf("expected edit Esc to clear edit state, got key=%q value=%q", m.panel.editingKey, m.panel.editValue)
	}
	if m.focus != focusOverlay {
		t.Fatalf("expected edit Esc to keep overlay focus, got %v", m.focus)
	}
}

func TestTranscriptRendererToolDetailLinesFitTerminalWidth(t *testing.T) {
	t.Setenv("COLUMNS", "80")

	var tr transcript
	asst := tr.appendAssistantBlock("assistant-1", time.Unix(1, 0))
	asst.upsertToolStart("call-1", "web", map[string]any{"query": strings.Repeat("argument ", 20)}, time.Unix(1, 0))
	asst.finishTool("call-1", "web", strings.Repeat("result ", 20), false, time.Unix(2, 0))

	out := plainView((transcriptRenderer{}).renderTranscript(tr, renderContext{width: 80}))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "query") || strings.Contains(line, "Result") {
			if width := lipgloss.Width(line); width > 80 {
				t.Fatalf("expected tool detail line to fit terminal width, got width %d for line %q", width, line)
			}
		}
	}
}

func TestHandleRuntimeEvent_TurnStartKeepsPreviousRoundInTranscript(t *testing.T) {
	m := newTestModel()
	calledPrintAbove := false
	m.printAboveFn = func(string) { calledPrintAbove = true }

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

	// Second TurnStart should keep the previous tool segment in transcript,
	// not flush it above the TUI.
	cmd := m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	if cmd != nil {
		t.Fatal("expected TurnStart to avoid print commands after transcript migration")
	}
	if calledPrintAbove {
		t.Fatal("TurnStart printed above the TUI")
	}
	out := plainView(m.renderer.renderTranscript(m.transcript, renderContext{width: 80}))
	if !strings.Contains(out, "read_file") {
		t.Fatalf("expected transcript to include tool name; got:\n%s", out)
	}
	if !strings.Contains(out, "path") || !strings.Contains(out, "/tmp/demo.md") {
		t.Fatalf("expected transcript to include structured tool args; got:\n%s", out)
	}
	if !strings.Contains(out, "Result") {
		t.Fatalf("expected transcript to include tool result; got:\n%s", out)
	}
}

func TestAgentEnd_FinalizesTranscriptWithToolCallsFromSameTurn(t *testing.T) {
	m := newTestModel()
	calledPrintAbove := false
	m.printAboveFn = func(string) { calledPrintAbove = true }

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
	if cmd != nil {
		t.Fatal("expected agent end to avoid print commands after transcript migration")
	}
	if calledPrintAbove {
		t.Fatal("agent end printed above the TUI")
	}
	asst := m.transcript.activeAssistant()
	if asst == nil || asst.status != assistantStatusDone {
		t.Fatalf("assistant not finalized: %+v", asst)
	}
	out := plainView(m.renderer.renderTranscript(m.transcript, renderContext{width: 80}))
	if !strings.Contains(out, "read_file") {
		t.Fatalf("expected final transcript to include tool name; got:\n%s", out)
	}
	if !strings.Contains(out, "path") || !strings.Contains(out, "/tmp/demo.md") {
		t.Fatalf("expected final transcript to include structured tool args; got:\n%s", out)
	}
	if !strings.Contains(out, "Result") || !strings.Contains(out, "hello world") {
		t.Fatalf("expected final transcript to include tool result and final text; got:\n%s", out)
	}
}

func TestInteractiveModelClearCommandResetsVisibleState(t *testing.T) {
	m := newTestModel()
	m.waiting = true
	m.status = "thinking"

	m.applyClearCommandResult()

	if m.active || m.waiting {
		t.Fatalf("expected clear command to reset active state, got active=%v waiting=%v", m.active, m.waiting)
	}
	if m.status != "ready" {
		t.Fatalf("expected ready status, got %q", m.status)
	}
}

func TestApplySlashCommandResultAppendsCommandBlock(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)

	cmd := m.applySlashCommandResult("/status", &appcore.CommandResult{Text: "ready", Status: "ready"})
	if cmd != nil {
		t.Fatalf("plain command result returned print command")
	}
	if len(m.transcript.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.transcript.blocks))
	}
	got := m.transcript.blocks[0].command
	if got == nil || got.command != "/status" || got.text != "ready" || got.status != "ready" {
		t.Fatalf("command block not appended: %+v", got)
	}
}

func TestApplySlashCommandResultAppendsMarkdownCommandBlock(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)

	cmd := m.applySlashCommandResult("/help", &appcore.CommandResult{Markdown: "**help**", Status: "ready"})
	if cmd != nil {
		t.Fatalf("markdown command result returned print command")
	}
	if len(m.transcript.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.transcript.blocks))
	}
	got := m.transcript.blocks[0].command
	if got == nil || got.command != "/help" || got.markdown != "**help**" || got.status != "ready" {
		t.Fatalf("markdown command block not appended: %+v", got)
	}
}

func TestApplySlashCommandResultOpensOverlayAndRecordsCommand(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)

	cmd := m.applySlashCommandResult("/mcp", &appcore.CommandResult{
		Text:      "MCP status",
		UIRequest: appcore.UIRequestMCP,
	})
	if cmd != nil {
		t.Fatalf("UI command returned print command")
	}
	if m.panel == nil || m.panel.kind != appcore.UIRequestMCP {
		t.Fatalf("panel not opened: %+v", m.panel)
	}
	if m.focus != focusOverlay {
		t.Fatalf("focus = %v, want overlay", m.focus)
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].command == nil {
		t.Fatalf("command transcript block missing: %+v", m.transcript.blocks)
	}
}

func TestClearCommandClearsTranscriptAndAddsSystemBlock(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	m.transcript.appendUserBlock("u1", "old", time.Unix(1, 0))
	m.active = true
	m.waiting = true
	m.responseReceived = true
	m.spinnerIdx = 3
	m.status = "thinking"

	cmd := m.applySlashCommandResult("/clear", &appcore.CommandResult{
		Text:          "New session started.",
		Status:        "ready",
		ResetSession:  true,
		ClearViewport: true,
	})
	if cmd != nil {
		t.Fatalf("clear returned terminal clear command")
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].system == nil {
		t.Fatalf("clear did not replace transcript with system block: %+v", m.transcript.blocks)
	}
	if got := m.transcript.blocks[0].system.message; got != "New session started." {
		t.Fatalf("system message = %q, want New session started.", got)
	}
	if m.active || m.waiting || m.responseReceived {
		t.Fatalf("clear did not reset active state: active=%v waiting=%v responseReceived=%v",
			m.active, m.waiting, m.responseReceived)
	}
	if m.spinnerIdx != 0 {
		t.Fatalf("spinnerIdx = %d, want 0", m.spinnerIdx)
	}
	if m.status != "ready" {
		t.Fatalf("status = %q, want ready", m.status)
	}
}

func TestSlashCommandCompletionAcceptsFirstMatch(t *testing.T) {
	m := newTestModel()
	m.textInput.SetValue("/sk")

	if !m.acceptSlashCommandCompletion() {
		t.Fatal("expected slash command completion to be accepted")
	}
	if got := m.textInput.Value(); got != "/skills " {
		t.Fatalf("expected /skills completion, got %q", got)
	}
}

func TestManagementPanelOpensFromUIRequest(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	result := &appcore.CommandResult{UIRequest: appcore.UIRequestMCP, Text: "fallback"}

	if cmd := m.applySlashCommandResult("/mcp", result); cmd != nil {
		t.Fatalf("expected panel open to avoid print command")
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].command == nil {
		t.Fatalf("expected command block in transcript: %+v", m.transcript.blocks)
	}
	if m.panel == nil || m.panel.kind != appcore.UIRequestMCP {
		t.Fatalf("expected MCP panel, got %+v", m.panel)
	}
	out := plainView(m.View())
	if !strings.Contains(out, "MCP servers") {
		t.Fatalf("expected MCP management panel, got:\n%s", out)
	}
	if strings.Count(out, "fallback") != 1 {
		t.Fatalf("expected fallback only from transcript command block, got:\n%s", out)
	}
}

func TestSessionsPanelRendersSessionRows(t *testing.T) {
	m := newSessionPanelTestModel(t)
	m.openManagementPanel(appcore.UIRequestSessions)

	out := plainView(m.renderManagementPanel())

	if !strings.Contains(out, "Sessions") {
		t.Fatalf("expected sessions panel title, got:\n%s", out)
	}
	if !strings.Contains(out, "cli:target") || !strings.Contains(out, "target prompt") {
		t.Fatalf("expected target session row, got:\n%s", out)
	}
	if !strings.Contains(out, "current") {
		t.Fatalf("expected current session marker, got:\n%s", out)
	}
}

func TestTranscriptFromSessionMessagesBuildsBlocks(t *testing.T) {
	messages := []appcore.SessionMessageView{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Reasoning: "thinking", ToolCalls: []appcore.SessionToolCallView{
			{ID: "tool-1", Name: "shell", ArgumentsMap: map[string]any{"cmd": "date"}},
		}},
		{Role: "tool", ToolCallID: "tool-1", Name: "shell", Content: "tool result"},
		{Role: "assistant", Content: "answer"},
	}

	tr := transcriptFromSessionMessages(messages, time.Unix(1, 0))

	if len(tr.blocks) != 2 {
		t.Fatalf("blocks = %d, want user + assistant", len(tr.blocks))
	}
	if tr.activeAssistantID != "" {
		t.Fatalf("activeAssistantID = %q, want empty", tr.activeAssistantID)
	}
	asst := tr.blocks[1].assistant
	if asst == nil || asst.status != assistantStatusDone {
		t.Fatalf("assistant block missing or unfinished: %+v", tr.blocks[1])
	}
	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80}))
	for _, want := range []string{"hello", "thinking", "answer", "shell", "tool result"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session transcript missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "✦ ori"); got != 1 {
		t.Fatalf("assistant headers = %d, want 1:\n%s", got, out)
	}
}

func TestTranscriptFromSessionMessagesMatchesDelayedToolResultByID(t *testing.T) {
	messages := []appcore.SessionMessageView{
		{Role: "assistant", ToolCalls: []appcore.SessionToolCallView{
			{ID: "tool-1", Name: "shell", ArgumentsMap: map[string]any{"cmd": "date"}},
		}},
		{Role: "assistant", Content: "answer before tool result"},
		{Role: "tool", ToolCallID: "tool-1", Name: "shell", Content: "delayed result"},
	}

	tr := transcriptFromSessionMessages(messages, time.Unix(1, 0))

	if tr.activeAssistantID != "" {
		t.Fatalf("activeAssistantID = %q, want empty", tr.activeAssistantID)
	}
	if len(tr.blocks) != 1 {
		t.Fatalf("blocks = %d, want one assistant block", len(tr.blocks))
	}
	asst := tr.blocks[0].assistant
	if asst == nil {
		t.Fatalf("assistant block missing: %+v", tr.blocks[0])
	}
	tool := asst.findTool("tool-1", "")
	if tool == nil || tool.result != "delayed result" || tool.orphan {
		t.Fatalf("tool result was not matched by ID: %+v", tool)
	}
	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80}))
	for _, want := range []string{"shell", "answer before tool result", "delayed result"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session transcript missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(orphan)") {
		t.Fatalf("matched tool rendered as orphan:\n%s", out)
	}
	if got := strings.Count(out, "✦ ori"); got != 1 {
		t.Fatalf("assistant headers = %d, want 1:\n%s", got, out)
	}
}

func TestResumeSelectedSessionLoadsTranscript(t *testing.T) {
	m := newSessionPanelTestModel(t)
	m.initTranscriptViewport(80, 10)
	m.openManagementPanel(appcore.UIRequestSessions)
	for i, item := range m.managementSessions() {
		if item.Key == "cli:target" {
			m.panel.selected = i
			break
		}
	}

	cmd := m.resumeSelectedSession()

	if cmd != nil {
		t.Fatalf("resume returned print command")
	}
	if len(m.transcript.blocks) == 0 {
		t.Fatalf("resume did not load transcript blocks")
	}
	if m.panel != nil {
		t.Fatalf("expected panel to close after resume")
	}
	if m.focus != focusInput {
		t.Fatalf("focus = %v, want input", m.focus)
	}
	out := plainView(m.transcriptViewportText)
	for _, want := range []string{"target prompt", "target answer", "resumed cli:target"} {
		if !strings.Contains(out, want) {
			t.Fatalf("resume viewport missing %q:\n%s", want, out)
		}
	}
}

func TestResumeSelectedSessionSwitchesContextAndClearsVisibleState(t *testing.T) {
	m := newSessionPanelTestModel(t)
	m.initTranscriptViewport(80, 10)
	m.openManagementPanel(appcore.UIRequestSessions)
	for i, item := range m.managementSessions() {
		if item.Key == "cli:target" {
			m.panel.selected = i
			break
		}
	}
	m.active = true
	m.waiting = true
	m.responseReceived = true
	oldUnsubCalled := false
	m.unsubRuntime = func() { oldUnsubCalled = true }

	cmd := m.resumeSelectedSession()

	if m.sessionKey != "cli:target" {
		t.Fatalf("sessionKey = %q; want cli:target", m.sessionKey)
	}
	if m.chatID != "target" {
		t.Fatalf("chatID = %q; want target", m.chatID)
	}
	if !oldUnsubCalled {
		t.Fatal("expected old runtime subscription to be released")
	}
	if m.unsubRuntime == nil {
		t.Fatal("expected runtime events to be resubscribed for resumed session")
	}
	if m.panel != nil {
		t.Fatalf("expected panel to close after resume")
	}
	if m.focus != focusInput {
		t.Fatalf("expected focus input after resume, got %v", m.focus)
	}
	if m.active || m.waiting || m.responseReceived {
		t.Fatalf("expected resume to clear active state, got active=%v waiting=%v responseReceived=%v",
			m.active, m.waiting, m.responseReceived)
	}
	if m.status != "ready" {
		t.Fatalf("status = %q, want ready", m.status)
	}
	if cmd != nil {
		t.Fatal("expected resume to return nil after loading transcript")
	}
	out := plainView(m.transcriptViewportText)
	for _, want := range []string{"target prompt", "target answer", "resumed cli:target"} {
		if !strings.Contains(out, want) {
			t.Fatalf("resume transcript missing %q:\n%s", want, out)
		}
	}
}

func TestResumeSelectedSessionThenNewPromptUsesUniqueBlockIDs(t *testing.T) {
	m := newSessionPanelTestModel(t)
	m.initTranscriptViewport(80, 10)
	m.openManagementPanel(appcore.UIRequestSessions)
	for i, item := range m.managementSessions() {
		if item.Key == "cli:target" {
			m.panel.selected = i
			break
		}
	}

	if cmd := m.resumeSelectedSession(); cmd != nil {
		t.Fatalf("resume returned print command")
	}
	m.beginPromptForTranscript("new prompt")

	seen := map[string]bool{}
	for _, block := range m.transcript.blocks {
		if block.id == "" {
			t.Fatalf("block with empty id: %+v", block)
		}
		if seen[block.id] {
			t.Fatalf("duplicate transcript block id %q in blocks: %+v", block.id, m.transcript.blocks)
		}
		seen[block.id] = true
	}
}

func newSessionPanelTestModel(t *testing.T) *interactiveModel {
	t.Helper()
	store, err := session.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSessionStore: %v", err)
	}
	for _, sess := range []*session.Session{
		{
			Key:       "cli:current",
			CreatedAt: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 5, 15, 8, 1, 0, 0, time.UTC),
			Metadata:  map[string]any{},
			Messages: []session.Message{
				{Role: "user", Content: "current prompt"},
			},
		},
		{
			Key:       "cli:target",
			CreatedAt: time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 5, 15, 9, 2, 0, 0, time.UTC),
			Metadata:  map[string]any{},
			Messages: []session.Message{
				{Role: "user", Content: "target prompt"},
				{Role: "assistant", Content: "target answer"},
			},
		},
	} {
		if err := store.Save(sess); err != nil {
			t.Fatalf("Save %s: %v", sess.Key, err)
		}
	}
	mgmt := appcore.NewManagementService(appcore.ManagementOptions{SessionStore: store})
	m := newTestModel()
	m.sessionKey = "cli:current"
	m.chatID = "current"
	m.dispatcher = appcore.NewDispatcher(appcore.DispatcherOptions{
		SessionStore: store,
		Management:   mgmt,
	})
	return m
}

func TestManagementPanelKeepsDisabledStatusColorWhenSelected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	skillsDir := filepath.Join(tmp, "skills")
	writeTUITestSkill(t, filepath.Join(skillsDir, "demo", "SKILL.md"), `---
name: demo
description: "Demo skill"
---

# Demo
`)
	loader := skills.NewSkillLoader(skillsDir, filepath.Join(tmp, "no-builtins"))
	loader.SetDisabled([]string{"demo"})
	mgmt := appcore.NewManagementService(appcore.ManagementOptions{
		Config:      &config.Config{Skills: config.SkillsConfig{Disabled: []string{"demo"}}},
		SkillLoader: loader,
	})
	m := newTestModel()
	m.dispatcher = appcore.NewDispatcher(appcore.DispatcherOptions{Management: mgmt})
	m.openManagementPanel(appcore.UIRequestSkills)

	out := m.renderManagementPanel()
	wantDisabled := managementDisabledStyle.Render("disabled")
	if !strings.Contains(out, wantDisabled) {
		t.Fatalf("expected selected row to preserve disabled status color %q, got:\n%s", wantDisabled, out)
	}
	if got, want := managementEnabledStyle.GetForeground(), managementDisabledStyle.GetForeground(); got == want {
		t.Fatalf("enabled and disabled styles should use different foregrounds, both got %v", got)
	}
}

func writeTUITestSkill(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestHandleEnterCompletesPartialSlashCommandWithoutClearingInput(t *testing.T) {
	m := newTestModel()
	m.textInput.SetValue("/sk")

	_, cmd := m.handleEnter()
	if cmd != nil {
		t.Fatal("expected completion to avoid dispatch command")
	}
	if got := m.textInput.Value(); got != "/skills " {
		t.Fatalf("expected partial slash command to complete, got %q", got)
	}
}

func TestRenderSlashCommandSuggestionsShowsCountAndFirstPage(t *testing.T) {
	m := newTestModel()
	m.dispatcher = newCompletionTestDispatcher()
	m.textInput.SetValue("/skill:")

	out := plainView(m.renderSlashCommandSuggestions())
	if !strings.Contains(out, "↑/↓") || strings.Contains(out, "PgUp/PgDn") {
		t.Fatalf("expected arrow-key pagination hint, got:\n%s", out)
	}
	if !strings.Contains(out, "1-6 of 8") {
		t.Fatalf("expected completion count for first page, got:\n%s", out)
	}
	if !strings.Contains(out, "/skill:alpha") || !strings.Contains(out, "/skill:foxtrot") {
		t.Fatalf("expected first page suggestions, got:\n%s", out)
	}
	if strings.Contains(out, "/skill:golf") {
		t.Fatalf("did not expect second page suggestion on first page, got:\n%s", out)
	}
}

func TestRenderSlashCommandSuggestionsHighlightsSelectedRowBrightly(t *testing.T) {
	if got, want := slashCommandSelectedStyle.GetForeground(), lipgloss.Color("86"); got != want {
		t.Fatalf("expected selected suggestion foreground %v, got %v", want, got)
	}
	if !slashCommandSelectedStyle.GetBold() {
		t.Fatal("expected selected suggestion to be bold")
	}
}

func TestSlashCommandSuggestionArrowKeysMoveSelectionByRow(t *testing.T) {
	m := newTestModel()
	m.dispatcher = newCompletionTestDispatcher()
	m.textInput.SetValue("/skill:")

	m.Update(tea.KeyMsg{Type: tea.KeyDown})

	if !m.acceptSlashCommandCompletion() {
		t.Fatal("expected slash command completion to be accepted")
	}
	if got := m.textInput.Value(); got != "/skill:bravo " {
		t.Fatalf("expected down arrow to select next row, got %q", got)
	}
}

func TestSlashCommandSuggestionScrollsAfterSelectionLeavesWindow(t *testing.T) {
	m := newTestModel()
	m.dispatcher = newCompletionTestDispatcher()
	m.textInput.SetValue("/skill:")

	for range 6 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	out := plainView(m.renderSlashCommandSuggestions())
	if !strings.Contains(out, "2-7 of 8") {
		t.Fatalf("expected completion window to scroll one row, got:\n%s", out)
	}
	if !strings.Contains(out, "/skill:bravo") || !strings.Contains(out, "/skill:golf") {
		t.Fatalf("expected scrolled suggestions, got:\n%s", out)
	}
	if strings.Contains(out, "/skill:alpha") {
		t.Fatalf("did not expect first suggestion after scroll, got:\n%s", out)
	}

	if !m.acceptSlashCommandCompletion() {
		t.Fatal("expected slash command completion to be accepted")
	}
	if got := m.textInput.Value(); got != "/skill:golf " {
		t.Fatalf("expected selected row completion, got %q", got)
	}
}

func TestSlashCommandCompletionAcceptsSelectedRow(t *testing.T) {
	m := newTestModel()
	m.dispatcher = newCompletionTestDispatcher()
	m.textInput.SetValue("/skill:")

	for range 7 {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})

	out := plainView(m.renderSlashCommandSuggestions())
	if !strings.Contains(out, "3-8 of 8") {
		t.Fatalf("expected window to keep selected row visible, got:\n%s", out)
	}
	if !m.acceptSlashCommandCompletion() {
		t.Fatal("expected slash command completion to be accepted")
	}
	if got := m.textInput.Value(); got != "/skill:golf " {
		t.Fatalf("expected selected row completion, got %q", got)
	}
}

func TestAgentEnd_FinalizesTranscriptWithToolCallsFromPreviousTurn(t *testing.T) {
	m := newTestModel()
	calledPrintAbove := false
	m.printAboveFn = func(string) { calledPrintAbove = true }

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

	// Second TurnStart keeps the previous round in transcript.
	flushCmd := m.handleRuntimeEvent(runtime.Event{Kind: runtime.EventTurnStart, Timestamp: time.Now()})
	if flushCmd != nil {
		t.Fatal("expected TurnStart to avoid print commands after transcript migration")
	}

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
	if cmd != nil {
		t.Fatal("expected agent end to avoid print commands after transcript migration")
	}
	if calledPrintAbove {
		t.Fatal("agent end printed above the TUI")
	}
	asst := m.transcript.activeAssistant()
	if asst == nil || asst.status != assistantStatusDone {
		t.Fatalf("assistant not finalized: %+v", asst)
	}
	out := plainView(m.renderer.renderTranscript(m.transcript, renderContext{width: 80}))
	if !strings.Contains(out, "read_file") {
		t.Fatalf("expected transcript to include read_file from a prior turn; got:\n%s", out)
	}
	if !strings.Contains(out, "wrapping up") || !strings.Contains(out, "hello world") {
		t.Fatalf("expected transcript to include reasoning and assistant answer; got:\n%s", out)
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

func TestTranscriptRendererReasoningBlockSummarizesLiveAndCompleted(t *testing.T) {
	reasoning := strings.Join([]string{
		"line 1 hidden",
		"line 2 hidden",
		"line 3 hidden",
		"line 4 visible in live only",
		"line 5 visible",
		"line 6 visible",
		"line 7 visible",
	}, "\n")

	live := plainView(renderReasoningBlockForWidth(reasoning, reasoningModeLive, 80))
	completed := plainView(renderReasoningBlockForWidth(reasoning, reasoningModeCompleted, 80))

	for name, out := range map[string]string{"live": live, "completed": completed} {
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
	if strings.Contains(completed, "line 4 visible in live only") {
		t.Fatalf("completed reasoning should show only last three lines, got:\n%s", completed)
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

	var tr transcript
	asst := tr.appendAssistantBlock("assistant-1", time.Unix(1, 0))
	asst.upsertToolStart("call-1", "shell", map[string]any{"extremely_long_argument_key": strings.Repeat("value ", 20)}, time.Unix(1, 0))

	out := plainView((transcriptRenderer{}).renderTranscript(tr, renderContext{width: 32, active: true, now: time.Unix(2, 0)}))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "extremely") || strings.Contains(line, "...") {
			if width := lipgloss.Width(line); width > 32 {
				t.Fatalf("expected structured arg line to fit terminal width, got width %d for line %q", width, line)
			}
		}
	}
}

func TestRenderToolResultShowsPreviewAndHiddenLineCount(t *testing.T) {
	var tr transcript
	asst := tr.appendAssistantBlock("assistant-1", time.Unix(1, 0))
	asst.upsertToolStart("call-1", "read_file", nil, time.Unix(1, 0))
	asst.finishTool("call-1", "read_file", strings.Join([]string{
		"line 1",
		"line 2",
		"line 3",
		"line 4",
		"line 5 hidden",
		"line 6 hidden",
	}, "\n"), false, time.Unix(2, 0))

	out := plainView((transcriptRenderer{}).renderTranscript(tr, renderContext{width: 80}))
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
