package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"nanobot-go/internal/config"
)

// Wizard state
type wizardState int

const (
	stateMainMenu wizardState = iota
	stateProviderMenu
	stateAgentSettings
	stateGatewaySettings
	stateToolsSettings
	stateEditField
	stateViewSummary
)

var (
	// Styles
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("63")).
			Bold(true)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)

	dimmedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("63")).
			Bold(true).
			Underline(true)

	fieldStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226"))
)

type wizardModel struct {
	cfg           config.Config
	state         wizardState
	cursor        int
	fieldCursor   int
	editValue     string
	editFieldName string
	textInput     textinput.Model
	quitting      bool
	saved         bool
}

func newWizardModel() *wizardModel {
	return &wizardModel{
		cfg: config.Config{
			Agents: config.AgentDefaults{
				Model:               "claude-opus-4-5",
				Provider:            "auto",
				MaxTokens:           8192,
				ContextWindowTokens: 65536,
				Temperature:         0.1,
				MaxToolIterations:    40,
			},
			Gateway: config.GatewayConfig{
				Host: "0.0.0.0",
				Port: 18790,
			},
			Tools: config.ToolsConfig{
				Web: config.WebConfig{
					SearchProvider: "brave",
				},
			},
			Providers: config.ProvidersConfig{},
		},
		state:       stateMainMenu,
		cursor:      0,
		fieldCursor: 0,
	}
}

func (m *wizardModel) Init() tea.Cmd {
	return nil
}

func (m *wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *wizardModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateMainMenu:
		return m.handleMainMenu(msg)
	case stateProviderMenu:
		return m.handleProviderMenu(msg)
	case stateAgentSettings:
		return m.handleAgentSettings(msg)
	case stateGatewaySettings:
		return m.handleGatewaySettings(msg)
	case stateToolsSettings:
		return m.handleToolsSettings(msg)
	case stateEditField:
		return m.handleEditField(msg)
	case stateViewSummary:
		return m.handleViewSummary(msg)
	}
	return m, nil
}

func (m *wizardModel) handleMainMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := []string{
		"[P] LLM Provider",
		"[A] Agent Settings",
		"[G] Gateway",
		"[T] Tools",
		"[V] View Configuration",
		"[S] Save and Exit",
		"[X] Exit Without Saving",
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(choices)-1 {
			m.cursor++
		}
	case "enter", " ":
		switch m.cursor {
		case 0:
			m.state = stateProviderMenu
			m.cursor = 0
		case 1:
			m.state = stateAgentSettings
			m.cursor = 0
		case 2:
			m.state = stateGatewaySettings
			m.cursor = 0
		case 3:
			m.state = stateToolsSettings
			m.cursor = 0
		case 4:
			// View configuration summary
			m.state = stateViewSummary
			m.cursor = 0
		case 5:
			m.saved = true
			return m, tea.Quit
		case 6:
			return m, tea.Quit
		}
	case "ctrl+c", "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) handleProviderMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := []string{
		"OpenAI",
		"Azure OpenAI",
		"Anthropic",
		"OpenRouter",
		"MiniMax",
		"<- Back",
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(choices)-1 {
			m.cursor++
		}
	case "enter", " ":
		if m.cursor == 5 {
			m.state = stateMainMenu
			m.cursor = 0
		} else {
			// Enter edit mode for provider API key
			provider := choices[m.cursor]
			m.editFieldName = provider
			m.state = stateEditField
			m.editValue = m.getProviderAPIKey(provider)
			m.textInput = textinput.New()
			m.textInput.Placeholder = "API Key"
			m.textInput.EchoMode = textinput.EchoPassword
			m.textInput.Focus()
		}
	case "esc":
		m.state = stateMainMenu
		m.cursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) handleAgentSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := []string{
		"Model",
		"Temperature",
		"Max Tokens",
		"Context Window Tokens",
		"Max Tool Iterations",
		"<- Back",
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(choices)-1 {
			m.cursor++
		}
	case "enter", " ":
		if m.cursor == len(choices)-1 {
			m.state = stateMainMenu
			m.cursor = 0
		} else {
			m.editFieldName = choices[m.cursor]
			m.state = stateEditField
			m.editValue = m.getAgentField(choices[m.cursor])
			m.textInput = textinput.New()
			m.textInput.Placeholder = choices[m.cursor]
			m.textInput.Focus()
		}
	case "esc":
		m.state = stateMainMenu
		m.cursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) handleGatewaySettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := []string{
		"Host",
		"Port",
		"<- Back",
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(choices)-1 {
			m.cursor++
		}
	case "enter", " ":
		if m.cursor == len(choices)-1 {
			m.state = stateMainMenu
			m.cursor = 0
		} else {
			m.editFieldName = choices[m.cursor]
			m.state = stateEditField
			m.editValue = m.getGatewayField(choices[m.cursor])
			m.textInput = textinput.New()
			m.textInput.Placeholder = choices[m.cursor]
			m.textInput.Focus()
		}
	case "esc":
		m.state = stateMainMenu
		m.cursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) handleToolsSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	choices := []string{
		"Web Search Provider",
		"Web Search API Key",
		"<- Back",
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(choices)-1 {
			m.cursor++
		}
	case "enter", " ":
		if m.cursor == len(choices)-1 {
			m.state = stateMainMenu
			m.cursor = 0
		} else {
			m.editFieldName = choices[m.cursor]
			m.state = stateEditField
			m.editValue = m.getToolsField(choices[m.cursor])
			m.textInput = textinput.New()
			m.textInput.Placeholder = choices[m.cursor]
			if choices[m.cursor] == "Web Search API Key" {
				m.textInput.EchoMode = textinput.EchoPassword
			}
			m.textInput.Focus()
		}
	case "esc":
		m.state = stateMainMenu
		m.cursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m *wizardModel) handleEditField(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Save the value
		m.saveEditValue()
		m.state = m.getPreviousState()
		m.textInput = textinput.Model{}
	case tea.KeyEscape:
		m.state = m.getPreviousState()
		m.textInput = textinput.Model{}
	case tea.KeyCtrlC:
		return m, tea.Quit
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *wizardModel) handleViewSummary(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", " ", "escape", "ctrl+c", "q":
		m.state = stateMainMenu
	}
	return m, nil
}

func (m *wizardModel) getPreviousState() wizardState {
	switch m.editFieldName {
	case "OpenAI", "Azure OpenAI", "Anthropic", "OpenRouter", "MiniMax":
		return stateProviderMenu
	case "Model", "Temperature", "Max Tokens", "Context Window Tokens", "Max Tool Iterations":
		return stateAgentSettings
	case "Host", "Port":
		return stateGatewaySettings
	case "Web Search Provider", "Web Search API Key":
		return stateToolsSettings
	}
	return stateMainMenu
}

func (m *wizardModel) saveEditValue() {
	value := m.textInput.Value()

	switch m.editFieldName {
	case "OpenAI":
		if m.cfg.Providers.OpenAI == nil {
			m.cfg.Providers.OpenAI = make(map[string]any)
		}
		m.cfg.Providers.OpenAI["api_key"] = value
	case "Azure OpenAI":
		if m.cfg.Providers.Azure == nil {
			m.cfg.Providers.Azure = make(map[string]any)
		}
		m.cfg.Providers.Azure["api_key"] = value
	case "Anthropic":
		if m.cfg.Providers.Anthropic == nil {
			m.cfg.Providers.Anthropic = make(map[string]any)
		}
		m.cfg.Providers.Anthropic["api_key"] = value
	case "OpenRouter":
		if m.cfg.Providers.OpenRouter == nil {
			m.cfg.Providers.OpenRouter = make(map[string]any)
		}
		m.cfg.Providers.OpenRouter["api_key"] = value
	case "MiniMax":
		if m.cfg.Providers.Minimax == nil {
			m.cfg.Providers.Minimax = make(map[string]any)
		}
		m.cfg.Providers.Minimax["api_key"] = value
	case "Model":
		m.cfg.Agents.Model = value
	case "Temperature":
		fmt.Sscanf(value, "%f", &m.cfg.Agents.Temperature)
	case "Max Tokens":
		fmt.Sscanf(value, "%d", &m.cfg.Agents.MaxTokens)
	case "Context Window Tokens":
		fmt.Sscanf(value, "%d", &m.cfg.Agents.ContextWindowTokens)
	case "Max Tool Iterations":
		fmt.Sscanf(value, "%d", &m.cfg.Agents.MaxToolIterations)
	case "Host":
		m.cfg.Gateway.Host = value
	case "Port":
		fmt.Sscanf(value, "%d", &m.cfg.Gateway.Port)
	case "Web Search Provider":
		m.cfg.Tools.Web.SearchProvider = value
	case "Web Search API Key":
		m.cfg.Tools.Web.SearchAPIKey = value
	}
}

func (m *wizardModel) getProviderAPIKey(provider string) string {
	var providerMap map[string]any
	switch provider {
	case "OpenAI":
		providerMap = m.cfg.Providers.OpenAI
	case "Azure OpenAI":
		providerMap = m.cfg.Providers.Azure
	case "Anthropic":
		providerMap = m.cfg.Providers.Anthropic
	case "OpenRouter":
		providerMap = m.cfg.Providers.OpenRouter
	case "MiniMax":
		providerMap = m.cfg.Providers.Minimax
	}
	if providerMap == nil {
		return ""
	}
	if v, ok := providerMap["api_key"].(string); ok {
		return v
	}
	return ""
}

func (m *wizardModel) getAgentField(field string) string {
	switch field {
	case "Model":
		return m.cfg.Agents.Model
	case "Temperature":
		return fmt.Sprintf("%f", m.cfg.Agents.Temperature)
	case "Max Tokens":
		return fmt.Sprintf("%d", m.cfg.Agents.MaxTokens)
	case "Context Window Tokens":
		return fmt.Sprintf("%d", m.cfg.Agents.ContextWindowTokens)
	case "Max Tool Iterations":
		return fmt.Sprintf("%d", m.cfg.Agents.MaxToolIterations)
	}
	return ""
}

func (m *wizardModel) getGatewayField(field string) string {
	switch field {
	case "Host":
		return m.cfg.Gateway.Host
	case "Port":
		return fmt.Sprintf("%d", m.cfg.Gateway.Port)
	}
	return ""
}

func (m *wizardModel) getToolsField(field string) string {
	switch field {
	case "Web Search Provider":
		return m.cfg.Tools.Web.SearchProvider
	case "Web Search API Key":
		return m.cfg.Tools.Web.SearchAPIKey
	}
	return ""
}

func (m *wizardModel) viewConfigSummary() string {
	summary := "\n=== Configuration Summary ===\n\n"
	summary += "Agent Settings:\n"
	summary += fmt.Sprintf("  Model: %s\n", m.cfg.Agents.Model)
	summary += fmt.Sprintf("  Provider: %s\n", m.cfg.Agents.Provider)
	summary += fmt.Sprintf("  Temperature: %.1f\n", m.cfg.Agents.Temperature)
	summary += fmt.Sprintf("  Max Tokens: %d\n", m.cfg.Agents.MaxTokens)
	summary += fmt.Sprintf("  Context Window: %d\n", m.cfg.Agents.ContextWindowTokens)
	summary += fmt.Sprintf("  Max Tool Iterations: %d\n\n", m.cfg.Agents.MaxToolIterations)

	summary += "Gateway:\n"
	summary += fmt.Sprintf("  Host: %s\n", m.cfg.Gateway.Host)
	summary += fmt.Sprintf("  Port: %d\n\n", m.cfg.Gateway.Port)

	summary += "Tools:\n"
	summary += fmt.Sprintf("  Web Search Provider: %s\n\n", m.cfg.Tools.Web.SearchProvider)

	summary += "Providers:\n"
	if apiKey := m.getProviderAPIKey("OpenAI"); apiKey != "" {
		summary += "  OpenAI: configured\n"
	} else {
		summary += "  OpenAI: not configured\n"
	}
	if apiKey := m.getProviderAPIKey("OpenRouter"); apiKey != "" {
		summary += "  OpenRouter: configured\n"
	} else {
		summary += "  OpenRouter: not configured\n"
	}
	if apiKey := m.getProviderAPIKey("MiniMax"); apiKey != "" {
		summary += "  MiniMax: configured\n"
	} else {
		summary += "  MiniMax: not configured\n"
	}

	return summary
}

func (m *wizardModel) View() string {
	if m.state == stateEditField {
		return m.viewEditField()
	}

	var s string

	// Nice banner
	s += "\n"
	s += titleStyle.Render("╔══════════════════════════════════════════╗") + "\n"
	s += titleStyle.Render("║     Nanobot Configuration Wizard        ║") + "\n"
	s += titleStyle.Render("╚══════════════════════════════════════════╝") + "\n"
	s += "\n"

	switch m.state {
	case stateMainMenu:
		s += m.viewMainMenu()
	case stateProviderMenu:
		s += m.viewProviderMenu()
	case stateAgentSettings:
		s += m.viewAgentSettings()
	case stateGatewaySettings:
		s += m.viewGatewaySettings()
	case stateToolsSettings:
		s += m.viewToolsSettings()
	case stateViewSummary:
		s += m.viewSummary()
	}

	s += dimmedStyle.Render("\nPress Ctrl+C or Q to exit\n")

	return s
}

func (m *wizardModel) viewMainMenu() string {
	choices := []string{
		"[P] LLM Provider",
		"[A] Agent Settings",
		"[G] Gateway",
		"[T] Tools",
		"[V] View Configuration",
		"[S] Save and Exit",
		"[X] Exit Without Saving",
	}

	var s string
	s += headerStyle.Render("Main Menu") + "\n"
	s += "\n"
	for i, choice := range choices {
		cursor := "  "
		if m.cursor == i {
			cursor = selectedStyle.Render("► ")
			choice = selectedStyle.Render(choice)
		}
		s += fmt.Sprintf("%s%s\n", cursor, choice)
	}
	return s
}

func (m *wizardModel) viewProviderMenu() string {
	choices := []string{
		"OpenAI",
		"Azure OpenAI",
		"Anthropic",
		"OpenRouter",
		"MiniMax",
		"<- Back",
	}

	var s string
	s += headerStyle.Render("LLM Provider Configuration") + "\n"
	s += dimmedStyle.Render("Select a provider to configure API key") + "\n"
	s += "\n"

	for i, choice := range choices {
		cursor := "  "
		if m.cursor == i {
			cursor = selectedStyle.Render("► ")
			choice = selectedStyle.Render(choice)
		}
		s += fmt.Sprintf("%s%s\n", cursor, choice)
	}
	return s
}

func (m *wizardModel) viewAgentSettings() string {
	fields := []struct {
		name   string
		value  string
	}{
		{"Model", m.cfg.Agents.Model},
		{"Temperature", fmt.Sprintf("%f", m.cfg.Agents.Temperature)},
		{"Max Tokens", fmt.Sprintf("%d", m.cfg.Agents.MaxTokens)},
		{"Context Window Tokens", fmt.Sprintf("%d", m.cfg.Agents.ContextWindowTokens)},
		{"Max Tool Iterations", fmt.Sprintf("%d", m.cfg.Agents.MaxToolIterations)},
	}

	var s string
	s += headerStyle.Render("Agent Settings") + "\n"
	s += "\n"

	for i, f := range fields {
		cursor := "  "
		display := fmt.Sprintf("%s: %s", fieldStyle.Render(f.name), valueStyle.Render(f.value))
		if m.cursor == i {
			cursor = selectedStyle.Render("► ")
			display = selectedStyle.Render(f.name + ": ") + valueStyle.Render(f.value)
		}
		s += fmt.Sprintf("%s%s\n", cursor, display)
	}

	cursor := "  "
	backText := "<- Back"
	if m.cursor == len(fields) {
		cursor = selectedStyle.Render("► ")
		backText = selectedStyle.Render(backText)
	}
	s += fmt.Sprintf("%s%s\n", cursor, backText)

	return s
}

func (m *wizardModel) viewGatewaySettings() string {
	fields := []struct {
		name   string
		value  string
	}{
		{"Host", m.cfg.Gateway.Host},
		{"Port", fmt.Sprintf("%d", m.cfg.Gateway.Port)},
	}

	var s string
	s += headerStyle.Render("Gateway Settings") + "\n"
	s += "\n"

	for i, f := range fields {
		cursor := "  "
		display := fmt.Sprintf("%s: %s", fieldStyle.Render(f.name), valueStyle.Render(f.value))
		if m.cursor == i {
			cursor = selectedStyle.Render("► ")
			display = selectedStyle.Render(f.name + ": ") + valueStyle.Render(f.value)
		}
		s += fmt.Sprintf("%s%s\n", cursor, display)
	}

	cursor := "  "
	backText := "<- Back"
	if m.cursor == len(fields) {
		cursor = selectedStyle.Render("► ")
		backText = selectedStyle.Render(backText)
	}
	s += fmt.Sprintf("%s%s\n", cursor, backText)

	return s
}

func (m *wizardModel) viewToolsSettings() string {
	fields := []struct {
		name   string
		value  string
	}{
		{"Web Search Provider", m.cfg.Tools.Web.SearchProvider},
		{"Web Search API Key", maskString(m.cfg.Tools.Web.SearchAPIKey)},
	}

	var s string
	s += headerStyle.Render("Tools Settings") + "\n"
	s += "\n"

	for i, f := range fields {
		cursor := "  "
		display := fmt.Sprintf("%s: %s", fieldStyle.Render(f.name), valueStyle.Render(f.value))
		if m.cursor == i {
			cursor = selectedStyle.Render("► ")
			display = selectedStyle.Render(f.name + ": ") + valueStyle.Render(f.value)
		}
		s += fmt.Sprintf("%s%s\n", cursor, display)
	}

	cursor := "  "
	backText := "<- Back"
	if m.cursor == len(fields) {
		cursor = selectedStyle.Render("► ")
		backText = selectedStyle.Render(backText)
	}
	s += fmt.Sprintf("%s%s\n", cursor, backText)

	return s
}

func (m *wizardModel) viewSummary() string {
	var s string
	s += "\n" + headerStyle.Render("Configuration Summary") + "\n"
	s += "\n"
	s += headerStyle.Render("Agent Settings") + "\n"
	s += fmt.Sprintf("  %s: %s\n", fieldStyle.Render("Model"), valueStyle.Render(m.cfg.Agents.Model))
	s += fmt.Sprintf("  %s: %s\n", fieldStyle.Render("Provider"), valueStyle.Render(m.cfg.Agents.Provider))
	s += fmt.Sprintf("  %s: %.1f\n", fieldStyle.Render("Temperature"), m.cfg.Agents.Temperature)
	s += fmt.Sprintf("  %s: %d\n", fieldStyle.Render("Max Tokens"), m.cfg.Agents.MaxTokens)
	s += fmt.Sprintf("  %s: %d\n", fieldStyle.Render("Context Window"), m.cfg.Agents.ContextWindowTokens)
	s += fmt.Sprintf("  %s: %d\n", fieldStyle.Render("Max Tool Iterations"), m.cfg.Agents.MaxToolIterations)
	s += "\n"

	s += headerStyle.Render("Gateway") + "\n"
	s += fmt.Sprintf("  %s: %s\n", fieldStyle.Render("Host"), valueStyle.Render(m.cfg.Gateway.Host))
	s += fmt.Sprintf("  %s: %d\n", fieldStyle.Render("Port"), m.cfg.Gateway.Port)
	s += "\n"

	s += headerStyle.Render("Tools") + "\n"
	s += fmt.Sprintf("  %s: %s\n", fieldStyle.Render("Web Search Provider"), valueStyle.Render(m.cfg.Tools.Web.SearchProvider))
	s += "\n"

	s += dimmedStyle.Render("Press Enter to continue...") + "\n"

	return s
}

func (m *wizardModel) viewEditField() string {
	var s string
	s += titleStyle.Render("\n=== Edit " + m.editFieldName + " ===\n\n")

	prompt := fieldStyle.Render("Enter new value: ")

	// Show text input
	s += prompt + m.textInput.View() + "\n"

	s += dimmedStyle.Render("\nEnter to save, Escape to cancel\n")

	return s
}

func maskString(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

// RunWizard runs the interactive onboard wizard
func RunWizard() (*config.Config, bool, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, false, err
	}
	configDir := filepath.Join(usr.HomeDir, ".nanobot")
	configPath := filepath.Join(configDir, "config.json")

	// Determine starting config
	var cfg config.Config
	if _, statErr := os.Stat(configPath); statErr == nil {
		// Config exists, load it
		loadedCfg, err := config.Load("")
		if err == nil && loadedCfg != nil {
			cfg = *loadedCfg
		} else {
			cfg = getDefaultConfig()
		}
	} else {
		cfg = getDefaultConfig()
	}

	// Create model with full initialization
	model := &wizardModel{
		cfg:         cfg,
		state:       stateMainMenu,
		cursor:      0,
		fieldCursor: 0,
	}

	// Use standard tea.NewProgram without custom signal handling
	// Let bubbletea handle Ctrl+C properly with alt screen
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Run the program and get the final model
	finalModel, err := p.Run()
	if err != nil {
		return nil, false, err
	}

	wm, ok := finalModel.(*wizardModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected model type")
	}

	if wm.saved {
		os.MkdirAll(configDir, 0755)
		saveConfig(&wm.cfg, configPath)
		return &wm.cfg, true, nil
	}

	return nil, false, nil
}

func getDefaultConfig() config.Config {
	return config.Config{
		Agents: config.AgentDefaults{
			Model:               "claude-opus-4-5",
			Provider:            "auto",
			MaxTokens:           8192,
			ContextWindowTokens: 65536,
			Temperature:         0.1,
			MaxToolIterations:   40,
		},
		Gateway: config.GatewayConfig{
			Host: "0.0.0.0",
			Port: 18790,
		},
		Tools: config.ToolsConfig{
			Web: config.WebConfig{
				SearchProvider: "brave",
			},
		},
		Providers: config.ProvidersConfig{},
	}
}

func saveConfig(cfg *config.Config, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Simple JSON output
	importance := map[string]any{
		"agents": map[string]any{
			"model":                cfg.Agents.Model,
			"provider":             cfg.Agents.Provider,
			"max_tokens":           cfg.Agents.MaxTokens,
			"context_window_tokens": cfg.Agents.ContextWindowTokens,
			"temperature":           cfg.Agents.Temperature,
			"max_tool_iterations":  cfg.Agents.MaxToolIterations,
		},
		"gateway": map[string]any{
			"host": cfg.Gateway.Host,
			"port": cfg.Gateway.Port,
		},
		"tools": map[string]any{
			"web": map[string]any{
				"search_provider": cfg.Tools.Web.SearchProvider,
			},
		},
		"providers": cfg.Providers,
	}

	// Pretty print using encoding/json
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(importance)
}
