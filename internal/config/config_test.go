package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsToOriConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	oriDir := filepath.Join(home, ".ori")
	if err := os.MkdirAll(oriDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oriDir, "config.json"), []byte(`{
		"agents": {"model": "from-ori", "provider": "openai"}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agents.Model != "from-ori" {
		t.Fatalf("model = %q; want from-ori", cfg.Agents.Model)
	}
}

func TestLoadIgnoresHistoricalOrbConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	orbDir := filepath.Join(home, ".orb")
	if err := os.MkdirAll(orbDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orbDir, "config.json"), []byte(`{
		"agents": {"model": "from-orb", "provider": "openai"}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agents.Model == "from-orb" {
		t.Fatalf("loaded config from ignored .orb directory")
	}
}

func TestLoadWebToolSearchConfig(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.json")
	if err := os.WriteFile(path, []byte(`{
		"tools": {
			"web": {
				"search_provider": "searxng",
				"search_api_key": "secret",
				"search_base_url": "https://search.example.test",
				"search_max_results": 7
			}
		}
	}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tools.Web.SearchProvider != "searxng" {
		t.Fatalf("SearchProvider = %q; want searxng", cfg.Tools.Web.SearchProvider)
	}
	if cfg.Tools.Web.SearchAPIKey != "secret" {
		t.Fatalf("SearchAPIKey = %q; want secret", cfg.Tools.Web.SearchAPIKey)
	}
	if cfg.Tools.Web.SearchBaseURL != "https://search.example.test" {
		t.Fatalf("SearchBaseURL = %q; want https://search.example.test", cfg.Tools.Web.SearchBaseURL)
	}
	if cfg.Tools.Web.SearchMaxResults != 7 {
		t.Fatalf("SearchMaxResults = %d; want 7", cfg.Tools.Web.SearchMaxResults)
	}
}
