package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MCPLifecycle controls when an MCP server process/session is opened.
type MCPLifecycle string

const (
	MCPLifecycleLazy      MCPLifecycle = "lazy"
	MCPLifecycleEager     MCPLifecycle = "eager"
	MCPLifecycleKeepAlive MCPLifecycle = "keep-alive"
)

const (
	defaultMCPIdleTimeout = 10 * time.Minute
	defaultMCPBackoff     = 60 * time.Second
)

// MCPConfig is the merged MCP host configuration used by Ori.
type MCPConfig struct {
	Settings MCPSettings                `json:"settings"`
	Servers  map[string]MCPServerConfig `json:"mcpServers"`
}

// MCPSettings contains global MCP defaults.
type MCPSettings struct {
	IdleTimeout    time.Duration      `json:"-"`
	FailureBackoff time.Duration      `json:"-"`
	DirectTools    DirectToolSelector `json:"directTools,omitempty"`
	CachePath      string             `json:"cachePath,omitempty"`
}

// DirectToolSelector represents directTools: true, false, or ["tool"].
type DirectToolSelector struct {
	All      bool
	Names    []string
	Explicit bool
}

// Contains reports whether name is selected.
func (s DirectToolSelector) Contains(name string) bool {
	if s.All {
		return true
	}
	for _, n := range s.Names {
		if n == name {
			return true
		}
	}
	return false
}

// Enabled reports whether the selector enables at least one tool.
func (s DirectToolSelector) Enabled() bool {
	return s.All || len(s.Names) > 0
}

// MCPServerConfig holds one MCP server's transport and exposure settings.
type MCPServerConfig struct {
	Name         string
	Transport    string
	Command      string
	Args         []string
	Env          map[string]string
	URL          string
	Headers      map[string]string
	Description  string
	Instructions string
	Timeout      int
	EnabledTools []string
	ExcludeTools []string
	ToolTimeout  int
	Lifecycle    MCPLifecycle
	DirectTools  DirectToolSelector
	Enabled      bool
	EnabledSet   bool `json:"-"`
}

// MCPConfigLoadOptions controls config discovery and test injection.
type MCPConfigLoadOptions struct {
	Paths     []string
	HomeDir   string
	Workspace string
	Inline    map[string]any
}

// DefaultMCPConfigPaths returns MCP config files from lowest to highest priority.
func DefaultMCPConfigPaths(homeDir, workspace string) []string {
	paths := []string{
		filepath.Join(homeDir, ".ori", "mcp.json"),
	}
	if workspace != "" {
		paths = append(paths,
			filepath.Join(workspace, ".ori", "mcp.json"),
		)
	}
	return paths
}

// LoadMCPConfig loads and merges MCP configuration files and inline config.
func LoadMCPConfig(opts MCPConfigLoadOptions) (*MCPConfig, error) {
	home := opts.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	paths := opts.Paths
	if len(paths) == 0 {
		paths = DefaultMCPConfigPaths(home, opts.Workspace)
	}

	cfg := &MCPConfig{
		Settings: MCPSettings{
			IdleTimeout:    defaultMCPIdleTimeout,
			FailureBackoff: defaultMCPBackoff,
			CachePath:      filepath.Join(home, ".ori", "mcp-cache.json"),
		},
		Servers: map[string]MCPServerConfig{},
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		data, err := os.ReadFile(expandPath(path, home))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read MCP config %s: %w", path, err)
		}
		if err := applyMCPConfigJSON(cfg, data, home); err != nil {
			return nil, fmt.Errorf("parse MCP config %s: %w", path, err)
		}
	}

	if len(opts.Inline) > 0 {
		data, err := json.Marshal(opts.Inline)
		if err != nil {
			return nil, fmt.Errorf("marshal inline MCP config: %w", err)
		}
		if err := applyMCPConfigJSON(cfg, data, home); err != nil {
			return nil, fmt.Errorf("parse inline MCP config: %w", err)
		}
	}

	for name, server := range cfg.Servers {
		server.Name = name
		if server.Lifecycle == "" {
			server.Lifecycle = MCPLifecycleLazy
		}
		if server.Env == nil {
			server.Env = map[string]string{}
		}
		if server.Headers == nil {
			server.Headers = map[string]string{}
		}
		cfg.Servers[name] = server
	}
	return cfg, nil
}

func applyMCPConfigJSON(cfg *MCPConfig, data []byte, home string) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	if raw, ok := root["settings"]; ok {
		if err := applyMCPSettings(cfg, raw, home); err != nil {
			return err
		}
	}
	rawServers, ok := root["mcpServers"]
	if !ok {
		rawServers = root["servers"]
	}
	if len(rawServers) == 0 {
		return nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(rawServers, &servers); err != nil {
		return err
	}
	for name, raw := range servers {
		current, ok := cfg.Servers[name]
		if !ok {
			current = MCPServerConfig{
				Name:      name,
				Enabled:   true,
				Env:       map[string]string{},
				Headers:   map[string]string{},
				Lifecycle: MCPLifecycleLazy,
			}
		}
		if err := applyMCPServer(&current, raw, home); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		current.Name = name
		cfg.Servers[name] = current
	}
	return nil
}

func applyMCPSettings(cfg *MCPConfig, raw json.RawMessage, home string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if raw, ok := fields["idleTimeout"]; ok {
		seconds, err := decodeSeconds(raw)
		if err != nil {
			return fmt.Errorf("settings.idleTimeout: %w", err)
		}
		cfg.Settings.IdleTimeout = seconds
	}
	if raw, ok := fields["failureBackoff"]; ok {
		seconds, err := decodeSeconds(raw)
		if err != nil {
			return fmt.Errorf("settings.failureBackoff: %w", err)
		}
		cfg.Settings.FailureBackoff = seconds
	}
	if raw, ok := fields["cachePath"]; ok {
		var path string
		if err := json.Unmarshal(raw, &path); err != nil {
			return fmt.Errorf("settings.cachePath: %w", err)
		}
		cfg.Settings.CachePath = expandPath(expandEnv(path), home)
	}
	if raw, ok := fields["directTools"]; ok {
		selector, err := decodeDirectToolSelector(raw)
		if err != nil {
			return fmt.Errorf("settings.directTools: %w", err)
		}
		cfg.Settings.DirectTools = selector
	}
	return nil
}

func applyMCPServer(server *MCPServerConfig, raw json.RawMessage, home string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	decodeString := func(key string, dest *string) error {
		raw, ok := fields[key]
		if !ok {
			return nil
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dest = expandPath(expandEnv(value), home)
		return nil
	}
	decodeStringNoPath := func(key string, dest *string) error {
		raw, ok := fields[key]
		if !ok {
			return nil
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dest = expandEnv(value)
		return nil
	}
	if err := decodeString("command", &server.Command); err != nil {
		return err
	}
	if err := decodeStringNoPath("url", &server.URL); err != nil {
		return err
	}
	if err := decodeStringNoPath("transport", &server.Transport); err != nil {
		return err
	}
	if raw, ok := fields["args"]; ok {
		var args []string
		if err := json.Unmarshal(raw, &args); err != nil {
			return fmt.Errorf("args: %w", err)
		}
		for i := range args {
			args[i] = expandPath(expandEnv(args[i]), home)
		}
		server.Args = args
	}
	if raw, ok := fields["env"]; ok {
		env, err := decodeStringMap(raw, home, false)
		if err != nil {
			return fmt.Errorf("env: %w", err)
		}
		server.Env = mergeStringMap(server.Env, env)
	}
	if raw, ok := fields["headers"]; ok {
		headers, err := decodeStringMap(raw, home, false)
		if err != nil {
			return fmt.Errorf("headers: %w", err)
		}
		server.Headers = mergeStringMap(server.Headers, headers)
	}
	if raw, ok := fields["description"]; ok {
		if err := json.Unmarshal(raw, &server.Description); err != nil {
			return fmt.Errorf("description: %w", err)
		}
	}
	if raw, ok := fields["instructions"]; ok {
		if err := json.Unmarshal(raw, &server.Instructions); err != nil {
			return fmt.Errorf("instructions: %w", err)
		}
	}
	if raw, ok := fields["timeout"]; ok {
		if err := json.Unmarshal(raw, &server.Timeout); err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
	}
	if raw, ok := fields["toolTimeout"]; ok {
		if err := json.Unmarshal(raw, &server.ToolTimeout); err != nil {
			return fmt.Errorf("toolTimeout: %w", err)
		}
	}
	if raw, ok := fields["enabledTools"]; ok {
		if err := json.Unmarshal(raw, &server.EnabledTools); err != nil {
			return fmt.Errorf("enabledTools: %w", err)
		}
	}
	if raw, ok := fields["excludeTools"]; ok {
		if err := json.Unmarshal(raw, &server.ExcludeTools); err != nil {
			return fmt.Errorf("excludeTools: %w", err)
		}
	}
	if raw, ok := fields["lifecycle"]; ok {
		var lifecycle string
		if err := json.Unmarshal(raw, &lifecycle); err != nil {
			return fmt.Errorf("lifecycle: %w", err)
		}
		server.Lifecycle = MCPLifecycle(lifecycle)
	}
	if raw, ok := fields["directTools"]; ok {
		selector, err := decodeDirectToolSelector(raw)
		if err != nil {
			return fmt.Errorf("directTools: %w", err)
		}
		server.DirectTools = selector
	}
	if raw, ok := fields["enabled"]; ok {
		if err := json.Unmarshal(raw, &server.Enabled); err != nil {
			return fmt.Errorf("enabled: %w", err)
		}
		server.EnabledSet = true
	}
	return nil
}

func decodeSeconds(raw json.RawMessage) (time.Duration, error) {
	var seconds float64
	if err := json.Unmarshal(raw, &seconds); err != nil {
		return 0, err
	}
	if seconds <= 0 {
		return 0, fmt.Errorf("must be positive")
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func decodeDirectToolSelector(raw json.RawMessage) (DirectToolSelector, error) {
	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return DirectToolSelector{All: asBool, Explicit: true}, nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		return DirectToolSelector{Names: names, Explicit: true}, nil
	}
	return DirectToolSelector{}, fmt.Errorf("must be boolean or string array")
}

func decodeStringMap(raw json.RawMessage, home string, pathValues bool) (map[string]string, error) {
	var in map[string]string
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		v = expandEnv(v)
		if pathValues {
			v = expandPath(v, home)
		}
		out[k] = v
	}
	return out, nil
}

func mergeStringMap(base, overlay map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for k, v := range overlay {
		base[k] = v
	}
	return base
}

func expandEnv(value string) string {
	return os.ExpandEnv(value)
}

func expandPath(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}
