package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"nanobot-go/internal/agent"
	appcore "nanobot-go/internal/app"
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

	// Create app to initialize providers/tools via plugin system
	appInstance, err := appcore.New(cfg)
	if err != nil {
		slog.Error("failed to create app", "error", err)
		os.Exit(1)
	}
	if err := appInstance.PluginRegistry.InitAll(ctx, appInstance); err != nil {
		slog.Error("failed to initialize plugins", "error", err)
		os.Exit(1)
	}

	sessionStore := appInstance.SessionStore
	toolRegistry := appInstance.ToolRegistry

	provider, err := appInstance.GetDefaultProvider()
	if err != nil {
		slog.Error("no provider available", "error", err)
		os.Exit(1)
	}

	maxIterations := cfg.Agents.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 40
	}

	if agentMessageFlag != "" {
		runAgentSingle(ctx, sessionStore, toolRegistry, provider, maxIterations)
	} else {
		runAgentInteractive(ctx, sessionStore, toolRegistry, provider, maxIterations)
	}
}

func runAgentSingle(ctx context.Context, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) {
	messageBus := bus.New(100)
	agentLoop := agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

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
			fmt.Println(renderReasoningMarkdown(msg.Reasoning))
		}
		fmt.Print(renderMarkdown(msg.Content))
	}
}

func runAgentInteractive(ctx context.Context, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) {
	messageBus := bus.New(100)
	agentLoop := agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

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
