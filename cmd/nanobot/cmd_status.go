package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"nanobot-go/internal/config"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show nanobot status",
	Run:   runStatus,
}

var statusConfigFlag string

func init() {
	statusCmd.Flags().StringVarP(&statusConfigFlag, "config", "c", "", "Config file path")
}

func runStatus(cmd *cobra.Command, args []string) {
	cfg, err := config.Load(statusConfigFlag)
	if err != nil {
		slog.Warn("failed to load config", "error", err)
		cfg = &config.Config{}
	}

	// Get config path
	configPath := statusConfigFlag
	if configPath == "" {
		homeDir, _ := os.UserHomeDir()
		configPath = filepath.Join(homeDir, ".nanobot", "config.json")
	}

	// Get workspace path
	workspace := cfg.Agents.Workspace
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}

	fmt.Printf("%s nanobot Status\n\n", logo)

	// Config status
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Config: %s ✓\n", configPath)
	} else {
		fmt.Printf("Config: %s ✗ (not found)\n", configPath)
	}

	// Workspace status
	if _, err := os.Stat(workspace); err == nil {
		fmt.Printf("Workspace: %s ✓\n", workspace)
	} else {
		fmt.Printf("Workspace: %s ✗ (not found)\n", workspace)
	}

	fmt.Println()

	// Model info
	fmt.Printf("Model: %s\n", cfg.Agents.Model)
	fmt.Printf("Provider: %s\n", cfg.Agents.Provider)

	// Check API keys
	if cfg.Providers.OpenAI != nil {
		if k, ok := cfg.Providers.OpenAI["api_key"].(string); ok && k != "" {
			fmt.Println("OpenAI: ✓ configured")
		} else {
			fmt.Println("OpenAI: not configured")
		}
	}

	if cfg.Providers.OpenRouter != nil {
		if k, ok := cfg.Providers.OpenRouter["api_key"].(string); ok && k != "" {
			fmt.Println("OpenRouter: ✓ configured")
		} else {
			fmt.Println("OpenRouter: not configured")
		}
	}

	if cfg.Providers.Anthropic != nil {
		if k, ok := cfg.Providers.Anthropic["api_key"].(string); ok && k != "" {
			fmt.Println("Anthropic: ✓ configured")
		} else {
			fmt.Println("Anthropic: not configured")
		}
	}

	if cfg.Providers.Minimax != nil {
		if k, ok := cfg.Providers.Minimax["api_key"].(string); ok && k != "" {
			fmt.Println("MiniMax: ✓ configured")
		} else {
			fmt.Println("MiniMax: not configured")
		}
	}
}
