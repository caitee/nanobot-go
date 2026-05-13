package main

import (
	"context"
	"fmt"
	"strings"

	appcore "ori/internal/app"
	"ori/internal/bus"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	if result.PromptReplacement != "" {
		return true, m.submitPrompt(input, result.PromptReplacement)
	}
	var cmds []tea.Cmd
	if result.ResetSession || result.ClearViewport {
		m.applyClearCommandResult()
		cmds = append(cmds, clearTerminalHistory())
	}
	if output := renderedCommandOutput(m.banner, input, result); output != "" {
		cmds = append(cmds, m.printAbove(output))
	}
	if result.Status != "" {
		m.status = result.Status
	}
	if len(cmds) == 0 {
		return true, nil
	}
	return true, tea.Sequence(cmds...)
}

func (m *interactiveModel) submitPrompt(displayContent, dispatchContent string) tea.Cmd {
	m.mu.Lock()
	m.active = true
	m.waiting = true
	m.responseReceived = false
	m.spinnerIdx = 0
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
	m.flushedText = ""
	m.status = "waiting"
	m.viewVersion++
	m.mu.Unlock()

	padded := displayContent + strings.Repeat(" ", max(0, getTerminalWidth()-lipgloss.Width(displayContent)))
	userMsg := "\n" + userMessageStyle.Render(padded)
	printCmd := m.printAbove(userMsg)

	m.dispatcher.Bus().PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     m.chatID,
		Content:    dispatchContent,
		SessionKey: m.sessionKey,
	})

	return tea.Batch(printCmd, m.tickSpinner())
}

func (m *interactiveModel) applyClearCommandResult() {
	m.clearActiveState()
	m.active = false
	m.waiting = false
	m.responseReceived = false
	m.spinnerIdx = 0
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
	m.flushedText = ""
	m.status = "ready"
	m.viewVersion++
}

func (m *interactiveModel) acceptSlashCommandCompletion() bool {
	value := strings.TrimSpace(m.textInput.Value())
	matches := m.slashCommandCompletions(value)
	if len(matches) == 0 {
		return false
	}
	m.textInput.SetValue("/" + matches[0].Name + " ")
	m.textInput.CursorEnd()
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
	matches := m.slashCommandCompletions(m.textInput.Value())
	if len(matches) == 0 {
		return ""
	}
	if len(matches) > 6 {
		matches = matches[:6]
	}
	width := getTerminalWidth()
	var lines []string
	for i, cmd := range matches {
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
		if i == 0 {
			style = toolEntryStyle
		}
		lines = append(lines, "  "+style.Render(line))
	}
	return strings.Join(lines, "\n") + "\n"
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

func commandResultOutput(result *appcore.CommandResult) string {
	if result == nil {
		return ""
	}
	if result.Markdown != "" {
		return renderMarkdown(result.Markdown)
	}
	if result.Text != "" {
		return result.Text
	}
	return ""
}

func renderCommandResultBlock(command string, result *appcore.CommandResult) string {
	output := commandResultOutput(result)
	if output == "" {
		return ""
	}
	var b strings.Builder
	if command != "" {
		padded := command + strings.Repeat(" ", max(0, getTerminalWidth()-lipgloss.Width(command)))
		b.WriteString("\n")
		b.WriteString(userMessageStyle.Render(padded))
		b.WriteString("\n")
		b.WriteString(borderStyle.Render(strings.Repeat("─", getTerminalWidth())))
		b.WriteString("\n")
	}
	b.WriteString(output)
	return strings.TrimRight(b.String(), "\n")
}

func renderedCommandOutput(banner, command string, result *appcore.CommandResult) string {
	if result != nil && (result.ResetSession || result.ClearViewport) {
		return renderResetCommandOutput(banner, command, result)
	}
	return renderCommandResultBlock(command, result)
}

func renderResetCommandOutput(banner, command string, result *appcore.CommandResult) string {
	block := renderCommandResultBlock(command, result)
	if banner == "" {
		return block
	}
	if block == "" {
		return strings.TrimRight(banner, "\n")
	}
	return strings.TrimRight(banner, "\n") + "\n" + block
}

func clearTerminalHistory() tea.Cmd {
	return func() tea.Msg {
		fmt.Print("\x1b[2J\x1b[H\x1b[3J")
		return nil
	}
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
