package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"nanobot-go/internal/bus"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type interactiveModel struct {
	textInput        textinput.Model
	messageBus       bus.MessageBus
	sessionKey       string
	chatID           string
	waiting          bool
	quitting         bool
	done             chan struct{}
	mu               sync.Mutex
	spinnerIdx       int
	agentEventCh     <-chan bus.AgentEvent
	outboundCh       <-chan bus.OutboundMessage
	responseReceived bool
	program          *tea.Program // Reference to the program for printing

	active          bool
	toolCalls       []toolCallEntry
	streamText      string
	streamReasoning string
	status          string // Current agent status
}

type toolCallEntry struct {
	id         string
	name       string
	args       string
	status     string // "pending" | "running" | "done" | "error"
	result     string
	durationMs int64
	expanded   bool
}

type spinnerTickMsg struct{}

type responseMsg struct {
	content         string
	reasoning       string
	agentEventFinal bool
	fallback        bool
}

type agentEventMsg struct {
	ev bus.AgentEvent
}

type pollTickMsg struct{}

func newInteractiveModel(messageBus bus.MessageBus, sessionKey, chatID string) *interactiveModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.Prompt = "You: "

	return &interactiveModel{
		textInput:    ti,
		messageBus:   messageBus,
		sessionKey:   sessionKey,
		chatID:       chatID,
		done:         make(chan struct{}),
		agentEventCh: messageBus.SubscribeAgentEvents(),
		outboundCh:   messageBus.ConsumeOutbound(),
	}
}

func (m *interactiveModel) SetProgram(p *tea.Program) {
	m.program = p
}

func (m *interactiveModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.tickSpinner(), m.pollEvents())
}

func (m *interactiveModel) pollEvents() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-m.agentEventCh:
			return agentEventMsg{ev: ev}
		case resp, ok := <-m.outboundCh:
			if !ok {
				return nil
			}
			return responseMsg{
				content:         resp.Content,
				reasoning:       resp.Reasoning,
				agentEventFinal: outboundFromAgentEventFinal(resp),
			}
		case <-m.done:
			return nil
		case <-time.After(50 * time.Millisecond):
			return pollTickMsg{}
		}
	}
}

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

func (m *interactiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pollTickMsg:
		return m, m.pollEvents()

	case spinnerTickMsg:
		m.mu.Lock()
		if m.waiting {
			m.spinnerIdx = (m.spinnerIdx + 1) % len(spinnerFrames)
		}
		m.mu.Unlock()
		return m, m.tickSpinner()

	case responseMsg:
		m.mu.Lock()
		if msg.agentEventFinal && !msg.fallback && m.active && !m.responseReceived {
			m.mu.Unlock()
			return m, tea.Batch(m.pollEvents(), m.deferResponse(msg))
		}
		cmd := m.finalizeAssistantMessage(msg.content, msg.reasoning)
		m.mu.Unlock()
		return m, cmd

	case agentEventMsg:
		m.mu.Lock()
		if msg.ev.SessionKey == m.sessionKey && m.active {
			switch msg.ev.Type {
			case bus.EventLLMThinking:
				m.status = "thinking"
				m.streamText = ""
				m.streamReasoning = ""
			case bus.EventLLMResponding:
				m.status = "responding"
				m.streamText = ""
				m.streamReasoning = ""
			case bus.EventLLMStreamChunk:
				m.status = "streaming"
				if data, ok := msg.ev.StreamChunk(); ok {
					if data.IsReasoning {
						m.streamReasoning = data.FullText
					} else {
						m.streamText = data.FullText
					}
				}
			case bus.EventLLMFinal:
				var content, reasoning string
				if data, ok := msg.ev.LLMFinal(); ok {
					content = data.Content
					reasoning = data.ReasoningContent
					if data.Error != "" && content == "" {
						content = "Error: " + data.Error
					}
				}
				cmd := m.finalizeAssistantMessage(content, reasoning)
				m.mu.Unlock()
				return m, cmd
			case bus.EventLLMToolCalls:
				m.status = "using tools"
				if toolCalls, ok := msg.ev.ToolCalls(); ok {
					for _, tc := range toolCalls {
						m.toolCalls = append(m.toolCalls, toolCallEntry{
							id: tc.ID, name: tc.Name, args: formatArgs(tc.Args), status: "pending",
						})
					}
				}
			case bus.EventToolStart:
				if data, ok := msg.ev.ToolCall(); ok {
					if idx := m.findToolCall(data.ID, data.Name, "pending", ""); idx >= 0 {
						m.toolCalls[idx].status = "running"
						m.toolCalls[idx].args = formatArgs(data.Args)
					} else {
						m.toolCalls = append(m.toolCalls, toolCallEntry{
							id: data.ID, name: data.Name, args: formatArgs(data.Args), status: "running",
						})
					}
				}
			case bus.EventToolEnd:
				if data, ok := msg.ev.ToolResult(); ok {
					if idx := m.findToolCall(data.ToolID, data.ToolName); idx >= 0 {
						if data.Success {
							m.toolCalls[idx].status = "done"
							m.toolCalls[idx].result = data.Result
						} else {
							m.toolCalls[idx].status = "error"
							m.toolCalls[idx].result = data.Error
						}
						m.toolCalls[idx].durationMs = data.DurationMs
					}
				}
			case bus.EventToolError:
				if data, ok := msg.ev.ToolResult(); ok {
					if idx := m.findToolCall(data.ToolID, data.ToolName); idx >= 0 {
						m.toolCalls[idx].status = "error"
						m.toolCalls[idx].result = data.Error
						m.toolCalls[idx].durationMs = data.DurationMs
					}
				}
			case bus.EventSessionEnd:
				m.waiting = false
				m.active = false
			}
		}
		m.mu.Unlock()
		return m, m.pollEvents()

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			if m.active && (m.streamText != "" || m.streamReasoning != "" || len(m.toolCalls) > 0) {
				output := formatAssistantMessage(append([]toolCallEntry(nil), m.toolCalls...), m.streamText, m.streamReasoning)
				cmd := m.printAbove(output)
				m.clearActiveState()
				m.active = false
				m.waiting = false
				m.quitting = true
				return m, tea.Batch(cmd, tea.Quit)
			}
			m.quitting = true
			return m, tea.Quit
		}
		if m.waiting {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEnter:
			userInput := strings.TrimSpace(m.textInput.Value())
			m.textInput.SetValue("")
			if userInput == "" {
				return m, nil
			}
			lower := strings.ToLower(userInput)
			if lower == "exit" || lower == "quit" || lower == "/exit" || lower == "/quit" || lower == ":q" {
				m.quitting = true
				return m, tea.Quit
			}
			m.mu.Lock()
			m.active = true
			m.waiting = true
			m.responseReceived = false
			m.spinnerIdx = 0
			m.toolCalls = nil
			m.streamText = ""
			m.streamReasoning = ""
			m.status = ""
			m.mu.Unlock()

			// Print user message above the view
			userMsg := userPromptStyle.Render("You:") + " " + userInput
			cmd := m.printAbove(userMsg)

			m.messageBus.PublishInbound(bus.InboundMessage{
				Channel: "cli", SenderID: "user", ChatID: m.chatID,
				Content: userInput, SessionKey: m.sessionKey,
			})

			return m, tea.Batch(cmd, m.pollEvents())
		}
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *interactiveModel) finalizeAssistantMessage(content, reasoning string) tea.Cmd {
	if m.responseReceived {
		return m.pollEvents()
	}
	m.responseReceived = true
	m.waiting = false
	m.active = false
	m.status = "done"

	toolCalls := append([]toolCallEntry(nil), m.toolCalls...)
	m.clearActiveState()
	output := formatAssistantMessage(toolCalls, content, reasoning)
	return tea.Batch(m.printAbove(output), m.pollEvents())
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

func (m *interactiveModel) clearActiveState() {
	m.toolCalls = nil
	m.streamText = ""
	m.streamReasoning = ""
}

func (m *interactiveModel) findToolCall(id, name string, statuses ...string) int {
	statusAllowed := func(status string) bool {
		if len(statuses) == 0 {
			return true
		}
		for _, allowed := range statuses {
			if status == allowed {
				return true
			}
		}
		return false
	}

	if id != "" {
		for i := range m.toolCalls {
			if m.toolCalls[i].id == id && statusAllowed(m.toolCalls[i].status) {
				return i
			}
		}
	}
	for i := range m.toolCalls {
		if m.toolCalls[i].name == name && statusAllowed(m.toolCalls[i].status) {
			return i
		}
	}
	return -1
}

func outboundFromAgentEventFinal(msg bus.OutboundMessage) bool {
	v, ok := msg.Metadata[bus.OutboundMetadataAgentEventFinal]
	if !ok {
		return false
	}
	fromEvent, ok := v.(bool)
	return ok && fromEvent
}

func (m *interactiveModel) View() string {
	sep := borderStyle.Render(strings.Repeat("─", min(60, getTerminalWidth())))
	var s strings.Builder

	if m.quitting {
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render("Goodbye!\n"))
		return s.String()
	}

	if m.active {
		s.WriteString(assistantLabelStyle.Render("nanobot"))
		s.WriteString("\n")

		// Tool calls with live status
		if len(m.toolCalls) > 0 {
			s.WriteString("\n")
			for _, tc := range m.toolCalls {
				var icon, statusText string
				var iconStyle lipgloss.Style
				switch tc.status {
				case "done":
					icon = "✓"
					statusText = fmt.Sprintf(" • %s", formatDuration(tc.durationMs))
					iconStyle = toolDoneStyle
				case "error":
					icon = "✗"
					if tc.durationMs > 0 {
						statusText = fmt.Sprintf(" • %s", formatDuration(tc.durationMs))
					}
					iconStyle = toolErrorStyle
				case "running":
					icon = spinnerFrames[m.spinnerIdx]
					statusText = " running..."
					iconStyle = toolRunningStyle
				default:
					icon = "○"
					statusText = " pending"
					iconStyle = toolEntryStyle
				}
				s.WriteString("  ")
				s.WriteString(iconStyle.Render(icon) + " ")
				s.WriteString(toolEntryStyle.Render(tc.name))
				s.WriteString(toolDurationStyle.Render(statusText))
				s.WriteString("\n")
				// Use compact display for args and results
				maxWidth := getTerminalWidth() - 12
				if maxWidth < 40 {
					maxWidth = 40
				}
				hasResult := (tc.status == "done" && tc.result != "") || (tc.status == "error" && tc.result != "")
				if tc.args != "" {
					prefix := "    ┌ "
					if !hasResult {
						prefix = "    └ "
					}
					s.WriteString(toolArgsStyle.Render(prefix + "Args: " + truncateStr(tc.args, maxWidth)))
					s.WriteString("\n")
				}
				if tc.status == "error" && tc.result != "" {
					s.WriteString("    └ " + toolErrorStyle.Render("Error: "+truncateStr(tc.result, maxWidth)))
					s.WriteString("\n")
				} else if tc.status == "done" && tc.result != "" {
					s.WriteString(toolArgsStyle.Render("    └ Result: " + truncateStr(tc.result, maxWidth)))
					s.WriteString("\n")
				}
			}
			s.WriteString("\n")
		}

		// Keep reasoning live, but defer assistant text until the final message.
		// Rendering long streamed text in the normal terminal buffer leaves stale
		// frames in scrollback, which looks like duplicated responses.
		if m.streamReasoning != "" {
			s.WriteString(reasoningStyle.Render(m.streamReasoning))
			s.WriteString("\n")
		}

		// Status indicator during active streaming
		if m.status != "" && m.status != "done" {
			s.WriteString("\n")
			s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
			s.WriteString(" ")
			s.WriteString(toolRunningStyle.Render(m.status + "..."))
			s.WriteString("\n")
		}
	}

	// Footer
	s.WriteString(sep)
	s.WriteString("\n")
	// Only show waiting when waiting for a new turn, not during streaming
	if m.waiting && !m.active {
		s.WriteString(waitingStyle.Render("> waiting for response..."))
		s.WriteString("\n\n")
	}
	s.WriteString(m.textInput.View())
	s.WriteString("\n")

	return s.String()
}
