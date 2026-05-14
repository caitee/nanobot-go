package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchJSONFilePreservesUnknownFieldsAndSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeConfigTestFile(t, path, `{
		"agents": {
			"model": "old-model",
			"provider": "openai",
			"custom_agent_field": "keep"
		},
		"providers": {
			"openai": {
				"api_key": "secret-key"
			}
		},
		"unknown_top_level": {
			"nested": true
		}
	}`)

	err := PatchJSONFile(path, map[string]any{
		"agents": map[string]any{
			"model":            "new-model",
			"enable_reasoning": true,
		},
		"skills": map[string]any{
			"disabled": []string{"demo"},
		},
	})
	if err != nil {
		t.Fatalf("PatchJSONFile: %v", err)
	}

	var got map[string]any
	readJSONTestFile(t, path, &got)
	agents := got["agents"].(map[string]any)
	if agents["model"] != "new-model" {
		t.Fatalf("model = %v; want new-model", agents["model"])
	}
	if agents["custom_agent_field"] != "keep" {
		t.Fatalf("custom agent field was not preserved: %#v", agents)
	}
	providers := got["providers"].(map[string]any)
	openai := providers["openai"].(map[string]any)
	if openai["api_key"] != "secret-key" {
		t.Fatalf("provider secret was not preserved: %#v", openai)
	}
	if got["unknown_top_level"] == nil {
		t.Fatalf("unknown top-level object was not preserved: %#v", got)
	}
}

func TestPatchMCPServerEnabledCreatesUserFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".ori", "mcp.json")

	if err := PatchMCPServerEnabled(path, "alpha", false); err != nil {
		t.Fatalf("PatchMCPServerEnabled: %v", err)
	}

	var got map[string]any
	readJSONTestFile(t, path, &got)
	servers := got["mcpServers"].(map[string]any)
	alpha := servers["alpha"].(map[string]any)
	if alpha["enabled"] != false {
		t.Fatalf("enabled = %#v; want false", alpha["enabled"])
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v; want 0600", info.Mode().Perm())
	}
}

func writeConfigTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func readJSONTestFile(t *testing.T, path string, dest any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		t.Fatalf("Unmarshal: %v\n%s", err, string(data))
	}
}
