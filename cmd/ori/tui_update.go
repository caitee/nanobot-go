package main

import (
	"fmt"
	"strings"
	"time"

	"ori/internal/bus"
	"ori/internal/llm"
	"ori/internal/runtime"
	"ori/internal/tool"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *interactiveModel) tickSpinner() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second / 10)
		return spinnerTickMsg{}
	}
}

func (m *interactiveModel) deferResponse(msg responseMsg) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(100 * time.Millisecond)
		msg.fallback = true
		return msg
	}
}

// Update is the tea.Model entry point. It handles tickers, runtime events,
// final outbound messages, and keyboard input. Runtime events and outbound
// messages are delivered by the pump goroutine (see tui_model.go) rather than
// polled from Update, so each branch only needs to return the cmd it truly
// needs — never the "keep polling" plumbing.
func (m *interactiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerTickMsg:
		m.mu.Lock()
		waiting := m.waiting
		if waiting {
			m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		}
		m.mu.Unlock()
		if waiting {
			return m, m.tickSpinner()
		}
		return m, nil

	case responseMsg:
		m.mu.Lock()
		// If the runtime event stream is still expected to produce the final,
		// defer the outbound-based finalization by 100ms so the UI preserves
		// the richer agent_end path.
		if msg.agentEventFinal && !msg.fallback && m.active && !m.responseReceived {
			m.mu.Unlock()
			return m, m.deferResponse(msg)
		}
		if m.finalizeTranscriptFromOutbound(msg.content, msg.reasoning, msg.agentEventFinal) {
			m.refreshTranscriptViewport()
			m.viewVersion++
		}
		m.mu.Unlock()
		return m, nil

	case runtimeEventMsg:
		m.mu.Lock()
		if m.reduceRuntimeEvent(msg.ev) {
			m.refreshTranscriptViewport()
			m.viewVersion++
		}
		m.mu.Unlock()
		return m, nil

	case tea.WindowSizeMsg:
		m.resizeTranscriptViewport(msg.Width, transcriptViewportHeightFor(msg.Height))
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			if m.active {
				m.cancelActiveAssistant()
				m.quitting = true
				m.shutdown()
				m.refreshTranscriptViewport()
				m.viewVersion++
				return m, tea.Quit
			}
			m.quitting = true
			m.shutdown()
			m.viewVersion++
			return m, tea.Quit
		}
		if m.panel != nil {
			if handled, cmd := m.handleManagementPanelKey(msg); handled {
				return m, cmd
			}
		}
		if m.focus == focusTranscript {
			if msg.Type == tea.KeyEsc {
				m.focus = focusInput
				m.viewVersion++
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if m.viewport.AtBottom() {
				m.clearNewTranscriptOutput()
			}
			m.viewVersion++
			return m, cmd
		}
		switch msg.Type {
		case tea.KeyPgUp, tea.KeyPgDown:
			m.focus = focusTranscript
			if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
				m.resizeTranscriptViewport(getTerminalWidth(), transcriptViewportHeight())
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if m.viewport.AtBottom() {
				m.clearNewTranscriptOutput()
			}
			m.viewVersion++
			return m, cmd
		case tea.KeyEsc:
			if m.focus == focusTranscript {
				m.focus = focusInput
				m.viewVersion++
				return m, nil
			}
		}
		if m.waiting {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyTab:
			if m.acceptSlashCommandCompletion() {
				return m, nil
			}
		case tea.KeyUp:
			if m.moveSlashCommandSelection(-1) {
				return m, nil
			}
		case tea.KeyDown:
			if m.moveSlashCommandSelection(1) {
				return m, nil
			}
		case tea.KeyEnter:
			return m.handleEnter()
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *interactiveModel) handleEnter() (tea.Model, tea.Cmd) {
	userInput := strings.TrimSpace(m.textInput.Value())
	if userInput == "" {
		m.textInput.SetValue("")
		return m, nil
	}
	lower := strings.ToLower(userInput)
	if lower == "exit" || lower == "quit" || lower == ":q" {
		m.textInput.SetValue("")
		m.quitting = true
		m.shutdown()
		return m, tea.Quit
	}
	if strings.HasPrefix(userInput, "/") {
		if m.shouldCompleteSlashCommandOnEnter(userInput) && m.acceptSlashCommandCompletion() {
			return m, nil
		}
		m.textInput.SetValue("")
		if handled, cmd := m.handleSlashCommand(userInput); handled {
			return m, cmd
		}
	} else {
		m.textInput.SetValue("")
	}
	return m, m.submitPrompt(userInput, userInput)
}

// handleRuntimeEvent keeps legacy tests on the transcript reducer path.
// Called with m.mu held.
func (m *interactiveModel) handleRuntimeEvent(ev runtime.Event) tea.Cmd {
	if !m.active && ev.Kind != runtime.EventAgentEnd {
		return nil
	}
	if m.reduceRuntimeEvent(ev) {
		m.refreshTranscriptViewport()
	}
	return nil
}

func cloneToolArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func (m *interactiveModel) printAbove(content string) tea.Cmd {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil
	}
	return func() tea.Msg {
		if m.printAboveFn != nil {
			m.printAboveFn(content)
			return nil
		}
		if m.program != nil {
			m.program.Println(content)
		}
		return nil
	}
}

// outboundFromAgentEventFinal reports whether an outbound message came from
// the agent_end path (so the TUI knows whether to prefer its own finalization).
func outboundFromAgentEventFinal(msg bus.OutboundMessage) bool {
	v, ok := msg.Metadata[bus.OutboundMetadataAgentEventFinal]
	if !ok {
		return false
	}
	fromEvent, ok := v.(bool)
	return ok && fromEvent
}

// contentsToString renders a tool_result payload (typically []llm.Content) into
// a short string for the TUI.
func contentsToString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []llm.Content:
		var b strings.Builder
		for _, c := range x {
			if t, ok := c.(llm.TextContent); ok {
				b.WriteString(t.Text)
			}
		}
		return b.String()
	case tool.Result:
		return contentsToString(x.Content)
	case *tool.Result:
		if x == nil {
			return ""
		}
		return contentsToString(x.Content)
	}
	return fmt.Sprintf("%v", v)
}
