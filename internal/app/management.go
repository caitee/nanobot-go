package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"ori/internal/config"
	"ori/internal/skills"
	"ori/internal/tool"
	legacytools "ori/internal/tools"
)

const (
	// UIRequestMCP asks a capable client to open the MCP management panel.
	UIRequestMCP = "mcp"
	// UIRequestSkills asks a capable client to open the skill management panel.
	UIRequestSkills = "skills"
	// UIRequestConfig asks a capable client to open the config management panel.
	UIRequestConfig = "config"
)

// ManagementService owns user-facing management operations for TUI panels and
// slash-command text fallbacks.
type ManagementService struct {
	cfg          *config.Config
	configPath   string
	mcpPath      string
	skillLoader  *skills.SkillLoader
	mcpManager   *legacytools.MCPManager
	toolRegistry tool.Registry
	hotApply     func() error
}

// ManagementOptions configures a ManagementService.
type ManagementOptions struct {
	Config       *config.Config
	ConfigPath   string
	MCPPath      string
	SkillLoader  *skills.SkillLoader
	MCPManager   *legacytools.MCPManager
	ToolRegistry tool.Registry
	HotApply     func() error
}

// MCPServerView is a stable TUI-friendly server snapshot.
type MCPServerView struct {
	Name      string
	Enabled   bool
	Connected bool
	Lifecycle string
	Tools     int
	Resources int
	Prompts   int
	LastError string
}

// SkillView is a stable TUI-friendly skill snapshot.
type SkillView struct {
	Name        string
	Description string
	Source      string
	Available   bool
	Enabled     bool
}

// ConfigFieldView describes one editable config field.
type ConfigFieldView struct {
	Key             string
	Label           string
	Value           string
	Kind            string
	RestartRequired bool
}

// NewManagementService creates a management service with default user paths.
func NewManagementService(opts ManagementOptions) *ManagementService {
	cfg := opts.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = cfg.SourcePath
	}
	home, _ := os.UserHomeDir()
	if configPath == "" && home != "" {
		configPath = config.DefaultConfigPath(home)
	}
	mcpPath := opts.MCPPath
	if mcpPath == "" && home != "" {
		mcpPath = config.DefaultMCPConfigPath(home)
	}
	s := &ManagementService{
		cfg:          cfg,
		configPath:   configPath,
		mcpPath:      mcpPath,
		skillLoader:  opts.SkillLoader,
		mcpManager:   opts.MCPManager,
		toolRegistry: opts.ToolRegistry,
		hotApply:     opts.HotApply,
	}
	if s.mcpManager != nil && s.toolRegistry != nil {
		s.mcpManager.SetMetadataChangeHook(s.refreshMCPDirectTools)
	}
	return s
}

// SetHotApply installs the callback used after runtime-affecting saves.
func (s *ManagementService) SetHotApply(fn func() error) { s.hotApply = fn }

// MCPServers returns the configured MCP servers.
func (s *ManagementService) MCPServers() []MCPServerView {
	if s == nil || s.mcpManager == nil {
		return nil
	}
	status := s.mcpManager.Status()
	out := make([]MCPServerView, 0, len(status))
	for _, item := range status {
		out = append(out, MCPServerView{
			Name:      item.Name,
			Enabled:   item.Enabled,
			Connected: item.Connected,
			Lifecycle: item.Lifecycle,
			Tools:     item.Tools,
			Resources: item.Resources,
			Prompts:   item.Prompts,
			LastError: item.LastError,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FormatMCPStatus returns a plain text command fallback.
func (s *ManagementService) FormatMCPStatus() string {
	servers := s.MCPServers()
	if len(servers) == 0 {
		return "No MCP servers configured."
	}
	lines := []string{"MCP servers:"}
	for _, item := range servers {
		enabled := "disabled"
		if item.Enabled {
			enabled = "enabled"
		}
		state := "disconnected"
		if item.Connected {
			state = "connected"
		}
		line := fmt.Sprintf("%s: %s, %s, lifecycle=%s, tools=%d, resources=%d, prompts=%d",
			item.Name, enabled, state, item.Lifecycle, item.Tools, item.Resources, item.Prompts)
		if item.LastError != "" {
			line += ", last_error=" + item.LastError
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ToggleMCPServer persists and applies a server enabled toggle.
func (s *ManagementService) ToggleMCPServer(ctx context.Context, name string) (string, error) {
	if s == nil || s.mcpManager == nil {
		return "", fmt.Errorf("MCP manager is not available")
	}
	var current *MCPServerView
	for _, item := range s.MCPServers() {
		if item.Name == name {
			copy := item
			current = &copy
			break
		}
	}
	if current == nil {
		return "", fmt.Errorf("MCP server not found: %s", name)
	}
	next := !current.Enabled
	if err := s.mcpManager.SetServerEnabled(ctx, name, next); err != nil {
		return "", err
	}
	if s.mcpPath != "" {
		if err := config.PatchMCPServerEnabled(s.mcpPath, name, next); err != nil {
			return "", err
		}
	}
	s.refreshMCPDirectTools()
	if next {
		return "enabled " + name, nil
	}
	return "disabled " + name, nil
}

// RefreshMCPServer connects a server and refreshes metadata.
func (s *ManagementService) RefreshMCPServer(ctx context.Context, name string) (string, error) {
	if s == nil || s.mcpManager == nil {
		return "", fmt.Errorf("MCP manager is not available")
	}
	if err := s.mcpManager.ConnectServer(ctx, name); err != nil {
		return "", err
	}
	s.refreshMCPDirectTools()
	return "refreshed " + name, nil
}

func (s *ManagementService) refreshMCPDirectTools() {
	refreshMCPDirectTools(s.toolRegistry, s.mcpManager)
}

func refreshMCPDirectTools(reg tool.Registry, manager *legacytools.MCPManager) {
	if reg == nil || manager == nil {
		return
	}
	for _, t := range reg.All() {
		if legacytools.IsMCPDirectTool(t) {
			reg.Unregister(t.Name())
		}
	}
	for _, t := range manager.DirectTools() {
		reg.Register(t)
	}
}

// Skills returns all discoverable skills, including disabled ones.
func (s *ManagementService) Skills() []SkillView {
	if s == nil || s.skillLoader == nil {
		return nil
	}
	items := s.skillLoader.ListAllSkills(false)
	out := make([]SkillView, 0, len(items))
	for _, item := range items {
		out = append(out, SkillView{
			Name:        item.Name,
			Description: item.Description,
			Source:      item.Source,
			Available:   item.Available,
			Enabled:     item.Enabled,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// FormatSkillStatus returns a plain text command fallback.
func (s *ManagementService) FormatSkillStatus() string {
	if s == nil || s.skillLoader == nil {
		return "No skills found."
	}
	return skills.FormatSkillList(s.skillLoader.ListAllSkills(false))
}

// ToggleSkill persists and applies a skill visibility toggle.
func (s *ManagementService) ToggleSkill(name string) (string, error) {
	if s == nil || s.skillLoader == nil {
		return "", fmt.Errorf("skill loader is not available")
	}
	disabled := map[string]bool{}
	for _, item := range s.cfg.Skills.Disabled {
		disabled[item] = true
	}
	if disabled[name] {
		delete(disabled, name)
	} else {
		disabled[name] = true
	}
	next := make([]string, 0, len(disabled))
	for item := range disabled {
		next = append(next, item)
	}
	sort.Strings(next)
	s.cfg.Skills.Disabled = next
	s.skillLoader.SetDisabled(next)
	if s.configPath != "" {
		if err := config.PatchJSONFile(s.configPath, map[string]any{
			"skills": map[string]any{"disabled": next},
		}); err != nil {
			return "", err
		}
	}
	if s.hotApply != nil {
		if err := s.hotApply(); err != nil {
			return "", err
		}
	}
	if disabled[name] {
		return "disabled " + name, nil
	}
	return "enabled " + name, nil
}

// ConfigFields returns editable common config fields.
func (s *ManagementService) ConfigFields() []ConfigFieldView {
	if s == nil || s.cfg == nil {
		return nil
	}
	cfg := s.cfg
	return []ConfigFieldView{
		{Key: "agents.provider", Label: "Provider", Value: cfg.Agents.Provider, Kind: "string"},
		{Key: "agents.model", Label: "Model", Value: cfg.Agents.Model, Kind: "string"},
		{Key: "agents.enable_reasoning", Label: "Reasoning", Value: strconv.FormatBool(cfg.Agents.EnableReasoning), Kind: "bool"},
		{Key: "agents.reasoning_effort", Label: "Reasoning effort", Value: cfg.Agents.ReasoningEffort, Kind: "string"},
		{Key: "agents.temperature", Label: "Temperature", Value: formatFloat(cfg.Agents.Temperature), Kind: "float"},
		{Key: "agents.max_tokens", Label: "Max tokens", Value: strconv.Itoa(cfg.Agents.MaxTokens), Kind: "int"},
		{Key: "tools.web.search_provider", Label: "Web search provider", Value: cfg.Tools.Web.SearchProvider, Kind: "string", RestartRequired: true},
		{Key: "tools.web.search_base_url", Label: "Web search base URL", Value: cfg.Tools.Web.SearchBaseURL, Kind: "string", RestartRequired: true},
		{Key: "tools.web.search_max_results", Label: "Web max results", Value: strconv.Itoa(cfg.Tools.Web.SearchMaxResults), Kind: "int", RestartRequired: true},
		{Key: "tools.exec.enabled", Label: "Shell tool enabled", Value: strconv.FormatBool(cfg.Tools.Exec.Enabled), Kind: "bool", RestartRequired: true},
		{Key: "gateway.host", Label: "Gateway host", Value: cfg.Gateway.Host, Kind: "string", RestartRequired: true},
		{Key: "gateway.port", Label: "Gateway port", Value: strconv.Itoa(cfg.Gateway.Port), Kind: "int", RestartRequired: true},
	}
}

// FormatConfigStatus returns a plain text command fallback.
func (s *ManagementService) FormatConfigStatus() string {
	fields := s.ConfigFields()
	if len(fields) == 0 {
		return "No configurable fields available."
	}
	lines := []string{"Config:"}
	for _, field := range fields {
		value := field.Value
		if value == "" {
			value = "(empty)"
		}
		if field.RestartRequired {
			value += " (restart required after change)"
		}
		lines = append(lines, fmt.Sprintf("%s: %s", field.Key, value))
	}
	return strings.Join(lines, "\n")
}

// SaveConfigFields validates and persists edited config field values.
func (s *ManagementService) SaveConfigFields(values map[string]string) (bool, error) {
	if s == nil || s.cfg == nil {
		return false, fmt.Errorf("config is not available")
	}
	restartRequired := false
	patch := map[string]any{}
	for _, field := range s.ConfigFields() {
		raw, ok := values[field.Key]
		if !ok {
			continue
		}
		value, err := parseConfigField(field, raw)
		if err != nil {
			return false, err
		}
		if field.RestartRequired {
			restartRequired = true
		}
		applyConfigValue(s.cfg, field.Key, value)
		addConfigPatchValue(patch, field.Key, value)
	}
	if s.configPath != "" {
		if err := config.PatchJSONFile(s.configPath, patch); err != nil {
			return false, err
		}
	}
	if s.hotApply != nil {
		if err := s.hotApply(); err != nil {
			return false, err
		}
	}
	return restartRequired, nil
}

func parseConfigField(field ConfigFieldView, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	switch field.Kind {
	case "bool":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be true or false", field.Key)
		}
		return value, nil
	case "int":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be an integer", field.Key)
		}
		return value, nil
	case "float":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", field.Key)
		}
		return value, nil
	default:
		return raw, nil
	}
}

func applyConfigValue(cfg *config.Config, key string, value any) {
	switch key {
	case "agents.provider":
		cfg.Agents.Provider = value.(string)
	case "agents.model":
		cfg.Agents.Model = value.(string)
	case "agents.enable_reasoning":
		cfg.Agents.EnableReasoning = value.(bool)
	case "agents.reasoning_effort":
		cfg.Agents.ReasoningEffort = value.(string)
	case "agents.temperature":
		cfg.Agents.Temperature = value.(float64)
	case "agents.max_tokens":
		cfg.Agents.MaxTokens = value.(int)
	case "tools.web.search_provider":
		cfg.Tools.Web.SearchProvider = value.(string)
	case "tools.web.search_base_url":
		cfg.Tools.Web.SearchBaseURL = value.(string)
	case "tools.web.search_max_results":
		cfg.Tools.Web.SearchMaxResults = value.(int)
	case "tools.exec.enabled":
		cfg.Tools.Exec.Enabled = value.(bool)
	case "gateway.host":
		cfg.Gateway.Host = value.(string)
	case "gateway.port":
		cfg.Gateway.Port = value.(int)
	}
}

func addConfigPatchValue(root map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func formatFloat(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
