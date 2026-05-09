package app_test

import (
	"context"
	"os"
	"testing"

	"ori/internal/app"
	"ori/internal/config"
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
