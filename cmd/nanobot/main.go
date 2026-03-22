package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"

	"nanobot-go/internal/agent"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/config"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "nanobot",
	Short: "Nanobot AI Assistant",
	Long:  "A lightweight AI assistant framework rewritten in Go",
}

var onboardCmd = &cobra.Command{
	Use:   "onboard",
	Short: "Initialize nanobot configuration",
	Run:   runOnboard,
}

var onboardWizardFlag bool

func init() {
	onboardCmd.Flags().BoolVarP(&onboardWizardFlag, "wizard", "w", false, "Use interactive wizard")
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run the agent",
	Run:   runAgent,
}

var messageFlag string
var sessionFlag string
var workspaceFlag string

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the gateway server",
	Run:   runGateway,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show nanobot status",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Nanobot Status")
		fmt.Println("==============")
		fmt.Println("Version: 0.1.0-go")
		fmt.Println("Status: Running")
	},
}

var channelsCmd = &cobra.Command{
	Use:   "channels",
	Short: "Manage channels",
}

var channelsLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to a channel",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Channel login not implemented")
	},
}

var channelsStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show channel status",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Channel Status")
		fmt.Println("==============")
	},
}

func init() {
	agentCmd.Flags().StringVarP(&messageFlag, "message", "m", "", "Message to send")
	agentCmd.Flags().StringVarP(&sessionFlag, "session", "s", "cli:direct", "Session ID")
	agentCmd.Flags().StringVarP(&workspaceFlag, "workspace", "w", "", "Workspace directory")

	rootCmd.AddCommand(onboardCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(gatewayCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(channelsCmd)
	channelsCmd.AddCommand(channelsLoginCmd)
	channelsCmd.AddCommand(channelsStatusCmd)
}

func runAgent(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()
	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Initialize components
	messageBus := bus.New(100)
	sessionStore, err := session.NewFileSessionStore("sessions")
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	toolRegistry := tools.NewRegistry()
	// Register default tools
	toolRegistry.Register(tools.NewMessageTool())
	toolRegistry.Register(tools.NewFilesystemTool(nil))
	toolRegistry.Register(tools.NewShellTool(true, nil, nil))
	toolRegistry.Register(tools.NewWebTool())

	// Create provider
	provider := providers.NewOpenAIProvider(
		"https://api.openai.com/v1",
		os.Getenv("OPENAI_API_KEY"),
		"gpt-4",
	)

	agentLoop := agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, cfg.Agents.MaxToolIterations)

	if messageFlag != "" {
		// Single message mode
		inbound := bus.InboundMessage{
			Channel:    "cli",
			SenderID:   "cli",
			ChatID:     sessionFlag,
			Content:    messageFlag,
			SessionKey: sessionFlag,
		}
		messageBus.PublishInbound(inbound)
		<-ctx.Done()
	} else {
		// Interactive mode
		fmt.Println("Nanobot agent started. Press Ctrl+C to exit.")
		agentLoop.Start(ctx)
	}
}

func runGateway(cmd *cobra.Command, args []string) {
	fmt.Println("Starting gateway server...")
	// Gateway startup would be implemented here
}

func runOnboard(cmd *cobra.Command, args []string) {
	usr, err := user.Current()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting home directory: %v\n", err)
		os.Exit(1)
	}
	configDir := filepath.Join(usr.HomeDir, ".nanobot")
	configPath := filepath.Join(configDir, "config.json")

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		if !onboardWizardFlag {
			fmt.Printf("Config already exists at %s\n", configPath)
			fmt.Println("Use --wizard flag for interactive configuration, or manually edit the config file.")
			return
		}
		// In wizard mode, for now just overwrite (future: could merge)
		fmt.Printf("Config exists, will be overwritten\n")
	}

	// Create default configuration (matches Python Config() defaults)
	cfg := config.Config{
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
	}

	// Save config
	file, err := os.Create(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing config file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Printf("Created config at %s\n", configPath)

	if onboardWizardFlag {
		fmt.Println("Interactive wizard not yet implemented in Go version")
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Add your API key to %s\n", configPath)
	fmt.Println("     Get one at: https://openrouter.ai/keys")
	fmt.Println("  2. Run: ./nanobot agent -m \"Hello!\"")
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
