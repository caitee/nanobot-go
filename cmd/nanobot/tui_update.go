package main

import (
	"fmt"
	"strings"
	"time"

	appcore "nanobot-go/internal/app"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/llm"
	"nanobot-go/internal/runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *interactiveModel) tickSpinner() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second / 10)
		return spinnerTickMsg{}
	}
}

func (m *interactiveModel) tickTypewriter() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(15 * time.Millisecond)
		return typewriterTickMsg{}
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

	case typewriterTickMsg:
		m.mu.Lock()
		if len(m.typewriterQueue) > 0 {
			charsPerTick := 1
			qLen := len(m.typewriterQueue)
			switch {
			case qLen > 80:
				charsPerTick = 8
			case qLen > 40:
				charsPerTick = 4
			case qLen > 20:
				charsPerTick = 2
			}
			n := min(charsPerTick, len(m.typewriterQueue))
			m.displayedText += string(m.typewriterQueue[:n])
			m.typewriterQueue = m.typewriterQueue[n:]
		}
		m.mu.Unlock()
		if len(m.typewriterQueue) > 0 {
			return m, m.tickTypewriter()
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
		cmd := m.finalizeAssistantMessage(msg.content, msg.reasoning)
		m.mu.Unlock()
		return m, cmd

	case runtimeEventMsg:
		m.mu.Lock()
		cmd := m.handleRuntimeEvent(msg.ev)
		m.mu.Unlock()
		return m, cmd

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			if m.active && (m.streamText != "" || (m.currentRound != nil && m.currentRound.reasoning != "") || len(m.rounds) > 0) {
				output := m.formatCurrentState()
				cmd := m.printAbove(output)
				m.clearActiveState()
				m.active = false
				m.waiting = false
				m.quitting = true
				m.shutdown()
				return m, tea.Batch(cmd, tea.Quit)
			}
			m.quitting = true
			m.shutdown()
			return m, tea.Quit
		}
		if m.waiting {
			return m, nil
		}
		switch msg.Type {
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
	m.textInput.SetValue("")
	if userInput == "" {
		return m, nil
	}
	lower := strings.ToLower(userInput)
	if lower == "exit" || lower == "quit" || lower == "/exit" || lower == "/quit" || lower == ":q" {
		m.quitting = true
		m.shutdown()
		return m, tea.Quit
	}
	m.mu.Lock()
	m.active = true
	m.waiting = true
	m.responseReceived = false
	m.spinnerIdx = 0
	m.rounds = nil
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
	m.status = "waiting"
	m.mu.Unlock()

	padded := userInput + strings.Repeat(" ", max(0, getTerminalWidth()-lipgloss.Width(userInput)))
	userMsg := "\n" + userMessageStyle.Render(padded)
	cmd := m.printAbove(userMsg)

	m.dispatcher.Bus().PublishInbound(bus.InboundMessage{
		Channel: "cli", SenderID: "user", ChatID: m.chatID,
		Content: userInput, SessionKey: m.sessionKey,
	})

	return m, tea.Batch(cmd, m.tickSpinner())
}

// handleRuntimeEvent processes a single runtime.Event. Called with m.mu held.
// Returns a Cmd for the final message path; nil otherwise.
func (m *interactiveModel) handleRuntimeEvent(ev runtime.Event) tea.Cmd {
	if !m.active {
		return nil
	}
	switch ev.Kind {
	case runtime.EventAgentStart:
		m.status = "thinking"

	case runtime.EventTurnStart:
		m.status = "thinking"
		// New turn → close out the previous round if it had content.
		if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
			m.rounds = append(m.rounds, *m.currentRound)
		}
		m.currentRound = &thinkingRound{}
		m.streamText = ""
		m.displayedText = ""
		m.typewriterQueue = nil

	case runtime.EventMessageUpdate:
		data, ok := ev.MessageUpdate()
		if !ok {
			return nil
		}
		switch data.StreamEvent.Kind {
		case llm.StreamEventThinkingDelta:
			m.status = "thinking"
			if m.currentRound == nil {
				m.currentRound = &thinkingRound{}
			}
			m.currentRound.reasoning += data.StreamEvent.Delta
		case llm.StreamEventTextDelta:
			m.status = "responding"
			m.streamText += data.StreamEvent.Delta
			if data.StreamEvent.Delta != "" {
				wasEmpty := len(m.typewriterQueue) == 0
				m.typewriterQueue = append(m.typewriterQueue, []rune(data.StreamEvent.Delta)...)
				if wasEmpty {
					return m.tickTypewriter()
				}
			}
		}

	case runtime.EventToolExecutionStart:
		m.status = "using tools"
		data, ok := ev.ToolStart()
		if !ok {
			return nil
		}
		if m.currentRound == nil {
			m.currentRound = &thinkingRound{}
		}
		m.currentRound.toolCalls = append(m.currentRound.toolCalls, toolCallEntry{
			id:     data.ToolCallID,
			name:   data.ToolName,
			args:   formatArgs(data.Args),
			status: "running",
		})

	case runtime.EventToolExecutionEnd:
		data, ok := ev.ToolEnd()
		if !ok {
			return nil
		}
		if idx := m.findToolCall(data.ToolCallID, data.ToolName); idx >= 0 {
			if data.IsError {
				m.currentRound.toolCalls[idx].status = "error"
			} else {
				m.currentRound.toolCalls[idx].status = "done"
			}
			m.currentRound.toolCalls[idx].result = contentsToString(data.Result)
		}

	case runtime.EventAgentEnd:
		// Finalize in this turn. The outbound message arrives shortly after
		// with the same content (published by the dispatcher), but we use
		// the agent_end payload directly — it's the authoritative record.
		data, _ := ev.AgentEnd()
		text, reasoning := appcore.ExtractFinalAssistant(data.Messages)
		if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
			m.rounds = append(m.rounds, *m.currentRound)
			m.currentRound = nil
		}
		return m.finalizeAssistantMessage(text, reasoning)
	}
	return nil
}

func (m *interactiveModel) findToolCall(id, name string) int {
	if m.currentRound == nil {
		return -1
	}
	if id != "" {
		for i := range m.currentRound.toolCalls {
			if m.currentRound.toolCalls[i].id == id {
				return i
			}
		}
	}
	for i := range m.currentRound.toolCalls {
		if m.currentRound.toolCalls[i].name == name {
			return i
		}
	}
	return -1
}

// finalizeAssistantMessage prints the final response above the TUI and clears
// the active scratch area. Callers must hold m.mu.
func (m *interactiveModel) finalizeAssistantMessage(content, reasoning string) tea.Cmd {
	if m.responseReceived {
		return nil
	}
	m.responseReceived = true
	m.waiting = false
	m.active = false
	m.status = "done"

	if len(m.typewriterQueue) > 0 {
		m.displayedText += string(m.typewriterQueue)
		m.typewriterQueue = nil
	}

	output := m.formatFinalMessage(content, reasoning)
	m.clearActiveState()
	return m.printAbove(output)
}

func (m *interactiveModel) formatCurrentState() string {
	var allRounds []thinkingRound
	allRounds = append(allRounds, m.rounds...)
	if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
		allRounds = append(allRounds, *m.currentRound)
	}
	return formatAssistantMessage(allRounds, m.displayedText, "")
}

func (m *interactiveModel) formatFinalMessage(content, reasoning string) string {
	var allRounds []thinkingRound
	allRounds = append(allRounds, m.rounds...)
	if reasoning != "" {
		if len(allRounds) == 0 || allRounds[len(allRounds)-1].reasoning != reasoning {
			allRounds = append(allRounds, thinkingRound{reasoning: reasoning})
		}
	}
	return formatAssistantMessage(allRounds, content, reasoning)
}

func (m *interactiveModel) clearActiveState() {
	m.rounds = nil
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
}

func (m *interactiveModel) printAbove(content string) tea.Cmd {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil
	}
	return func() tea.Msg {
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
	}
	return fmt.Sprintf("%v", v)
}
