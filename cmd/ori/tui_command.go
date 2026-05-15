package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	appcore "ori/internal/app"
	"ori/internal/bus"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const slashCommandSuggestionPageSize = 6

func (m *interactiveModel) handleSlashCommand(input string) (bool, tea.Cmd) {
	name := slashCommandName(input)
	switch name {
	case "quit", "exit":
		m.quitting = true
		m.shutdown()
		return true, tea.Quit
	}
	if m.dispatcher == nil {
		return false, nil
	}
	result, handled := m.dispatcher.ExecuteCommand(context.Background(), input, bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     m.chatID,
		Content:    input,
		SessionKey: m.sessionKey,
	})
	if !handled {
		return false, nil
	}
	if result == nil {
		return true, nil
	}
	return true, m.applySlashCommandResult(input, result)
}

func (m *interactiveModel) applySlashCommandResult(input string, result *appcore.CommandResult) tea.Cmd {
	if result == nil {
		return nil
	}
	if result.PromptReplacement != "" {
		return m.submitPrompt(input, result.PromptReplacement)
	}
	if result.ResetSession || result.ClearViewport {
		m.applyClearCommandResult()
	}
	m.appendCommandResult(input, result)
	if result.UIRequest != "" {
		m.openManagementPanel(result.UIRequest)
	}
	if result.Status != "" {
		m.status = result.Status
	}
	m.refreshTranscriptViewport()
	m.viewVersion++
	return nil
}

func (m *interactiveModel) submitPrompt(displayContent, dispatchContent string) tea.Cmd {
	m.mu.Lock()
	m.beginPromptForTranscript(displayContent)
	m.mu.Unlock()

	m.dispatcher.Bus().PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     m.chatID,
		Content:    dispatchContent,
		SessionKey: m.sessionKey,
	})

	return m.tickSpinner()
}

func (m *interactiveModel) beginPromptForTranscript(displayContent string) {
	m.beginTranscriptPrompt(displayContent, time.Now())
	m.spinnerIdx = 0
	m.refreshTranscriptViewport()
	m.viewVersion++
}

func (m *interactiveModel) applyClearCommandResult() {
	m.active = false
	m.waiting = false
	m.responseReceived = false
	m.spinnerIdx = 0
	m.status = "ready"
	m.viewVersion++
}

func (m *interactiveModel) acceptSlashCommandCompletion() bool {
	value := strings.TrimSpace(m.textInput.Value())
	matches := m.slashCommandCompletions(value)
	if len(matches) == 0 {
		return false
	}
	m.syncSlashCompletionSelection(value, len(matches))
	idx := m.slashCompletionSelected
	if idx >= len(matches) {
		idx = 0
	}
	m.textInput.SetValue("/" + matches[idx].Name + " ")
	m.textInput.CursorEnd()
	m.slashCompletionQuery = ""
	m.slashCompletionSelected = 0
	m.slashCompletionWindowStart = 0
	m.viewVersion++
	return true
}

func (m *interactiveModel) slashCommandCompletions(value string) []appcore.Command {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		return nil
	}
	prefix := strings.TrimPrefix(value, "/")
	commands := m.availableSlashCommands()
	out := make([]appcore.Command, 0, len(commands))
	for _, cmd := range commands {
		if strings.HasPrefix(cmd.Name, prefix) {
			out = append(out, cmd)
		}
	}
	return out
}

func (m *interactiveModel) moveSlashCommandSelection(delta int) bool {
	value := strings.TrimSpace(m.textInput.Value())
	matches := m.slashCommandCompletions(value)
	if len(matches) == 0 {
		return false
	}
	m.syncSlashCompletionSelection(value, len(matches))
	next := m.slashCompletionSelected + delta
	if next < 0 {
		next = 0
	}
	if next >= len(matches) {
		next = len(matches) - 1
	}
	if next == m.slashCompletionSelected {
		return true
	}
	m.slashCompletionSelected = next
	m.ensureSlashCompletionSelectionVisible(len(matches))
	m.viewVersion++
	return true
}

func (m *interactiveModel) syncSlashCompletionSelection(value string, total int) {
	query := strings.TrimSpace(value)
	if query != m.slashCompletionQuery {
		m.slashCompletionQuery = query
		m.slashCompletionSelected = 0
		m.slashCompletionWindowStart = 0
	}
	if total <= 0 {
		m.slashCompletionSelected = 0
		m.slashCompletionWindowStart = 0
		return
	}
	if m.slashCompletionSelected >= total {
		m.slashCompletionSelected = total - 1
	}
	if m.slashCompletionSelected < 0 {
		m.slashCompletionSelected = 0
	}
	m.ensureSlashCompletionSelectionVisible(total)
}

func (m *interactiveModel) ensureSlashCompletionSelectionVisible(total int) {
	if total <= 0 {
		m.slashCompletionWindowStart = 0
		return
	}
	if m.slashCompletionSelected < m.slashCompletionWindowStart {
		m.slashCompletionWindowStart = m.slashCompletionSelected
	}
	if m.slashCompletionSelected >= m.slashCompletionWindowStart+slashCommandSuggestionPageSize {
		m.slashCompletionWindowStart = m.slashCompletionSelected - slashCommandSuggestionPageSize + 1
	}
	maxStart := max(0, total-slashCommandSuggestionPageSize)
	if m.slashCompletionWindowStart > maxStart {
		m.slashCompletionWindowStart = maxStart
	}
	if m.slashCompletionWindowStart < 0 {
		m.slashCompletionWindowStart = 0
	}
}

func (m *interactiveModel) availableSlashCommands() []appcore.Command {
	if m.dispatcher != nil {
		return m.dispatcher.ListCommands()
	}
	return []appcore.Command{
		{Name: "clear", Description: "Clear the current conversation"},
		{Name: "help", Description: "Show available commands"},
		{Name: "new", Description: "Start a new conversation"},
		{Name: "quit", Description: "Quit interactive mode"},
		{Name: "reasoning", Description: "Toggle thinking mode", ArgumentHint: "on|off"},
		{Name: "skills", Description: "List available skills"},
		{Name: "status", Description: "Show bot status"},
		{Name: "stop", Description: "Stop the current task"},
	}
}

func (m *interactiveModel) shouldCompleteSlashCommandOnEnter(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		return false
	}
	name := strings.TrimPrefix(value, "/")
	for _, cmd := range m.availableSlashCommands() {
		if cmd.Name == name {
			return false
		}
	}
	return len(m.slashCommandCompletions(value)) > 0
}

func (m *interactiveModel) renderSlashCommandSuggestions() string {
	value := m.textInput.Value()
	matches := m.slashCommandCompletions(value)
	if len(matches) == 0 {
		return ""
	}
	m.syncSlashCompletionSelection(value, len(matches))
	start := m.slashCompletionWindowStart
	end := min(start+slashCommandSuggestionPageSize, len(matches))
	pageMatches := matches[start:end]
	width := getTerminalWidth()
	lines := []string{completionCountLine(start, end, len(matches), width)}
	for i, cmd := range pageMatches {
		name := "/" + cmd.Name
		if cmd.ArgumentHint != "" {
			name += " " + cmd.ArgumentHint
		}
		desc := cmd.Description
		line := name
		if desc != "" {
			line += "  " + desc
		}
		if lipgloss.Width(line) > width-2 {
			line = truncateCommandSuggestion(line, width-2)
		}
		style := toolArgsStyle
		if start+i == m.slashCompletionSelected {
			style = slashCommandSelectedStyle
		}
		lines = append(lines, "  "+style.Render(line))
	}
	return strings.Join(lines, "\n") + "\n"
}

func completionCountLine(start, end, total, width int) string {
	line := fmt.Sprintf("%d-%d of %d", start+1, end, total)
	if total > slashCommandSuggestionPageSize {
		line += " · ↑/↓"
	}
	if width > 2 && lipgloss.Width(line) > width-2 {
		line = truncateCommandSuggestion(line, width-2)
	}
	return "  " + toolArgsStyle.Render(line)
}

func slashCommandName(input string) string {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return ""
	}
	input = strings.TrimPrefix(input, "/")
	if idx := strings.IndexByte(input, ' '); idx >= 0 {
		input = input[:idx]
	}
	return strings.ToLower(input)
}

func truncateCommandSuggestion(s string, maxWidth int) string {
	if maxWidth <= 1 || lipgloss.Width(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
