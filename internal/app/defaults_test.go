package app_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"ori/internal/app"
	"ori/internal/config"
	"ori/internal/tool"
	legacytools "ori/internal/tools"
)

// TestProviderPluginsRegisterDirectlyToLLMRegistry verifies that provider
// plugins register directly to llm.Registry instead of going through the
// legacy providers.Registry bridge.
func TestProviderPluginsRegisterDirectlyToLLMRegistry(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		provider string
	}{
		{
			name:     "openai",
			envKey:   "OPENAI_API_KEY",
			envValue: "test-key-openai",
			provider: "openai",
		},
		{
			name:     "anthropic",
			envKey:   "ANTHROPIC_API_KEY",
			envValue: "test-key-anthropic",
			provider: "anthropic",
		},
		{
			name:     "minimax",
			envKey:   "MINIMAX_API_KEY",
			envValue: "test-key-minimax",
			provider: "minimax",
		},
		{
			name:     "azure",
			envKey:   "AZURE_API_KEY",
			envValue: "test-key-azure",
			provider: "azure",
		},
		{
			name:     "openrouter",
			envKey:   "OPENROUTER_API_KEY",
			envValue: "test-key-openrouter",
			provider: "openrouter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			oldVal := os.Getenv(tt.envKey)
			os.Setenv(tt.envKey, tt.envValue)
			defer func() {
				if oldVal == "" {
					os.Unsetenv(tt.envKey)
				} else {
					os.Setenv(tt.envKey, oldVal)
				}
			}()

			// Create app with minimal config
			cfg := &config.Config{
				Agents: config.AgentDefaults{
					Provider: tt.provider,
					Model:    "test-model",
				},
				Providers: config.ProvidersConfig{
					OpenAI:     map[string]any{"api_key": tt.envValue},
					Anthropic:  map[string]any{"api_key": tt.envValue},
					Minimax:    map[string]any{"api_key": tt.envValue},
					Azure:      map[string]any{"api_key": tt.envValue, "api_base": "https://test.openai.azure.com"},
					OpenRouter: map[string]any{"api_key": tt.envValue},
				},
			}
			a, err := app.New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			// Initialize plugins
			ctx := context.Background()
			if err := a.PluginRegistry.InitAll(ctx, a); err != nil {
				t.Fatalf("InitAll: %v", err)
			}

			// Verify provider is registered in llm.Registry
			p, err := a.LLMRegistry.Get(tt.provider)
			if err != nil {
				t.Fatalf("LLMRegistry.Get(%q): %v", tt.provider, err)
			}
			if p == nil {
				t.Fatalf("expected provider, got nil")
			}

			// Verify provider is NOT in legacy registry
			// (it should only be in llm.Registry now)
			names := a.LegacyProviderRegistry.List()
			for _, name := range names {
				if name == tt.provider {
					t.Errorf("provider %q should not be in LegacyProviderRegistry", tt.provider)
				}
			}
		})
	}
}

func TestMCPPluginRegistersProxyAndCachedDirectTools(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	cachePath := filepath.Join(tmp, "mcp-cache.json")
	serverCfg := legacytools.MCPServerConfig{
		Name:      "alpha",
		Command:   "server",
		Enabled:   true,
		Lifecycle: legacytools.MCPLifecycleLazy,
		Env:       map[string]string{},
		Headers:   map[string]string{},
	}
	cache := legacytools.MCPMetadataCache{
		Version: 1,
		Servers: map[string]legacytools.MCPServerMetadata{
			"alpha": {
				ConfigHash: legacytools.HashMCPServerConfig(serverCfg),
				Tools: []legacytools.MCPToolMeta{{
					Name:        "echo",
					Description: "echo input",
					InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
				}},
			},
		},
	}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("Marshal cache: %v", err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatalf("WriteFile cache: %v", err)
	}

	cfg := &config.Config{
		Agents: config.AgentDefaults{
			Workspace: tmp,
			Provider:  "openai",
			Model:     "test-model",
		},
		Tools: config.ToolsConfig{
			MCP: map[string]any{
				"settings": map[string]any{
					"cachePath":   cachePath,
					"directTools": true,
				},
				"mcpServers": map[string]any{
					"alpha": map[string]any{
						"command": "server",
					},
				},
			},
		},
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.PluginRegistry.InitAll(ctx, a); err != nil {
		t.Fatalf("InitAll: %v", err)
	}
	defer a.PluginRegistry.CloseAll()

	if !a.ToolRegistry.Has("mcp") {
		t.Fatalf("expected mcp proxy tool to be registered")
	}
	if !a.ToolRegistry.Has("mcp_alpha_echo") {
		t.Fatalf("expected cached direct MCP tool to be registered")
	}
}

func TestWebPluginUsesConfiguredSearchConfig(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)

	cfg := &config.Config{
		Agents: config.AgentDefaults{
			Workspace: tmp,
			Provider:  "openai",
			Model:     "test-model",
		},
		Tools: config.ToolsConfig{
			Web: config.WebConfig{
				SearchProvider:   "searxng",
				SearchAPIKey:     "search-secret",
				SearchBaseURL:    "https://search.example.test",
				SearchMaxResults: 1,
			},
		},
	}
	a, err := app.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := a.PluginRegistry.InitAll(ctx, a); err != nil {
		t.Fatalf("InitAll: %v", err)
	}
	defer a.PluginRegistry.CloseAll()

	webTool, ok := a.ToolRegistry.Get("web")
	if !ok {
		t.Fatalf("expected web tool to be registered")
	}
	legacy, ok := tool.UnwrapLegacy(webTool)
	if !ok {
		t.Fatalf("web tool should wrap a legacy implementation")
	}
	searchConfig := reflect.ValueOf(legacy).Elem().FieldByName("searchConfig")
	if searchConfig.IsNil() {
		t.Fatalf("expected configured web search config")
	}
	cfgValue := searchConfig.Elem()
	if got := cfgValue.FieldByName("Provider").String(); got != "searxng" {
		t.Fatalf("Provider = %q; want searxng", got)
	}
	if got := cfgValue.FieldByName("APIKey").String(); got != "search-secret" {
		t.Fatalf("APIKey = %q; want search-secret", got)
	}
	if got := cfgValue.FieldByName("BaseURL").String(); got != "https://search.example.test" {
		t.Fatalf("BaseURL = %q; want https://search.example.test", got)
	}
	if got := cfgValue.FieldByName("MaxResults").Int(); got != 1 {
		t.Fatalf("MaxResults = %d; want 1", got)
	}
}
