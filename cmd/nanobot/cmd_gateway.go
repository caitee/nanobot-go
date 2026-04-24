package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"nanobot-go/internal/agent"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/channels"
	"nanobot-go/internal/config"
	"nanobot-go/internal/cron"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"

	"github.com/spf13/cobra"
)

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Start the nanobot gateway server",
	Run:   runGateway,
}

var (
	gatewayPortFlag      int
	gatewayWorkspaceFlag string
	gatewayVerboseFlag   bool
	gatewayConfigFlag    string
)

func init() {
	gatewayCmd.Flags().IntVarP(&gatewayPortFlag, "port", "p", 0, "Gateway port")
	gatewayCmd.Flags().StringVarP(&gatewayWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	gatewayCmd.Flags().BoolVarP(&gatewayVerboseFlag, "verbose", "v", false, "Verbose output")
	gatewayCmd.Flags().StringVarP(&gatewayConfigFlag, "config", "c", "", "Config file path")
}

func runGateway(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set log level
	if gatewayVerboseFlag {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
	}

	cfg, err := config.Load(gatewayConfigFlag)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override port if specified
	port := gatewayPortFlag
	if port == 0 {
		port = cfg.Gateway.Port
	}

	// Override workspace if specified
	workspace := gatewayWorkspaceFlag
	if workspace == "" {
		workspace = cfg.Agents.Workspace
	}
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".nanobot", "workspace")
	}

	// Create workspace directory
	if err := os.MkdirAll(workspace, 0755); err != nil {
		slog.Error("failed to create workspace", "error", err)
		os.Exit(1)
	}

	// Create session store
	sessionStore, err := session.NewFileSessionStore(filepath.Join(workspace, "sessions"))
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	// Create message bus
	messageBus := bus.New(100)

	// Create cron service
	cronStorePath := filepath.Join(workspace, "data", "cron", "jobs.json")
	cronService := cron.NewCronService(cronStorePath, func(job *cron.CronJob) {
		slog.Info("cron job executing", "name", job.Name)
		msg := bus.InboundMessage{
			Channel:    job.Payload.Channel,
			ChatID:     job.Payload.To,
			Content:    job.Payload.Message,
			SessionKey: fmt.Sprintf("%s:%s", job.Payload.Channel, job.Payload.To),
		}
		messageBus.PublishInbound(msg)
	})

	// Create tool registry
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(tools.NewMessageTool())
	toolRegistry.Register(tools.NewFilesystemTool(nil))
	toolRegistry.Register(tools.NewShellTool(true, nil, nil))
	toolRegistry.Register(tools.NewWebTool())
	toolRegistry.Register(tools.NewCronTool(cronService, messageBus))
	toolRegistry.Register(tools.NewSpawnTool(nil))

	// Create provider
	provider := createProvider(cfg)

	// Create agent loop
	maxIterations := cfg.Agents.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 40
	}
	agentLoop := agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations, false)

	// Create channel manager
	channelManager := channels.NewManager(messageBus)

	// Handle shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
		channelManager.StopAll()
		cronService.Stop()
		messageBus.Close()
	}()

	// Start cron service
	if err := cronService.Start(ctx); err != nil {
		slog.Error("failed to start cron service", "error", err)
	}

	// Start channel manager
	if err := channelManager.StartAll(ctx); err != nil {
		slog.Error("failed to start channels", "error", err)
	}

	// Start agent loop
	go func() {
		if err := agentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()

	gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, port)
	slog.Info("gateway starting", "address", gatewayAddr)
	fmt.Printf("%s Starting nanobot gateway v%s on port %d...\n", logo, version, port)

	<-ctx.Done()
	slog.Info("gateway stopped")
}
