package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadMCPConfigMergesSourcesAndExpandsValues(t *testing.T) {
	t.Setenv("MCP_TOKEN", "secret-token")
	home := t.TempDir()
	workspace := t.TempDir()
	cachePath := filepath.Join(home, ".ori", "mcp-cache.json")

	global := filepath.Join(t.TempDir(), "global.json")
	writeTestFile(t, global, `{
		"settings": {
			"idleTimeout": 5,
			"cachePath": "`+cachePath+`",
			"directTools": false
		},
		"mcpServers": {
			"remote": {
				"url": "https://example.test/mcp",
				"headers": {"Authorization": "Bearer ${MCP_TOKEN}"},
				"lifecycle": "lazy"
			},
			"local": {
				"command": "~/bin/server",
				"args": ["--root", "${MCP_ROOT}"],
				"enabled": false
			}
		}
	}`)

	workspaceFile := filepath.Join(workspace, ".mcp.json")
	t.Setenv("MCP_ROOT", filepath.Join(workspace, "root"))
	writeTestFile(t, workspaceFile, `{
		"settings": {"idleTimeout": 12, "directTools": true},
		"mcpServers": {
			"remote": {
				"lifecycle": "keep-alive",
				"directTools": ["take_screenshot"]
			}
		}
	}`)

	cfg, err := LoadMCPConfig(MCPConfigLoadOptions{
		Paths:     []string{global, workspaceFile},
		HomeDir:   home,
		Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}

	if cfg.Settings.IdleTimeout != 12*time.Second {
		t.Fatalf("idle timeout = %v", cfg.Settings.IdleTimeout)
	}
	if cfg.Settings.CachePath != cachePath {
		t.Fatalf("cache path = %q", cfg.Settings.CachePath)
	}
	if !cfg.Settings.DirectTools.All {
		t.Fatalf("expected workspace directTools override")
	}

	remote := cfg.Servers["remote"]
	if remote.URL != "https://example.test/mcp" {
		t.Fatalf("remote URL lost during merge: %q", remote.URL)
	}
	if remote.Headers["Authorization"] != "Bearer secret-token" {
		t.Fatalf("header was not expanded: %#v", remote.Headers)
	}
	if remote.Lifecycle != MCPLifecycleKeepAlive {
		t.Fatalf("lifecycle = %q", remote.Lifecycle)
	}
	if !remote.DirectTools.Contains("take_screenshot") {
		t.Fatalf("directTools override not parsed: %#v", remote.DirectTools)
	}

	local := cfg.Servers["local"]
	if local.Enabled {
		t.Fatalf("explicit disabled server should stay disabled")
	}
	if local.Command != filepath.Join(home, "bin", "server") {
		t.Fatalf("home expansion failed: %q", local.Command)
	}
	if local.Args[1] != filepath.Join(workspace, "root") {
		t.Fatalf("env expansion failed: %#v", local.Args)
	}
}

func TestMCPDefaultConfigPathsUseOriDirectory(t *testing.T) {
	home := t.TempDir()
	workspace := filepath.Join(home, "workspace")

	paths := DefaultMCPConfigPaths(home, workspace)
	want := []string{
		filepath.Join(home, ".config", "mcp", "mcp.json"),
		filepath.Join(home, ".ori", "mcp.json"),
		filepath.Join(workspace, ".mcp.json"),
		filepath.Join(workspace, ".ori", "mcp.json"),
	}
	if len(paths) != len(want) {
		t.Fatalf("paths length = %d", len(paths))
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %q; want %q", i, paths[i], want[i])
		}
	}
}

func TestMCPMetadataCacheDoesNotPersistSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	cfg := MCPServerConfig{
		Name:    "remote",
		URL:     "https://example.test/mcp",
		Headers: map[string]string{"Authorization": "Bearer secret-token"},
		Env:     map[string]string{"PRIVATE_TOKEN": "secret-token"},
		Enabled: true,
	}
	cache := &MCPMetadataCache{Servers: map[string]MCPServerMetadata{
		"remote": {
			ConfigHash: HashMCPServerConfig(cfg),
			Tools:      []MCPToolMeta{{Name: "echo"}},
		},
	}}

	if err := cache.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "secret-token") || strings.Contains(string(data), "Authorization") {
		t.Fatalf("cache persisted secret material: %s", string(data))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
