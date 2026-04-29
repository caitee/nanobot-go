package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"nanobot-go/internal/agent"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/config"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Interact with the agent",
	Run:   runAgent,
}

var (
	agentMessageFlag   string
	agentSessionFlag   string
	agentWorkspaceFlag string
	agentConfigFlag    string
	agentMarkdownFlag  bool
	agentLogsFlag      bool
)

func init() {
	agentCmd.Flags().StringVarP(&agentMessageFlag, "message", "m", "", "Message to send to the agent")
	agentCmd.Flags().StringVarP(&agentSessionFlag, "session", "s", "cli:direct", "Session ID")
	agentCmd.Flags().StringVarP(&agentWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	agentCmd.Flags().StringVarP(&agentConfigFlag, "config", "c", "", "Config file path")
	agentCmd.Flags().BoolVarP(&agentMarkdownFlag, "markdown", "", true, "Render assistant output as Markdown")
	agentCmd.Flags().BoolVarP(&agentLogsFlag, "logs", "", false, "Show nanobot runtime logs during chat")
}

func runAgent(cmd *cobra.Command, args []string) {
	ctx := context.Background()
	cfg, err := config.Load(agentConfigFlag)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override workspace if specified
	workspace := agentWorkspaceFlag
	if workspace == "" {
		workspace = cfg.Agents.Workspace
	}
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}

	// Create session store
	sessionStore, err := session.NewFileSessionStore("sessions")
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	// Create tool registry
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(tools.NewMessageTool())
	toolRegistry.Register(tools.NewFilesystemTool(nil))
	toolRegistry.Register(tools.NewShellTool(true, nil, nil))
	toolRegistry.Register(tools.NewWebTool())
	toolRegistry.Register(tools.NewCronTool(nil, nil))
	toolRegistry.Register(tools.NewSpawnTool(nil))

	// Create provider based on config
	provider := createProvider(cfg)

	// Create agent loop
	maxIterations := cfg.Agents.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 40
	}
	agentLoop := agent.NewAgentLoop(nil, sessionStore, toolRegistry, provider, maxIterations, false)

	if agentMessageFlag != "" {
		// Single message mode
		runAgentSingle(ctx, agentLoop, sessionStore, toolRegistry, provider, maxIterations)
	} else {
		// Interactive mode
		runAgentInteractive(ctx, agentLoop, sessionStore, toolRegistry, provider, maxIterations)
	}
}

func createProvider(cfg *config.Config) providers.LLMProvider {
	model := cfg.Agents.Model
	providerName := cfg.Agents.Provider

	// Try to get API key from providers config
	var apiKey string
	var apiBase string

	switch providerName {
	case "openai":
		if cfg.Providers.OpenAI != nil {
			if k, ok := cfg.Providers.OpenAI["api_key"].(string); ok {
				apiKey = k
			}
			if b, ok := cfg.Providers.OpenAI["api_base"].(string); ok {
				apiBase = b
			}
		}
		if apiBase == "" {
			apiBase = "https://api.openai.com/v1"
		}
		return providers.NewOpenAIProvider(apiBase, apiKey, model)

	case "openrouter":
		if cfg.Providers.OpenRouter != nil {
			if k, ok := cfg.Providers.OpenRouter["api_key"].(string); ok {
				apiKey = k
			}
		}
		return providers.NewOpenRouterProvider(apiKey, model)

	case "anthropic":
		if cfg.Providers.Anthropic != nil {
			if k, ok := cfg.Providers.Anthropic["api_key"].(string); ok {
				apiKey = k
			}
		}
		// Anthropic uses OpenAI-compatible API
		if apiBase == "" {
			apiBase = "https://api.anthropic.com/v1"
		}
		return providers.NewOpenAIProvider(apiBase, apiKey, model)

	case "azure":
		if cfg.Providers.Azure != nil {
			if k, ok := cfg.Providers.Azure["api_key"].(string); ok {
				apiKey = k
			}
			if b, ok := cfg.Providers.Azure["api_base"].(string); ok {
				apiBase = b
			}
		}
		return providers.NewOpenAIProvider(apiBase, apiKey, model)

	case "minimax":
		if cfg.Providers.Minimax != nil {
			if k, ok := cfg.Providers.Minimax["api_key"].(string); ok {
				apiKey = k
			}
			if b, ok := cfg.Providers.Minimax["api_base"].(string); ok {
				apiBase = b
			}
		}
		return providers.NewMinimaxProvider(apiKey, apiBase, model)

	default:
		// Default to OpenRouter for "auto" or unknown
		if cfg.Providers.OpenRouter != nil {
			if k, ok := cfg.Providers.OpenRouter["api_key"].(string); ok {
				apiKey = k
			}
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENROUTER_API_KEY")
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		return providers.NewOpenRouterProvider(apiKey, model)
	}
}

func runAgentSingle(ctx context.Context, agentLoop *agent.AgentLoop, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) {
	messageBus := bus.New(100)
	agentLoop = agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

	// Parse session
	sessionKey := agentSessionFlag
	var chatID string
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		chatID = sessionKey[idx+1:]
	} else {
		chatID = sessionKey
	}

	inbound := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     chatID,
		Content:    agentMessageFlag,
		SessionKey: sessionKey,
	}
	// Start agent loop in background
	go func() {
		if err := agentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()

	messageBus.PublishInbound(inbound)

	// Wait for one response and print it.
	if msg, ok := <-messageBus.ConsumeOutbound(); ok {
		fmt.Println()
		fmt.Println(assistantLabelStyle.Render("nanobot:"))
		fmt.Println()
		if msg.Reasoning != "" {
			fmt.Println(reasoningStyle.Render(msg.Reasoning))
		}
		fmt.Print(renderMarkdown(msg.Content))
	}
}

func runAgentInteractive(ctx context.Context, agentLoop *agent.AgentLoop, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) {
	messageBus := bus.New(100)
	agentLoop = agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

	// Parse session
	sessionKey := agentSessionFlag
	var chatID string
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		chatID = sessionKey[idx+1:]
	} else {
		chatID = sessionKey
	}

	fmt.Printf("%s Interactive mode (type 'exit' or Ctrl+C to quit)\n\n", logo)

	// Start agent loop in background
	go func() {
		if err := agentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()

	// Create text input for line editing
	model := newInteractiveModel(messageBus, sessionKey, chatID)
	p := tea.NewProgram(model, tea.WithoutSignals())
	model.SetProgram(p)
	if _, err := p.Run(); err != nil {
		slog.Error("interactive mode error", "error", err)
	}
}
