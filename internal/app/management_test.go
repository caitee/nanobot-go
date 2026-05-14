package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"ori/internal/config"
	"ori/internal/skills"
	"ori/internal/tool"
	legacytools "ori/internal/tools"
)

func TestManagementToggleSkillPersistsAndAppliesVisibility(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	configPath := filepath.Join(tmp, "config.json")
	writeManagementTestFile(t, configPath, `{
		"providers": {"openai": {"api_key": "secret"}}
	}`)
	skillsDir := filepath.Join(tmp, "skills")
	writeManagementSkill(t, filepath.Join(skillsDir, "demo", "SKILL.md"), "demo")
	loader := skills.NewSkillLoader(skillsDir, filepath.Join(tmp, "no-builtins"))
	applied := 0
	mgmt := NewManagementService(ManagementOptions{
		Config:      &config.Config{SourcePath: configPath},
		ConfigPath:  configPath,
		SkillLoader: loader,
		HotApply: func() error {
			applied++
			return nil
		},
	})

	msg, err := mgmt.ToggleSkill("demo")
	if err != nil {
		t.Fatalf("ToggleSkill: %v", err)
	}
	if msg != "disabled demo" {
		t.Fatalf("message = %q; want disabled demo", msg)
	}
	if got := loader.ListSkillNames(false); len(got) != 0 {
		t.Fatalf("disabled skill should be hidden, got %v", got)
	}
	if applied != 1 {
		t.Fatalf("hot apply count = %d; want 1", applied)
	}
	var raw map[string]any
	readManagementJSON(t, configPath, &raw)
	if raw["providers"].(map[string]any)["openai"].(map[string]any)["api_key"] != "secret" {
		t.Fatalf("provider secret was not preserved: %#v", raw)
	}
	disabled := raw["skills"].(map[string]any)["disabled"].([]any)
	if len(disabled) != 1 || disabled[0] != "demo" {
		t.Fatalf("disabled = %#v; want demo", disabled)
	}
}

func TestManagementSaveConfigFieldsPersistsAndReportsRestart(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.json")
	writeManagementTestFile(t, configPath, `{"custom": {"keep": true}}`)
	cfg := &config.Config{SourcePath: configPath}
	applied := 0
	mgmt := NewManagementService(ManagementOptions{
		Config:     cfg,
		ConfigPath: configPath,
		HotApply: func() error {
			applied++
			return nil
		},
	})

	restart, err := mgmt.SaveConfigFields(map[string]string{
		"agents.provider":              "anthropic",
		"agents.model":                 "claude-test",
		"agents.enable_reasoning":      "true",
		"agents.temperature":           "0.2",
		"agents.max_tokens":            "1234",
		"tools.web.search_max_results": "9",
	})
	if err != nil {
		t.Fatalf("SaveConfigFields: %v", err)
	}
	if !restart {
		t.Fatalf("expected restart-required flag for web settings")
	}
	if applied != 1 {
		t.Fatalf("hot apply count = %d; want 1", applied)
	}
	if cfg.Agents.Provider != "anthropic" || cfg.Agents.Model != "claude-test" || !cfg.Agents.EnableReasoning {
		t.Fatalf("config not updated: %+v", cfg.Agents)
	}
	if cfg.Tools.Web.SearchMaxResults != 9 {
		t.Fatalf("web max results = %d; want 9", cfg.Tools.Web.SearchMaxResults)
	}
	var raw map[string]any
	readManagementJSON(t, configPath, &raw)
	if raw["custom"].(map[string]any)["keep"] != true {
		t.Fatalf("custom field was not preserved: %#v", raw)
	}
}

func TestManagementToggleMCPServerRefreshesDirectTools(t *testing.T) {
	tmp := t.TempDir()
	mcpPath := filepath.Join(tmp, "mcp.json")
	server := legacytools.MCPServerConfig{Name: "alpha", Command: "server", Enabled: true}
	manager := legacytools.NewMCPManager(legacytools.MCPManagerOptions{
		Config: &legacytools.MCPConfig{
			Settings: legacytools.MCPSettings{DirectTools: legacytools.DirectToolSelector{All: true}},
			Servers:  map[string]legacytools.MCPServerConfig{"alpha": server},
		},
		Cache: &legacytools.MCPMetadataCache{Servers: map[string]legacytools.MCPServerMetadata{
			"alpha": {
				ConfigHash: legacytools.HashMCPServerConfig(server),
				Tools: []legacytools.MCPToolMeta{{
					Name:        "echo",
					Description: "echo input",
					InputSchema: map[string]any{"type": "object"},
				}},
			},
		}},
	})
	reg := tool.NewRegistry()
	reg.Register(legacytools.NewMCPProxyTool(manager))
	for _, direct := range manager.DirectTools() {
		reg.Register(direct)
	}
	if !reg.Has("mcp_alpha_echo") {
		t.Fatalf("expected direct MCP tool before toggle")
	}
	mgmt := NewManagementService(ManagementOptions{
		Config:       &config.Config{},
		MCPPath:      mcpPath,
		MCPManager:   manager,
		ToolRegistry: reg,
	})

	if _, err := mgmt.ToggleMCPServer(context.Background(), "alpha"); err != nil {
		t.Fatalf("ToggleMCPServer: %v", err)
	}
	if reg.Has("mcp_alpha_echo") {
		t.Fatalf("direct MCP tool should be removed after disabling server")
	}
	var raw map[string]any
	readManagementJSON(t, mcpPath, &raw)
	alpha := raw["mcpServers"].(map[string]any)["alpha"].(map[string]any)
	if alpha["enabled"] != false {
		t.Fatalf("persisted enabled = %#v; want false", alpha["enabled"])
	}
}

func writeManagementSkill(t *testing.T, path, name string) {
	t.Helper()
	writeManagementTestFile(t, path, `---
name: `+name+`
description: "Demo skill"
---

# Demo
`)
}

func writeManagementTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readManagementJSON(t *testing.T, path string, dest any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, string(data))
	}
}
