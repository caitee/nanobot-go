package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appcore "ori/internal/app"
	"ori/internal/bus"
	"ori/internal/config"

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
	agentCmd.Flags().StringVarP(&agentSessionFlag, "session", "s", "", "Session ID (default: generate unique session per run)")
	agentCmd.Flags().StringVarP(&agentWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	agentCmd.Flags().StringVarP(&agentConfigFlag, "config", "c", "", "Config file path")
	agentCmd.Flags().BoolVarP(&agentMarkdownFlag, "markdown", "", true, "Render assistant output as Markdown")
	agentCmd.Flags().BoolVarP(&agentLogsFlag, "logs", "", false, "Show ori runtime logs during chat")
}

// generateSessionKey creates a unique session key for this invocation.
func generateSessionKey() string {
	return fmt.Sprintf("cli:session-%d", time.Now().UnixNano())
}

// resolveSessionKey returns the session key to use: if the user provided
// an explicit -s value, use it; otherwise generate a fresh session.
func resolveSessionKey(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return generateSessionKey()
}

func runAgent(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
		workspace = filepath.Join(homeDir, ".ori", "workspace")
	}
	cfg.Agents.Workspace = workspace

	app, err := appcore.New(cfg)
	if err != nil {
		slog.Error("failed to create app", "error", err)
		os.Exit(1)
	}
	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start app", "error", err)
		os.Exit(1)
	}
	defer app.Stop()

	sessionKey := resolveSessionKey(agentSessionFlag)
	chatID := sessionKey
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		chatID = sessionKey[idx+1:]
	}

	if agentMessageFlag != "" {
		runAgentSingle(ctx, app, sessionKey, chatID)
		return
	}
	runAgentInteractive(ctx, app, sessionKey, chatID)
}

// runAgentSingle sends one prompt through the dispatcher, waits for the
// outbound response, and prints it to stdout.
func runAgentSingle(ctx context.Context, app *appcore.App, sessionKey, chatID string) {
	outbound := app.Bus.ConsumeOutbound()

	app.Bus.PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     chatID,
		Content:    agentMessageFlag,
		SessionKey: sessionKey,
	})

	select {
	case msg, ok := <-outbound:
		if !ok {
			return
		}
		fmt.Println()
		fmt.Println(assistantLabelStyle.Render("ori:"))
		fmt.Println()
		if msg.Reasoning != "" {
			fmt.Println(renderReasoningMarkdown(msg.Reasoning))
		}
		fmt.Print(renderMarkdown(msg.Content))
	case <-ctx.Done():
	}
}

func runAgentInteractive(ctx context.Context, app *appcore.App, sessionKey, chatID string) {
	fmt.Printf("%s Interactive mode (type 'exit' or Ctrl+C to quit)\n\n", logo)

	model := newInteractiveModel(app.Dispatcher, app.Bus, sessionKey, chatID)
	p := tea.NewProgram(model, tea.WithoutSignals())
	model.SetProgram(p)
	if _, err := p.Run(); err != nil {
		slog.Error("interactive mode error", "error", err)
	}
}
