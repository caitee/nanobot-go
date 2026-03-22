package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"nanobot-go/internal/agent"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/channels"
	"nanobot-go/internal/config"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	cfg, err := config.Load("")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize message bus
	messageBus := bus.New(100)

	// Initialize session store
	sessionStore, err := session.NewFileSessionStore("sessions")
	if err != nil {
		slog.Error("failed to create session store", "error", err)
		os.Exit(1)
	}

	// Initialize tool registry
	toolRegistry := tools.NewRegistry()
	toolRegistry.Register(tools.NewMessageTool())
	toolRegistry.Register(tools.NewFilesystemTool(nil))
	toolRegistry.Register(tools.NewShellTool(true, nil, nil))
	toolRegistry.Register(tools.NewWebTool())
	toolRegistry.Register(tools.NewCronTool())
	toolRegistry.Register(tools.NewSpawnTool())
	toolRegistry.Register(tools.NewMCPTool())

	// Initialize provider
	provider := providers.NewOpenAIProvider(
		"https://api.openai.com/v1",
		os.Getenv("OPENAI_API_KEY"),
		"gpt-4",
	)

	// Initialize channel manager
	channelManager := channels.NewManager(messageBus)

	// Register channels based on config
	// (In real implementation, would check cfg.Channels for each enabled channel)

	// Initialize agent loop
	maxIterations := 10
	if cfg.Agents.MaxToolIterations > 0 {
		maxIterations = cfg.Agents.MaxToolIterations
	}
	agentLoop := agent.NewAgentLoop(messageBus, sessionStore, toolRegistry, provider, maxIterations)

	// Handle shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
		channelManager.StopAll()
		messageBus.Close()
	}()

	// Start channel manager
	if err := channelManager.StartAll(ctx); err != nil {
		slog.Error("failed to start channels", "error", err)
	}

	// Start gateway
	gatewayAddr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)
	slog.Info("gateway starting", "address", gatewayAddr)

	// Start agent loop in background
	go func() {
		if err := agentLoop.Start(ctx); err != nil {
			slog.Error("agent loop error", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("gateway stopped")
}
