package main

import (
	"fmt"
	"strings"
	"time"

	appcore "ori/internal/app"
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
		mutated := false
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
			mutated = true
		}
		// Throttled flush: the glamour-rendered length check is O(N) in the
		// displayed text, so doing it on every delta thrashes the CPU during
		// long streams. Once per flushWindowCheckInterval is enough — flushing
		// is only a latency optimisation, not a correctness one.
		flushCmd := m.maybeFlushStreamWindowThrottled()
		if flushCmd != nil {
			mutated = true
		}
		if mutated {
			m.viewVersion++
		}
		m.mu.Unlock()
		var next tea.Cmd
		if len(m.typewriterQueue) > 0 {
			next = m.tickTypewriter()
		}
		if flushCmd == nil {
			return m, next
		}
		if next == nil {
			return m, flushCmd
		}
		return m, tea.Batch(flushCmd, next)

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
		m.viewVersion++
		m.mu.Unlock()
		return m, cmd

	case runtimeEventMsg:
		m.mu.Lock()
		cmd := m.handleRuntimeEvent(msg.ev)
		m.viewVersion++
		m.mu.Unlock()
		return m, cmd

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD:
			if m.active && (m.streamText != "" || (m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0))) {
				output := m.formatCurrentState()
				cmd := m.printAbove(output)
				m.clearActiveState()
				m.active = false
				m.waiting = false
				m.quitting = true
				m.shutdown()
				m.viewVersion++
				return m, tea.Batch(cmd, tea.Quit)
			}
			m.quitting = true
			m.shutdown()
			m.viewVersion++
			return m, tea.Quit
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

// handleRuntimeEvent processes a single runtime.Event. Called with m.mu held.
// Returns a Cmd for the final message path; nil otherwise.
func (m *interactiveModel) handleRuntimeEvent(ev runtime.Event) tea.Cmd {
	if !m.active {
		return nil
	}
	switch ev.Kind {
	case runtime.EventAgentStart:
		m.status = "thinking"
		// Flush the "✦ ori" banner once at the top of the response so it
		// sits above the first round of reasoning/tool calls instead of
		// appearing between the last round and the final message.
		return m.printAbove(renderAssistantHeader())

	case runtime.EventTurnStart:
		m.status = "thinking"
		// New turn → flush the previous round (if non-empty) to the history
		// area above the TUI, so the user sees it persist as the next round
		// starts. View() only renders currentRound, so without this flush the
		// finished round would vanish until finalizeAssistantMessage runs.
		var flushCmd tea.Cmd
		if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
			flushCmd = m.printAbove(m.renderCompletedRound(*m.currentRound))
		}
		m.currentRound = &thinkingRound{}
		m.streamText = ""
		m.displayedText = ""
		m.typewriterQueue = nil
		return flushCmd

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
			// The stream-window flush check now piggybacks on the typewriter
			// tick (throttled). Running it on every delta was the main source
			// of spinner stutter on long streams — glamour rendering the full
			// displayed text + lipgloss width calc is linear per delta, which
			// adds up to quadratic over a 10k-character response.
			return nil
		}

	case runtime.EventToolExecutionStart:
		m.status = "running tools"
		data, ok := ev.ToolStart()
		if !ok {
			return nil
		}
		if m.currentRound == nil {
			m.currentRound = &thinkingRound{}
		}
		args := formatArgs(data.Args)
		entry := toolCallEntry{
			id:        data.ToolCallID,
			name:      data.ToolName,
			args:      args,
			argsMap:   cloneToolArgs(data.Args),
			status:    "running",
			startTime: ev.Timestamp,
		}
		entry.displayArgs.set(args)
		m.currentRound.toolCalls = append(m.currentRound.toolCalls, entry)

	case runtime.EventToolExecUpdate:
		data, ok := ev.ToolUpdate()
		if !ok {
			return nil
		}
		if idx := m.findToolCall(data.ToolCallID, data.ToolName); idx >= 0 {
			partial := contentsToString(data.Partial)
			m.currentRound.toolCalls[idx].partial = partial
			m.currentRound.toolCalls[idx].displayPartial.set(partial)
			m.currentRound.toolCalls[idx].lastUpdate = ev.Timestamp
		}

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
			resultStr := contentsToString(data.Result)
			m.currentRound.toolCalls[idx].result = resultStr
			m.currentRound.toolCalls[idx].displayResult.set(resultStr)
			if !m.currentRound.toolCalls[idx].startTime.IsZero() {
				m.currentRound.toolCalls[idx].durationMs = ev.Timestamp.Sub(m.currentRound.toolCalls[idx].startTime).Milliseconds()
			}
		}
		// All tools in this round have settled — fall back to "thinking" so the
		// status line doesn't keep showing "using tools" in the gap before the
		// next delta/event arrives.
		if m.currentRound != nil && !hasRunningToolCall(m.currentRound.toolCalls) {
			m.status = "thinking"
		}

	case runtime.EventAgentEnd:
		// Finalize in this turn. The outbound message arrives shortly after
		// with the same content (published by the dispatcher), but we use
		// the agent_end payload directly — it's the authoritative record.
		data, _ := ev.AgentEnd()
		text, reasoning := appcore.ExtractFinalAssistant(data.Messages)
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

func hasRunningToolCall(calls []toolCallEntry) bool {
	for i := range calls {
		if calls[i].status == "running" {
			return true
		}
	}
	return false
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
	if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
		allRounds = append(allRounds, *m.currentRound)
	}
	return formatAssistantMessage(allRounds, m.displayedText, "")
}

func (m *interactiveModel) formatFinalMessage(content, reasoning string) string {
	// Previous rounds are already flushed above the TUI on each TurnStart, so
	// we only need to render the last round (if any) plus the final content.
	content = m.unflushedFinalContent(content)
	var allRounds []thinkingRound
	if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
		allRounds = append(allRounds, *m.currentRound)
	}
	if reasoning != "" && !roundsContainReasoning(allRounds, reasoning) {
		if len(allRounds) == 0 || allRounds[len(allRounds)-1].reasoning != reasoning {
			allRounds = append(allRounds, thinkingRound{reasoning: reasoning})
		}
	}
	return formatAssistantMessage(allRounds, content, reasoning)
}

func roundsContainReasoning(rounds []thinkingRound, reasoning string) bool {
	for _, round := range rounds {
		if round.reasoning == reasoning {
			return true
		}
	}
	return false
}

func (m *interactiveModel) unflushedFinalContent(content string) string {
	if m.flushedText == "" || content == "" {
		return content
	}
	if strings.HasPrefix(content, m.flushedText) {
		return strings.TrimPrefix(content, m.flushedText)
	}
	return content
}

func (m *interactiveModel) clearActiveState() {
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
	m.flushedText = ""
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

// maybeFlushStreamWindowThrottled is a cheap O(1) gate in front of
// maybeFlushStreamWindow. Flushing is only a latency optimisation (keeping
// the live window short); running the full glamour render on every tick
// just to count lines dominates the CPU budget on long streams.
//
// Skip rules:
//   - empty displayed text → nothing to flush.
//   - rune length is below the smallest possible "crosses threshold" size
//     (terminal columns × half-height). Even with perfect wrapping the
//     rendered output can't exceed threshold rows, so no flush is possible.
//
// Only when those fast paths miss do we pay for glamour. Caller must hold m.mu.
func (m *interactiveModel) maybeFlushStreamWindowThrottled() tea.Cmd {
	if m.displayedText == "" {
		return nil
	}
	threshold := flushLineThreshold()
	width := getTerminalWidth()
	if width <= 0 {
		width = 80
	}
	// Minimum rune count that could plausibly render to `threshold` lines.
	// Real rendering usually wraps harder than this lower bound — but we
	// only want to gate on a bound that is cheap AND guaranteed safe.
	minRunes := threshold * width / 2
	if len(m.displayedText) < minRunes && strings.Count(m.displayedText, "\n") < threshold {
		return nil
	}
	return m.maybeFlushStreamWindow()
}

// flushLineThreshold is the rendered-line count above which we flush a stable
// prefix out of the live window. Sized as half the terminal height, floored
// at 10 so a tiny terminal still gets a sensible window.
func flushLineThreshold() int {
	threshold := getTerminalHeight() / 2
	if threshold < 10 {
		return 10
	}
	return threshold
}

// maybeFlushStreamWindow checks if displayedText is too long and flushes
// the stable prefix to View above, keeping only the tail window.
func (m *interactiveModel) maybeFlushStreamWindow() tea.Cmd {
	if m.displayedText == "" {
		return nil
	}
	if m.currentRound != nil && (m.currentRound.reasoning != "" || len(m.currentRound.toolCalls) > 0) {
		return nil
	}

	// Calculate how many lines displayedText would render to
	renderer := getMarkdownRenderer()
	if renderer == nil {
		return nil
	}
	processed := preprocessMath(closeOpenMarkdown(m.displayedText))
	rendered, err := renderer.Render(processed)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")

	threshold := flushLineThreshold()
	if len(lines) <= threshold {
		return nil
	}

	// Find a safe cut point: look for paragraph breaks (empty lines) in the first half
	// of displayedText to avoid cutting mid-sentence or mid-markdown-block.
	textLines := strings.Split(m.displayedText, "\n")
	cutIndex := -1
	for i := 0; i < len(textLines)/2; i++ {
		if strings.TrimSpace(textLines[i]) == "" && i > 0 {
			cutIndex = i
		}
	}

	// If no good cut point found, don't flush (wait for more content)
	if cutIndex <= 0 {
		return nil
	}

	// Flush the prefix
	prefix := strings.Join(textLines[:cutIndex], "\n")
	renderedPrefix := m.renderLiveContent(prefix)
	flushCmd := m.printAbove(renderedPrefix)
	m.rememberFlushedText(prefix)

	// Keep the suffix in displayedText
	m.displayedText = strings.Join(textLines[cutIndex:], "\n")

	return flushCmd
}

func (m *interactiveModel) rememberFlushedText(prefix string) {
	if prefix == "" {
		return
	}
	if m.flushedText == "" {
		m.flushedText = prefix
		return
	}
	m.flushedText += "\n" + prefix
}

// renderCompletedRound renders a single completed round (reasoning + tool calls)
// for flushing to View above. Similar to renderRound but without live state.
func (m *interactiveModel) renderCompletedRound(round thinkingRound) string {
	return renderRoundContent(round, false)
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
