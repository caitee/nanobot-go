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
	rounds          []thinkingRound // All completed rounds
	currentRound    *thinkingRound  // Current round being built
	streamText      string          // Full text received from stream
	displayedText   string          // Text currently displayed (typewriter effect)
	typewriterQueue []rune          // Queue of runes waiting to be displayed
	status          string          // Current agent status
}

// thinkingRound represents one round of thinking + tool calls
type thinkingRound struct {
	reasoning string
	toolCalls []toolCallEntry
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

type agentEventBatchMsg struct {
	events []bus.AgentEvent
}

type pollTickMsg struct{}

type typewriterTickMsg struct{}

func newInteractiveModel(messageBus bus.MessageBus, sessionKey, chatID string) *interactiveModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.Prompt = "> "

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
	return tea.Batch(textinput.Blink, m.tickSpinner(), m.pollEvents(), m.tickTypewriter())
}

func (m *interactiveModel) pollEvents() tea.Cmd {
	return func() tea.Msg {
		// Try to consume multiple agent events in one go to avoid channel overflow
		var events []bus.AgentEvent
		for {
			select {
			case ev := <-m.agentEventCh:
				events = append(events, ev)
				// Keep draining if more events are available
				if len(events) < 20 { // Limit batch size
					continue
				}
				return agentEventBatchMsg{events: events}
			case resp, ok := <-m.outboundCh:
				if !ok {
					return nil
				}
				// If we have pending events, return them first
				if len(events) > 0 {
					return agentEventBatchMsg{events: events}
				}
				return responseMsg{
					content:         resp.Content,
					reasoning:       resp.Reasoning,
					agentEventFinal: outboundFromAgentEventFinal(resp),
				}
			case <-m.done:
				return nil
			default:
				// No more events immediately available
				if len(events) > 0 {
					return agentEventBatchMsg{events: events}
				}
				// Wait a bit before polling again
				time.Sleep(10 * time.Millisecond)
				return pollTickMsg{}
			}
		}
	}
}

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

	case typewriterTickMsg:
		m.mu.Lock()
		if len(m.typewriterQueue) > 0 {
			// If queue is large (new chunk arrived), accelerate
			charsPerTick := 1
			qLen := len(m.typewriterQueue)
			if qLen > 80 {
				charsPerTick = 8
			} else if qLen > 40 {
				charsPerTick = 4
			} else if qLen > 20 {
				charsPerTick = 2
			}
			n := min(charsPerTick, len(m.typewriterQueue))
			m.displayedText += string(m.typewriterQueue[:n])
			m.typewriterQueue = m.typewriterQueue[n:]
		}
		m.mu.Unlock()
		return m, m.tickTypewriter()

	case responseMsg:
		m.mu.Lock()
		if msg.agentEventFinal && !msg.fallback && m.active && !m.responseReceived {
			m.mu.Unlock()
			return m, tea.Batch(m.pollEvents(), m.deferResponse(msg))
		}
		cmd := m.finalizeAssistantMessage(msg.content, msg.reasoning)
		m.mu.Unlock()
		return m, cmd

	case agentEventBatchMsg:
		m.mu.Lock()
		var finalCmd tea.Cmd
		for _, ev := range msg.events {
			if ev.SessionKey == m.sessionKey && m.active {
				if ev.Type == bus.EventLLMFinal {
					var content, reasoning string
					if data, ok := ev.LLMFinal(); ok {
						content = data.Content
						reasoning = data.ReasoningContent
						if data.Error != "" && content == "" {
							content = "Error: " + data.Error
						}
					}
					if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
						m.rounds = append(m.rounds, *m.currentRound)
						m.currentRound = nil
					}
					finalCmd = m.finalizeAssistantMessage(content, reasoning)
				} else {
					m.processAgentEvent(ev)
				}
			}
		}
		m.mu.Unlock()
		if finalCmd != nil {
			return m, finalCmd
		}
		return m, m.pollEvents()

	case agentEventMsg:
		m.mu.Lock()
		if msg.ev.SessionKey == m.sessionKey && m.active {
			if msg.ev.Type == bus.EventLLMFinal {
				var content, reasoning string
				if data, ok := msg.ev.LLMFinal(); ok {
					content = data.Content
					reasoning = data.ReasoningContent
					if data.Error != "" && content == "" {
						content = "Error: " + data.Error
					}
				}
				if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
					m.rounds = append(m.rounds, *m.currentRound)
					m.currentRound = nil
				}
				cmd := m.finalizeAssistantMessage(content, reasoning)
				m.mu.Unlock()
				return m, cmd
			}
			m.processAgentEvent(msg.ev)
		}
		m.mu.Unlock()
		return m, m.pollEvents()

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
			m.rounds = nil
			m.currentRound = nil
			m.streamText = ""
			m.displayedText = ""
			m.typewriterQueue = nil
			m.status = "waiting"
			m.mu.Unlock()

			// Print user message with background highlight, add blank line for spacing
			padded := userInput + strings.Repeat(" ", max(0, getTerminalWidth()-lipgloss.Width(userInput)))
			userMsg := "\n" + userMessageStyle.Render(padded)
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

	// Flush any remaining typewriter queue immediately
	if len(m.typewriterQueue) > 0 {
		m.displayedText += string(m.typewriterQueue)
		m.typewriterQueue = nil
	}

	output := m.formatFinalMessage(content, reasoning)
	m.clearActiveState()
	return tea.Batch(m.printAbove(output), m.pollEvents())
}

// formatCurrentState formats the current state for display (used during Ctrl+C)
func (m *interactiveModel) formatCurrentState() string {
	var allRounds []thinkingRound
	// Add completed rounds
	allRounds = append(allRounds, m.rounds...)
	// Add current round if it has content
	if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
		allRounds = append(allRounds, *m.currentRound)
	}
	return formatAssistantMessage(allRounds, m.displayedText, "")
}

// formatFinalMessage formats the final message with all rounds
func (m *interactiveModel) formatFinalMessage(content, reasoning string) string {
	var allRounds []thinkingRound
	// Add completed rounds
	allRounds = append(allRounds, m.rounds...)
	// Add final reasoning if provided and not already in a round
	if reasoning != "" {
		// Check if we need to add it as a new round
		if len(allRounds) == 0 || allRounds[len(allRounds)-1].reasoning != reasoning {
			allRounds = append(allRounds, thinkingRound{reasoning: reasoning})
		}
	}
	return formatAssistantMessage(allRounds, content, reasoning)
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

// renderRound renders a single thinking round (reasoning + tool calls)
func (m *interactiveModel) renderRound(round thinkingRound, isLive bool) string {
	var s strings.Builder

	// Reasoning for this round
	if round.reasoning != "" {
		renderedReasoning := renderReasoningMarkdown(round.reasoning)
		// Limit reasoning display to last maxReasoningLines visible lines
		const maxReasoningLines = 5
		lines := strings.Split(renderedReasoning, "\n")
		// Strip trailing empty lines from the rendered output
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > maxReasoningLines {
			// Show scroll indicator and last N lines
			hidden := len(lines) - maxReasoningLines
			s.WriteString(reasoningStyle.Render(fmt.Sprintf("  ⋮ (%d more lines)", hidden)))
			s.WriteString("\n")
			s.WriteString(strings.Join(lines[len(lines)-maxReasoningLines:], "\n"))
		} else {
			s.WriteString(strings.Join(lines, "\n"))
		}
		s.WriteString("\n")
	}

	// Tool calls for this round
	if len(round.toolCalls) > 0 {
		for _, tc := range round.toolCalls {
			var icon, statusText string
			var iconStyle lipgloss.Style
			switch tc.status {
			case "done":
				icon = "\u2713"
				statusText = fmt.Sprintf(" \u2022 %s", formatDuration(tc.durationMs))
				iconStyle = toolDoneStyle
			case "error":
				icon = "\u2717"
				if tc.durationMs > 0 {
					statusText = fmt.Sprintf(" \u2022 %s", formatDuration(tc.durationMs))
				}
				iconStyle = toolErrorStyle
			case "running":
				if isLive {
					icon = spinnerFrames[m.spinnerIdx]
				} else {
					icon = "\u25cb"
				}
				statusText = " running..."
				iconStyle = toolRunningStyle
			default:
				icon = "\u25cb"
				statusText = " pending"
				iconStyle = toolEntryStyle
			}
			s.WriteString("  ")
			s.WriteString(iconStyle.Render(icon) + " ")
			s.WriteString(toolEntryStyle.Render(tc.name))
			s.WriteString(toolDurationStyle.Render(statusText))
			s.WriteString("\n")
			// Compact display for args and results
			maxWidth := getTerminalWidth() - 12
			if maxWidth < 40 {
				maxWidth = 40
			}
			hasResult := (tc.status == "done" && tc.result != "") || (tc.status == "error" && tc.result != "")
			if tc.args != "" {
				prefix := "    \u250c "
				if !hasResult {
					prefix = "    \u2514 "
				}
				s.WriteString(toolArgsStyle.Render(prefix + "Args: " + truncateStr(tc.args, maxWidth)))
				s.WriteString("\n")
			}
			if tc.status == "error" && tc.result != "" {
				s.WriteString("    \u2514 " + toolErrorStyle.Render("Error: "+truncateStr(tc.result, maxWidth)))
				s.WriteString("\n")
			} else if tc.status == "done" && tc.result != "" {
				s.WriteString(toolArgsStyle.Render("    \u2514 Result: " + truncateStr(tc.result, maxWidth)))
				s.WriteString("\n")
			}
		}
	}

	return s.String()
}

func (m *interactiveModel) clearActiveState() {
	m.rounds = nil
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
}

// renderLiveContent safely renders live streaming content.
// It attempts markdown rendering but falls back to raw text if syntax is incomplete.
func (m *interactiveModel) renderLiveContent(text string) string {
	if text == "" {
		return ""
	}

	// Check if we're in the middle of special syntax that shouldn't be rendered yet
	if isIncompleteMarkdown(text) {
		// Return raw text with basic styling
		return text
	}

	// Try to render as markdown
	processed := preprocessMath(text)
	renderer := getMarkdownRenderer()
	if renderer == nil {
		return text
	}

	rendered, err := renderer.Render(processed)
	if err != nil {
		// Rendering failed, return raw text
		return text
	}

	return strings.TrimSuffix(rendered, "\n")
}

// isIncompleteMarkdown checks if the text ends with incomplete markdown syntax
func isIncompleteMarkdown(text string) bool {
	// Check for unclosed fenced code blocks
	if strings.Count(text, "```")%2 != 0 {
		return true
	}

	// Check for unclosed block math ($$...$$)
	// Remove matched pairs first, then see if an opener remains
	temp := text
	for {
		idx := strings.Index(temp, "$$")
		if idx < 0 {
			break
		}
		end := strings.Index(temp[idx+2:], "$$")
		if end < 0 {
			// Unclosed $$
			return true
		}
		temp = temp[:idx] + temp[idx+2+end+2:]
	}

	// Check for unclosed inline math ($...$)
	// After removing $$, count remaining single $
	count := 0
	for i := 0; i < len(temp); i++ {
		if temp[i] == '$' {
			count++
		}
	}
	if count%2 != 0 {
		return true
	}

	// Check for unclosed inline code backtick at the very end
	// (single backtick that hasn't been closed)
	if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		lastLine := text[idx+1:]
		if strings.Count(lastLine, "`")%2 != 0 {
			return true
		}
	} else {
		if strings.Count(text, "`")%2 != 0 {
			return true
		}
	}

	return false
}

// processAgentEvent handles a single agent event (called with lock held)
func (m *interactiveModel) processAgentEvent(ev bus.AgentEvent) {
	switch ev.Type {
	case bus.EventLLMThinking:
		m.status = "thinking"
		// Start a new round
		if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
			// Save the previous round
			m.rounds = append(m.rounds, *m.currentRound)
		}
		m.currentRound = &thinkingRound{}
		m.streamText = ""
		m.displayedText = ""
		m.typewriterQueue = nil
	case bus.EventLLMResponding:
		m.status = "responding"
		m.streamText = ""
		m.displayedText = ""
		m.typewriterQueue = nil
	case bus.EventLLMStreamChunk:
		m.status = "streaming"
		if data, ok := ev.StreamChunk(); ok {
			if data.IsReasoning {
				if m.currentRound != nil {
					m.currentRound.reasoning = data.FullText
				}
			} else {
				newText := data.FullText
				switch {
				case strings.HasPrefix(newText, m.streamText):
					delta := newText[len(m.streamText):]
					if delta != "" {
						m.typewriterQueue = append(m.typewriterQueue, []rune(delta)...)
					}
				case newText != m.streamText:
					// Fallback for non-append updates: resync displayed text and queue.
					m.displayedText = newText
					m.typewriterQueue = nil
				}
				m.streamText = newText
			}
		}
	case bus.EventLLMFinal:
		// This is handled separately in Update() because it needs to return a Cmd
	case bus.EventLLMToolCalls:
		m.status = "using tools"
		if toolCalls, ok := ev.ToolCalls(); ok {
			if m.currentRound == nil {
				m.currentRound = &thinkingRound{}
			}
			for _, tc := range toolCalls {
				m.currentRound.toolCalls = append(m.currentRound.toolCalls, toolCallEntry{
					id: tc.ID, name: tc.Name, args: formatArgs(tc.Args), status: "pending",
				})
			}
		}
	case bus.EventToolStart:
		if data, ok := ev.ToolCall(); ok {
			if idx := m.findToolCall(data.ID, data.Name, "pending", ""); idx >= 0 {
				m.currentRound.toolCalls[idx].status = "running"
				m.currentRound.toolCalls[idx].args = formatArgs(data.Args)
			} else if m.currentRound != nil {
				m.currentRound.toolCalls = append(m.currentRound.toolCalls, toolCallEntry{
					id: data.ID, name: data.Name, args: formatArgs(data.Args), status: "running",
				})
			}
		}
	case bus.EventToolEnd:
		if data, ok := ev.ToolResult(); ok {
			if idx := m.findToolCall(data.ToolID, data.ToolName); idx >= 0 {
				if data.Success {
					m.currentRound.toolCalls[idx].status = "done"
					m.currentRound.toolCalls[idx].result = data.Result
				} else {
					m.currentRound.toolCalls[idx].status = "error"
					m.currentRound.toolCalls[idx].result = data.Error
				}
				m.currentRound.toolCalls[idx].durationMs = data.DurationMs
			}
		}
	case bus.EventToolError:
		if data, ok := ev.ToolResult(); ok {
			if idx := m.findToolCall(data.ToolID, data.ToolName); idx >= 0 {
				m.currentRound.toolCalls[idx].status = "error"
				m.currentRound.toolCalls[idx].result = data.Error
				m.currentRound.toolCalls[idx].durationMs = data.DurationMs
			}
		}
	case bus.EventSessionEnd:
		m.waiting = false
		m.active = false
	}
}

func (m *interactiveModel) findToolCall(id, name string, statuses ...string) int {
	if m.currentRound == nil {
		return -1
	}
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
		for i := range m.currentRound.toolCalls {
			if m.currentRound.toolCalls[i].id == id && statusAllowed(m.currentRound.toolCalls[i].status) {
				return i
			}
		}
	}
	for i := range m.currentRound.toolCalls {
		if m.currentRound.toolCalls[i].name == name && statusAllowed(m.currentRound.toolCalls[i].status) {
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
	sep := borderStyle.Render(strings.Repeat("─", getTerminalWidth()))
	var s strings.Builder

	if m.quitting {
		s.WriteString(sep)
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render("Goodbye!\n"))
		s.WriteString("\n")
		return s.String()
	}

	if m.active {
		// Show nanobot header in live view
		s.WriteString(sep)
		s.WriteString("\n")
		s.WriteString(spinnerStyle.Render("✦"))
		s.WriteString(" ")
		s.WriteString(assistantLabelStyle.Render("nanobot"))
		s.WriteString("\n")

		// Display completed rounds in order
		for i, round := range m.rounds {
			if i > 0 {
				s.WriteString("\n")
			}
			s.WriteString(m.renderRound(round, false))
		}

		// Display current round (in progress)
		if m.currentRound != nil {
			if len(m.rounds) > 0 {
				s.WriteString("\n")
			}
			s.WriteString(m.renderRound(*m.currentRound, true))
		}

		// Display live streaming text (typewriter effect)
		if m.displayedText != "" {
			// Try to render markdown safely; fall back to raw text if incomplete
			renderedText := m.renderLiveContent(m.displayedText)
			s.WriteString(renderedText)
			if len(m.typewriterQueue) > 0 {
				s.WriteString("▍") // Cursor indicator while typing
			}
			s.WriteString("\n")
		}
	}

	// Footer
	s.WriteString("\n")
	// Show nanobot status bar when active or waiting
	if m.active && m.status != "" && m.status != "done" {
		s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
		s.WriteString(" ")
		s.WriteString(toolRunningStyle.Render(m.status))
		s.WriteString("\n")
	} else if m.waiting && !m.active {
		s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
		s.WriteString(" ")
		s.WriteString(waitingStyle.Render("waiting"))
		s.WriteString("\n")
	}
	s.WriteString(sep)
	s.WriteString("\n")
	s.WriteString(m.textInput.View())
	s.WriteString("\n")

	return s.String()
}
