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
		m.viewVersion++
		return true, nil
	case tea.KeyUp:
		m.moveManagementPanelSelection(-1)
		return true, nil
	case tea.KeyDown:
		m.moveManagementPanelSelection(1)
		return true, nil
	case tea.KeySpace:
		m.activateManagementPanelSelection(false)
		return true, nil
	case tea.KeyEnter:
		m.activateManagementPanelSelection(true)
		return true, nil
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

func (m *interactiveModel) activateManagementPanelSelection(enter bool) {
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
	}
	m.viewVersion++
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
		style := toolArgsStyle
		prefix := "  "
		if start+i == m.panel.selected {
			style = slashCommandSelectedStyle
			prefix = "> "
		}
		lines = append(lines, prefix+style.Render(line))
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
			enabled := "disabled"
			if item.Enabled {
				enabled = "enabled"
			}
			state := "disconnected"
			if item.Connected {
				state = "connected"
			}
			row := fmt.Sprintf("%s  %s  %s  lifecycle=%s  tools=%d resources=%d prompts=%d",
				item.Name, enabled, state, item.Lifecycle, item.Tools, item.Resources, item.Prompts)
			if item.LastError != "" {
				row += "  error=" + item.LastError
			}
			rows = append(rows, row)
		}
		return rows
	case appcore.UIRequestSkills:
		items := m.managementSkills()
		rows := make([]string, 0, len(items))
		for _, item := range items {
			enabled := "disabled"
			if item.Enabled {
				enabled = "enabled"
			}
			available := "available"
			if !item.Available {
				available = "unavailable"
			}
			desc := strings.Join(strings.Fields(item.Description), " ")
			rows = append(rows, fmt.Sprintf("%s  %s  %s  [%s]  %s", item.Name, enabled, available, item.Source, desc))
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
