package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"nanobot-go/internal/agent"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/channels"
	"nanobot-go/internal/config"
	"nanobot-go/internal/cron"
	"nanobot-go/internal/plugin"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"
)

// App holds all application components and implements plugin.AppContext.
type App struct {
	Config          *config.Config
	Bus             bus.MessageBus
	SessionStore    session.SessionStore
	ToolRegistry    tools.ToolRegistry
	ProviderRegistry *providers.Registry
	ChannelManager  *channels.Manager
	CronService     *cron.CronService
	PluginRegistry  *plugin.Registry
	AgentLoop       *agent.AgentLoop
	SubagentManager *agent.SubagentManager

	ctx    context.Context
	cancel context.CancelFunc
}

func New(cfg *config.Config) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())

	messageBus := bus.New(100)

	sessionDir := "sessions"
	if cfg.Agents.Workspace != "" {
		sessionDir = filepath.Join(cfg.Agents.Workspace, "sessions")
	}
	sessionStore, err := session.NewFileSessionStore(sessionDir)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create session store: %w", err)
	}

	toolRegistry := tools.NewRegistry()
	providerRegistry := providers.NewRegistry()
	channelManager := channels.NewManager(messageBus)

	cronDataDir := "data/cron"
	if cfg.Agents.Workspace != "" {
		cronDataDir = filepath.Join(cfg.Agents.Workspace, "data", "cron")
	}
	cronJobsFile := filepath.Join(cronDataDir, "jobs.json")
	cronService := cron.NewCronService(cronJobsFile, func(job *cron.CronJob) {
		slog.Info("cron job executing", "name", job.Name)
		msg := bus.InboundMessage{
			Channel:    job.Payload.Channel,
			ChatID:     job.Payload.To,
			Content:    job.Payload.Message,
			SessionKey: fmt.Sprintf("%s:%s", job.Payload.Channel, job.Payload.To),
		}
		messageBus.PublishInbound(msg)
	})

	pluginRegistry := plugin.NewRegistry()

	app := &App{
		Config:           cfg,
		Bus:              messageBus,
		SessionStore:     sessionStore,
		ToolRegistry:     toolRegistry,
		ProviderRegistry: providerRegistry,
		ChannelManager:   channelManager,
		CronService:      cronService,
		PluginRegistry:   pluginRegistry,
		ctx:              ctx,
		cancel:           cancel,
	}

	RegisterDefaults(app.PluginRegistry)

	return app, nil
}

func (a *App) Start(ctx context.Context) error {
	slog.Info("starting nanobot application")

	if err := a.PluginRegistry.InitAll(ctx, a); err != nil {
		return fmt.Errorf("failed to initialize plugins: %w", err)
	}

	defaultProvider, err := a.GetDefaultProvider()
	if err != nil {
		return fmt.Errorf("failed to get default provider: %w", err)
	}

	workspace := a.Config.Agents.Workspace
	if workspace == "" {
		workspace = "."
	}
	maxIterations := a.Config.Agents.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 40
	}
	a.SubagentManager = agent.NewSubagentManager(
		defaultProvider,
		workspace,
		a.Bus,
		a.Config.Agents.Model,
		maxIterations,
	)

	a.AgentLoop = agent.NewAgentLoop(
		a.Bus,
		a.SessionStore,
		a.ToolRegistry,
		defaultProvider,
		maxIterations,
		a.Config.Agents.EnableReasoning,
	)

	if err := a.CronService.Start(ctx); err != nil {
		slog.Error("failed to start cron service", "error", err)
	}

	if err := a.ChannelManager.StartAll(ctx); err != nil {
		slog.Error("failed to start channels", "error", err)
	}

	go func() {
		if err := a.AgentLoop.Start(ctx); err != nil && err != context.Canceled {
			slog.Error("agent loop error", "error", err)
		}
	}()

	slog.Info("nanobot application started")
	return nil
}

func (a *App) Stop() error {
	slog.Info("stopping nanobot application")

	a.cancel()
	a.ChannelManager.StopAll()
	a.CronService.Stop()
	a.Bus.Close()

	if err := a.PluginRegistry.CloseAll(); err != nil {
		slog.Error("error closing plugins", "error", err)
	}

	slog.Info("nanobot application stopped")
	return nil
}

func (a *App) GetDefaultProvider() (providers.LLMProvider, error) {
	providerName := a.Config.Agents.Provider
	if providerName == "" || providerName == "auto" {
		// Try to find any registered provider
		names := a.ProviderRegistry.List()
		if len(names) == 0 {
			return nil, fmt.Errorf("no providers registered")
		}
		providerName = names[0]
	}

	provider, err := a.ProviderRegistry.Get(providerName)
	if err != nil {
		return nil, fmt.Errorf("provider not found: %s", providerName)
	}

	return provider, nil
}

func (a *App) GetConfig() any {
	return a.Config
}

func (a *App) GetBus() any {
	return a.Bus
}

func (a *App) GetSessionStore() any {
	return a.SessionStore
}

func (a *App) GetToolRegistry() any {
	return a.ToolRegistry
}

func (a *App) GetProviderRegistry() any {
	return a.ProviderRegistry
}

func (a *App) GetChannelManager() any {
	return a.ChannelManager
}

func (a *App) GetCronService() any {
	return a.CronService
}

func (a *App) Context() context.Context {
	return a.ctx
}

func (a *App) Done() <-chan struct{} {
	return a.ctx.Done()
}
