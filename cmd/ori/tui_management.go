package main

import (
	"context"
	"fmt"
	"strings"

	appcore "ori/internal/app"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type managementPanel struct {
	kind        string
	selected    int
	windowStart int
	message     string

	configDraft map[string]string
	editingKey  string
	editValue   string
}

func (m *interactiveModel) openManagementPanel(kind string) {
	panel := &managementPanel{kind: kind, configDraft: map[string]string{}}
	if kind == appcore.UIRequestConfig {
		for _, field := range m.managementConfigFields() {
			panel.configDraft[field.Key] = field.Value
		}
	}
	m.panel = panel
	m.focus = focusOverlay
	m.viewVersion++
}

func (m *interactiveModel) handleManagementPanelKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.panel == nil {
		return false, nil
	}
	if m.panel.editingKey != "" {
		return true, m.handleManagementPanelEditKey(msg)
	}
	switch msg.Type {
	case tea.KeyEsc:
		m.panel = nil
		m.focus = focusInput
		m.viewVersion++
		return true, nil
	case tea.KeyUp:
		m.moveManagementPanelSelection(-1)
		return true, nil
	case tea.KeyDown:
		m.moveManagementPanelSelection(1)
		return true, nil
	case tea.KeySpace:
		return true, m.activateManagementPanelSelection(false)
	case tea.KeyEnter:
		return true, m.activateManagementPanelSelection(true)
	}
	switch strings.ToLower(msg.String()) {
	case "r":
		if m.panel.kind == appcore.UIRequestMCP {
			m.refreshSelectedMCPServer()
			return true, nil
		}
	case "s":
		if m.panel.kind == appcore.UIRequestConfig {
			m.saveConfigPanel()
			return true, nil
		}
	}
	return true, nil
}

func (m *interactiveModel) handleManagementPanelEditKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.panel.editingKey = ""
		m.panel.editValue = ""
		m.focus = focusOverlay
	case tea.KeyEnter:
		m.panel.configDraft[m.panel.editingKey] = m.panel.editValue
		m.panel.editingKey = ""
		m.panel.editValue = ""
	case tea.KeyBackspace, tea.KeyCtrlH:
		runes := []rune(m.panel.editValue)
		if len(runes) > 0 {
			m.panel.editValue = string(runes[:len(runes)-1])
		}
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.panel.editValue += msg.String()
		}
	}
	m.viewVersion++
	return nil
}

func (m *interactiveModel) moveManagementPanelSelection(delta int) {
	total := len(m.managementPanelRows())
	if total == 0 {
		m.panel.selected = 0
		m.panel.windowStart = 0
		m.viewVersion++
		return
	}
	next := m.panel.selected + delta
	if next < 0 {
		next = 0
	}
	if next >= total {
		next = total - 1
	}
	m.panel.selected = next
	m.ensureManagementPanelSelectionVisible(total)
	m.viewVersion++
}

func (m *interactiveModel) ensureManagementPanelSelectionVisible(total int) {
	pageSize := m.managementPanelPageSize()
	if m.panel.selected < m.panel.windowStart {
		m.panel.windowStart = m.panel.selected
	}
	if m.panel.selected >= m.panel.windowStart+pageSize {
		m.panel.windowStart = m.panel.selected - pageSize + 1
	}
	maxStart := max(0, total-pageSize)
	if m.panel.windowStart > maxStart {
		m.panel.windowStart = maxStart
	}
	if m.panel.windowStart < 0 {
		m.panel.windowStart = 0
	}
}

func (m *interactiveModel) activateManagementPanelSelection(enter bool) tea.Cmd {
	switch m.panel.kind {
	case appcore.UIRequestMCP:
		if enter {
			m.refreshSelectedMCPServer()
		} else {
			m.toggleSelectedMCPServer()
		}
	case appcore.UIRequestSkills:
		m.toggleSelectedSkill()
	case appcore.UIRequestConfig:
		m.activateSelectedConfigField()
	case appcore.UIRequestSessions:
		if enter {
			return m.resumeSelectedSession()
		}
	}
	m.viewVersion++
	return nil
}

func (m *interactiveModel) toggleSelectedMCPServer() {
	rows := m.managementMCPServers()
	if len(rows) == 0 || m.panel.selected >= len(rows) {
		return
	}
	mgmt := m.management()
	if mgmt == nil {
		m.panel.message = "MCP manager is not available"
		return
	}
	msg, err := mgmt.ToggleMCPServer(context.Background(), rows[m.panel.selected].Name)
	m.setPanelMessage(msg, err)
}

func (m *interactiveModel) refreshSelectedMCPServer() {
	rows := m.managementMCPServers()
	if len(rows) == 0 || m.panel.selected >= len(rows) {
		return
	}
	mgmt := m.management()
	if mgmt == nil {
		m.panel.message = "MCP manager is not available"
		return
	}
	msg, err := mgmt.RefreshMCPServer(context.Background(), rows[m.panel.selected].Name)
	m.setPanelMessage(msg, err)
}

func (m *interactiveModel) toggleSelectedSkill() {
	rows := m.managementSkills()
	if len(rows) == 0 || m.panel.selected >= len(rows) {
		return
	}
	mgmt := m.management()
	if mgmt == nil {
		m.panel.message = "skill manager is not available"
		return
	}
	msg, err := mgmt.ToggleSkill(rows[m.panel.selected].Name)
	m.setPanelMessage(msg, err)
}

func (m *interactiveModel) activateSelectedConfigField() {
	fields := m.managementConfigFields()
	if len(fields) == 0 || m.panel.selected >= len(fields) {
		return
	}
	field := fields[m.panel.selected]
	current := m.panel.configDraft[field.Key]
	if field.Kind == "bool" {
		if strings.EqualFold(current, "true") {
			m.panel.configDraft[field.Key] = "false"
		} else {
			m.panel.configDraft[field.Key] = "true"
		}
		return
	}
	m.panel.editingKey = field.Key
	m.panel.editValue = current
}

func (m *interactiveModel) saveConfigPanel() {
	mgmt := m.management()
	if mgmt == nil {
		m.panel.message = "config manager is not available"
		return
	}
	restart, err := mgmt.SaveConfigFields(m.panel.configDraft)
	if err != nil {
		m.panel.message = "error: " + err.Error()
		return
	}
	if restart {
		m.panel.message = "saved; restart required for some changes"
	} else {
		m.panel.message = "saved"
	}
}

func (m *interactiveModel) setPanelMessage(msg string, err error) {
	if err != nil {
		m.panel.message = "error: " + err.Error()
		return
	}
	m.panel.message = msg
}

func (m *interactiveModel) renderManagementPanel() string {
	if m.panel == nil {
		return ""
	}
	rows := m.managementPanelRows()
	total := len(rows)
	m.ensureManagementPanelSelectionVisible(total)
	pageSize := m.managementPanelPageSize()
	start := m.panel.windowStart
	end := min(start+pageSize, total)
	width := getTerminalWidth()

	var lines []string
	lines = append(lines, managementPanelTitle(m.panel.kind))
	if total > 0 {
		lines = append(lines, completionCountLine(start, end, total, width))
	} else {
		lines = append(lines, "  "+toolArgsStyle.Render("0 of 0"))
	}
	for i, row := range rows[start:end] {
		line := row
		if lipgloss.Width(line) > width-4 {
			line = truncateCommandSuggestion(line, width-4)
		}
		if start+i == m.panel.selected {
			lines = append(lines, slashCommandSelectedStyle.Render("> ")+line)
			continue
		}
		lines = append(lines, "  "+line)
	}
	if total == 0 {
		lines = append(lines, "  "+toolArgsStyle.Render("(none)"))
	}
	if m.panel.editingKey != "" {
		lines = append(lines, "  "+slashCommandSelectedStyle.Render("edit "+m.panel.editingKey+": "+m.panel.editValue))
	}
	if m.panel.message != "" {
		lines = append(lines, "  "+toolRunningStyle.Render(m.panel.message))
	}
	lines = append(lines, "  "+toolArgsStyle.Render(managementPanelHelp(m.panel.kind)))
	return strings.Join(lines, "\n") + "\n"
}

func managementPanelTitle(kind string) string {
	switch kind {
	case appcore.UIRequestMCP:
		return slashCommandSelectedStyle.Render("MCP servers")
	case appcore.UIRequestSkills:
		return slashCommandSelectedStyle.Render("Skills")
	case appcore.UIRequestConfig:
		return slashCommandSelectedStyle.Render("Config")
	case appcore.UIRequestSessions:
		return slashCommandSelectedStyle.Render("Sessions")
	default:
		return slashCommandSelectedStyle.Render("Management")
	}
}

func managementPanelHelp(kind string) string {
	switch kind {
	case appcore.UIRequestMCP:
		return "↑/↓ select · Space enable/disable · Enter refresh · r refresh · Esc close"
	case appcore.UIRequestSkills:
		return "↑/↓ select · Space toggle · Enter toggle · Esc close"
	case appcore.UIRequestConfig:
		return "↑/↓ select · Space toggle bool · Enter edit · s save · Esc close"
	case appcore.UIRequestSessions:
		return "↑/↓ select · Enter resume · Esc close"
	default:
		return "Esc close"
	}
}

func (m *interactiveModel) managementPanelRows() []string {
	switch m.panel.kind {
	case appcore.UIRequestMCP:
		servers := m.managementMCPServers()
		rows := make([]string, 0, len(servers))
		for _, item := range servers {
			enabled := managementDisabledStyle.Render("disabled")
			if item.Enabled {
				enabled = managementEnabledStyle.Render("enabled")
			}
			state := "disconnected"
			if item.Connected {
				state = "connected"
			}
			row := fmt.Sprintf("%s  %s  %s  %s  %s",
				slashCommandSelectedStyle.Render(item.Name),
				enabled,
				toolArgsStyle.Render(state),
				toolArgsStyle.Render("lifecycle="+item.Lifecycle),
				toolArgsStyle.Render(fmt.Sprintf("tools=%d resources=%d prompts=%d", item.Tools, item.Resources, item.Prompts)))
			if item.LastError != "" {
				row += "  " + toolErrorStyle.Render("error="+item.LastError)
			}
			rows = append(rows, row)
		}
		return rows
	case appcore.UIRequestSkills:
		items := m.managementSkills()
		rows := make([]string, 0, len(items))
		for _, item := range items {
			enabled := managementDisabledStyle.Render("disabled")
			if item.Enabled {
				enabled = managementEnabledStyle.Render("enabled")
			}
			available := "available"
			if !item.Available {
				available = "unavailable"
			}
			desc := strings.Join(strings.Fields(item.Description), " ")
			rows = append(rows, fmt.Sprintf("%s  %s  %s  %s  %s",
				slashCommandSelectedStyle.Render(item.Name),
				enabled,
				toolArgsStyle.Render(available),
				toolArgsStyle.Render("["+item.Source+"]"),
				toolArgsStyle.Render(desc)))
		}
		return rows
	case appcore.UIRequestConfig:
		fields := m.managementConfigFields()
		rows := make([]string, 0, len(fields))
		for _, field := range fields {
			value := m.panel.configDraft[field.Key]
			if value == "" {
				value = "(empty)"
			}
			tag := ""
			if field.RestartRequired {
				tag = "  restart"
			}
			rows = append(rows, fmt.Sprintf("%s  %s%s", field.Label, value, tag))
		}
		return rows
	case appcore.UIRequestSessions:
		items := m.managementSessions()
		rows := make([]string, 0, len(items))
		for _, item := range items {
			current := ""
			if item.Current {
				current = toolRunningStyle.Render("current") + "  "
			}
			preview := strings.Join(strings.Fields(item.LastMessagePreview), " ")
			if preview == "" {
				preview = "(no user messages)"
			}
			rows = append(rows, fmt.Sprintf("%s  %s%s  %s  %s",
				slashCommandSelectedStyle.Render(item.Key),
				current,
				toolArgsStyle.Render(fmt.Sprintf("messages=%d", item.MessageCount)),
				toolArgsStyle.Render("updated="+item.UpdatedAt),
				toolArgsStyle.Render(preview)))
		}
		return rows
	default:
		return nil
	}
}

func (m *interactiveModel) managementPanelPageSize() int {
	return max(4, min(10, getTerminalHeight()-8))
}

func (m *interactiveModel) management() *appcore.ManagementService {
	if m.dispatcher == nil {
		return nil
	}
	return m.dispatcher.Management()
}

func (m *interactiveModel) managementMCPServers() []appcore.MCPServerView {
	mgmt := m.management()
	if mgmt == nil {
		return nil
	}
	return mgmt.MCPServers()
}

func (m *interactiveModel) managementSkills() []appcore.SkillView {
	mgmt := m.management()
	if mgmt == nil {
		return nil
	}
	return mgmt.Skills()
}

func (m *interactiveModel) managementConfigFields() []appcore.ConfigFieldView {
	mgmt := m.management()
	if mgmt == nil {
		return nil
	}
	return mgmt.ConfigFields()
}

func (m *interactiveModel) managementSessions() []appcore.SessionView {
	mgmt := m.management()
	if mgmt == nil {
		return nil
	}
	return mgmt.Sessions(m.sessionKey)
}

func (m *interactiveModel) resumeSelectedSession() tea.Cmd {
	items := m.managementSessions()
	if len(items) == 0 || m.panel == nil || m.panel.selected >= len(items) {
		return nil
	}
	item := items[m.panel.selected]
	if item.Key == "" {
		return nil
	}
	if m.dispatcher != nil && m.sessionKey != "" && m.sessionKey != item.Key {
		m.dispatcher.AbortSession(m.sessionKey)
	}
	m.sessionKey = item.Key
	m.chatID = chatIDForSessionKey(item.Key)
	m.subscribeRuntimeEvents(item.Key)
	m.applyClearCommandResult()
	m.panel = nil
	m.status = "ready"
	m.viewVersion++
	messages := m.managementSessionMessages(item.Key)
	return tea.Sequence(clearTerminalHistory(), m.printAbove(renderSessionResumeOutput(item.Key, item, messages)))
}

func chatIDForSessionKey(sessionKey string) string {
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		return sessionKey[idx+1:]
	}
	return sessionKey
}

func (m *interactiveModel) managementSessionMessages(key string) []appcore.SessionMessageView {
	mgmt := m.management()
	if mgmt == nil {
		return nil
	}
	return mgmt.SessionMessages(key)
}

func renderSessionResumeOutput(sessionKey string, item appcore.SessionView, messages []appcore.SessionMessageView) string {
	lines := []string{
		"Resumed session: " + sessionKey,
		fmt.Sprintf("Messages: %d", item.MessageCount),
	}
	if item.UpdatedAt != "" {
		lines = append(lines, "Updated: "+item.UpdatedAt)
	}
	if item.LastMessagePreview != "" {
		lines = append(lines, "Last user: "+item.LastMessagePreview)
	}
	var b strings.Builder
	b.WriteString(renderCommandResultBlock("/sessions", &appcore.CommandResult{Text: strings.Join(lines, "\n")}))
	if len(messages) == 0 {
		return b.String()
	}
	b.WriteString("\n")
	b.WriteString(borderStyle.Render(strings.Repeat("─", getTerminalWidth())))
	b.WriteString("\n")
	b.WriteString(slashCommandSelectedStyle.Render("History"))
	if history := renderSessionHistory(messages); history != "" {
		b.WriteString("\n\n")
		b.WriteString(history)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderSessionHistory(messages []appcore.SessionMessageView) string {
	toolResults := map[string]appcore.SessionMessageView{}
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			toolResults[msg.ToolCallID] = msg
		}
	}

	var b strings.Builder
	var user string
	var assistants []appcore.SessionMessageView
	var standalone []appcore.SessionMessageView
	flushTurn := func() {
		if user == "" && len(assistants) == 0 && len(standalone) == 0 {
			return
		}
		rendered := renderSessionTurn(user, assistants, standalone, toolResults)
		if rendered != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(rendered)
		}
		user = ""
		assistants = nil
		standalone = nil
	}

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			flushTurn()
			user = msg.Content
		case "assistant":
			assistants = append(assistants, msg)
		case "tool":
			if msg.ToolCallID == "" {
				standalone = append(standalone, msg)
			}
		default:
			if msg.Content != "" {
				standalone = append(standalone, msg)
			}
		}
	}
	flushTurn()
	return strings.TrimRight(b.String(), "\n")
}

func renderSessionTurn(user string, assistants []appcore.SessionMessageView, standalone []appcore.SessionMessageView, toolResults map[string]appcore.SessionMessageView) string {
	var b strings.Builder
	if rendered := renderSessionUserMessage(user); rendered != "" {
		b.WriteString(rendered)
	}
	if rendered := renderSessionAssistantMessages(assistants, toolResults); rendered != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(rendered)
	}
	for _, msg := range standalone {
		rendered := ""
		if msg.Role == "tool" {
			rendered = renderSessionToolMessage(msg)
		} else if msg.Content != "" {
			rendered = toolArgsStyle.Render(msg.Role) + "\n" + msg.Content
		}
		if rendered == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(rendered)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderSessionUserMessage(content string) string {
	if content == "" {
		return ""
	}
	var b strings.Builder
	for i, line := range strings.Split(content, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		padded := line + strings.Repeat(" ", max(0, getTerminalWidth()-lipgloss.Width(line)))
		b.WriteString(userMessageStyle.Render(padded))
	}
	return b.String()
}

func renderSessionAssistantMessages(messages []appcore.SessionMessageView, toolResults map[string]appcore.SessionMessageView) string {
	var rounds []thinkingRound
	var content []string
	finalReasoning := ""
	for _, msg := range messages {
		round := sessionThinkingRound(msg, toolResults)
		if round.reasoning != "" || len(round.toolCalls) > 0 {
			rounds = append(rounds, round)
		}
		if strings.TrimSpace(msg.Content) != "" {
			content = append(content, msg.Content)
		}
		if strings.TrimSpace(msg.Reasoning) != "" {
			finalReasoning = msg.Reasoning
		}
	}
	if len(rounds) == 0 && len(content) == 0 {
		return ""
	}
	output := renderAssistantHeader()
	output += formatAssistantMessage(rounds, strings.Join(content, "\n\n"), finalReasoning)
	return strings.TrimRight(output, "\n")
}

func sessionThinkingRound(msg appcore.SessionMessageView, toolResults map[string]appcore.SessionMessageView) thinkingRound {
	round := thinkingRound{reasoning: msg.Reasoning}
	for _, tc := range msg.ToolCalls {
		name := tc.Name
		if name == "" {
			name = tc.ID
		}
		entry := toolCallEntry{
			id:         tc.ID,
			name:       name,
			args:       tc.Arguments,
			argsMap:    tc.ArgumentsMap,
			status:     "done",
			durationMs: 0,
		}
		if result, ok := toolResults[tc.ID]; ok {
			entry.result = result.Content
			if entry.name == "" {
				entry.name = result.Name
			}
		}
		round.toolCalls = append(round.toolCalls, entry)
	}
	return round
}

func renderSessionToolMessage(msg appcore.SessionMessageView) string {
	label := "tool"
	if msg.Name != "" {
		label += ": " + msg.Name
	}
	if msg.ToolCallID != "" {
		label += " (" + msg.ToolCallID + ")"
	}
	if msg.Content == "" {
		return toolEntryStyle.Render(label)
	}
	return toolEntryStyle.Render(label) + "\n" + msg.Content
}
