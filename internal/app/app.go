package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"ori/internal/bus"
	"ori/internal/channels"
	"ori/internal/config"
	"ori/internal/cron"
	"ori/internal/llm"
	"ori/internal/plugin"
	"ori/internal/providers"
	"ori/internal/runtime"
	"ori/internal/session"
	"ori/internal/skills"
	"ori/internal/tool"
	legacytools "ori/internal/tools"
)

// App is the top-level container. It owns shared infrastructure
// (bus / session / registries / channels / cron) and the Dispatcher that
// wraps the new runtime.Agent.
type App struct {
	Config *config.Config
	Bus    bus.MessageBus

	SessionStore session.SessionStore
	SkillLoader  *skills.SkillLoader
	ToolRegistry tool.Registry
	LLMRegistry  *llm.Registry
	Management   *ManagementService
	MCPManager   *legacytools.MCPManager

	// LegacyProviderRegistry is kept through M5 so plugin code registered
	// under the old API can still add providers; the bridge adapts them
	// into the new Registry on App.Start.
	LegacyProviderRegistry *providers.Registry

	ChannelManager *channels.Manager
	CronService    *cron.CronService
	PluginRegistry *plugin.Registry

	Dispatcher *Dispatcher
	Subagents  *SubagentManager

	ctx    context.Context
	cancel context.CancelFunc
}

// New constructs a fresh App with all registries empty.
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

	workspace := cfg.Agents.Workspace
	if workspace == "" {
		workspace = "."
	}

	a := &App{
		Config:                 cfg,
		Bus:                    messageBus,
		SessionStore:           sessionStore,
		SkillLoader:            skills.NewSkillLoader(filepath.Join(workspace, "skills"), ""),
		ToolRegistry:           tool.NewRegistry(),
		LLMRegistry:            llm.NewRegistry(),
		LegacyProviderRegistry: providers.NewRegistry(),
		ChannelManager:         channels.NewManager(messageBus),
		CronService:            cronService,
		PluginRegistry:         plugin.NewRegistry(),
		ctx:                    ctx,
		cancel:                 cancel,
	}
	a.SkillLoader.SetDisabled(cfg.Skills.Disabled)

	RegisterDefaults(a.PluginRegistry)
	return a, nil
}

// Start initializes plugins, builds the dispatcher, wires the subagent
// manager, starts channels / cron, and begins consuming inbound messages.
func (a *App) Start(ctx context.Context) error {
	slog.Info("starting ori application")

	if err := a.PluginRegistry.InitAll(ctx, a); err != nil {
		return fmt.Errorf("failed to initialize plugins: %w", err)
	}
	if p, ok := a.PluginRegistry.Get("tool.mcp"); ok {
		if mcpPlugin, ok := p.(*mcpToolPlugin); ok {
			a.MCPManager = mcpPlugin.manager
		}
	}

	// Bridge every legacy provider into the new llm.Registry.
	// This is kept as a short-term transition path for any providers that
	// still register via LegacyProviderRegistry.
	for _, name := range a.LegacyProviderRegistry.List() {
		p, err := a.LegacyProviderRegistry.Get(name)
		if err != nil {
			continue
		}
		a.LLMRegistry.Register(name, llm.FromLegacy(p), "legacy:"+name)
	}

	providerName, err := a.defaultProviderName()
	if err != nil {
		return err
	}
	_, err = a.LLMRegistry.Get(providerName)
	if err != nil {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	modelName := a.Config.Agents.Model
	if modelName == "" {
		// Use a sensible default when no model is configured
		modelName = "gpt-4"
	}
	model := llm.Model{
		ID:        modelName,
		Name:      modelName,
		Provider:  providerName,
		Reasoning: a.Config.Agents.EnableReasoning,
	}

	workspace := a.Config.Agents.Workspace
	if workspace == "" {
		workspace = "."
	}

	// Subagents share the same tool registry minus "spawn" to avoid
	// recursive spawning, and run against the same llm.StreamFn.
	streamFn := a.LLMRegistry.StreamFnFor(providerName)
	subTools := filterTools(a.ToolRegistry.All(), func(t tool.AgentTool) bool {
		return t.Name() != "spawn"
	})
	a.Subagents = NewSubagentManager(streamFn, model, subTools, workspace, a.Bus)

	// Wire the spawn tool now that the manager exists.
	if spawnTool, ok := a.ToolRegistry.Get("spawn"); ok {
		if legacy, ok := tool.UnwrapLegacy(spawnTool); ok {
			if st, ok := legacy.(*legacytools.SpawnTool); ok {
				st.SetSpawner(a.Subagents)
			}
		}
	}

	// Build the dispatcher last so the subagent spawner is ready.
	systemPrompt := buildSystemPrompt(workspace, a.SkillLoader)
	a.Management = NewManagementService(ManagementOptions{
		Config:       a.Config,
		SkillLoader:  a.SkillLoader,
		MCPManager:   a.MCPManager,
		ToolRegistry: a.ToolRegistry,
	})
	a.Dispatcher = NewDispatcher(DispatcherOptions{
		Bus:              a.Bus,
		SessionStore:     a.SessionStore,
		ToolRegistry:     a.ToolRegistry,
		StreamFn:         streamFn,
		Model:            model,
		Temperature:      a.Config.Agents.Temperature,
		MaxTokens:        a.Config.Agents.MaxTokens,
		EnableReasoning:  a.Config.Agents.EnableReasoning,
		ReasoningEffort:  a.Config.Agents.ReasoningEffort,
		SkillLoader:      a.SkillLoader,
		Management:       a.Management,
		SystemPrompt:     systemPrompt,
		TransformContext: runtime.RuntimeContextTransform(runtime.RuntimeContext{}),
		Subagents:        a.Subagents,
	})
	a.Management.SetHotApply(a.applyHotConfig)

	if err := a.CronService.Start(ctx); err != nil {
		slog.Error("failed to start cron service", "error", err)
	}
	if err := a.ChannelManager.StartAll(ctx); err != nil {
		slog.Error("failed to start channels", "error", err)
	}

	go func() {
		if err := a.Dispatcher.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("dispatcher error", "error", err)
		}
	}()

	slog.Info("ori application started", "provider", providerName, "model", modelName)
	return nil
}

func (a *App) applyHotConfig() error {
	providerName, err := a.defaultProviderName()
	if err != nil {
		return err
	}
	if _, err := a.LLMRegistry.Get(providerName); err != nil {
		return fmt.Errorf("provider not found: %s", providerName)
	}
	modelName := a.Config.Agents.Model
	if modelName == "" {
		modelName = "gpt-4"
	}
	model := llm.Model{
		ID:        modelName,
		Name:      modelName,
		Provider:  providerName,
		Reasoning: a.Config.Agents.EnableReasoning,
	}
	if a.Dispatcher != nil {
		workspace := a.Config.Agents.Workspace
		if workspace == "" {
			workspace = "."
		}
		a.Dispatcher.ApplyRuntimeSettings(
			a.LLMRegistry.StreamFnFor(providerName),
			model,
			a.Config.Agents.EnableReasoning,
			a.Config.Agents.ReasoningEffort,
			a.Config.Agents.Temperature,
			a.Config.Agents.MaxTokens,
		)
		a.Dispatcher.SetSystemPrompt(buildSystemPrompt(workspace, a.SkillLoader))
	}
	return nil
}

func buildSystemPrompt(workspace string, loader *skills.SkillLoader) string {
	builder := runtime.NewSystemPromptBuilder(workspace)
	if loader != nil {
		builder.Fragments = append(builder.Fragments,
			runtime.PromptFragmentFunc(loader.BuildSkillsSummary),
			runtime.PromptFragmentFunc(func() string {
				content := strings.TrimSpace(loader.LoadSkillsForContext(loader.GetAlwaysSkills()))
				if content == "" {
					return ""
				}
				return "## Always-On Skills\n\n" + content
			}),
		)
	}
	return builder.Build()
}

// Stop performs graceful shutdown.
func (a *App) Stop() error {
	slog.Info("stopping ori application")
	a.cancel()
	a.ChannelManager.StopAll()
	a.CronService.Stop()
	a.Bus.Close()
	if err := a.PluginRegistry.CloseAll(); err != nil {
		slog.Error("error closing plugins", "error", err)
	}
	slog.Info("ori application stopped")
	return nil
}

// defaultProviderName resolves the configured provider, falling back to the
// first registered provider when set to "auto" or empty.
func (a *App) defaultProviderName() (string, error) {
	name := a.Config.Agents.Provider
	if name != "" && name != "auto" {
		return name, nil
	}
	names := a.LLMRegistry.List()
	if len(names) == 0 {
		return "", fmt.Errorf("no providers registered")
	}
	return names[0], nil
}

// GetDefaultProvider returns the legacy LLMProvider for the default
// provider name. Kept for compatibility with callers that still need the
// legacy interface (heartbeat adapter, memory consolidator).
func (a *App) GetDefaultProvider() (providers.LLMProvider, error) {
	name, err := a.defaultProviderName()
	if err != nil {
		return nil, err
	}
	return a.LegacyProviderRegistry.Get(name)
}

// plugin.AppContext glue -------------------------------------------------

func (a *App) GetConfig() any           { return a.Config }
func (a *App) GetBus() any              { return a.Bus }
func (a *App) GetSessionStore() any     { return a.SessionStore }
func (a *App) GetToolRegistry() any     { return a.ToolRegistry }
func (a *App) GetProviderRegistry() any { return a.LegacyProviderRegistry }
func (a *App) GetLLMRegistry() any      { return a.LLMRegistry }
func (a *App) GetChannelManager() any   { return a.ChannelManager }
func (a *App) GetCronService() any      { return a.CronService }

// Context returns the App-level cancellable context.
func (a *App) Context() context.Context { return a.ctx }

// Done is the App-level shutdown signal.
func (a *App) Done() <-chan struct{} { return a.ctx.Done() }

func filterTools(in []tool.AgentTool, keep func(tool.AgentTool) bool) []tool.AgentTool {
	out := make([]tool.AgentTool, 0, len(in))
	for _, t := range in {
		if keep(t) {
			out = append(out, t)
		}
	}
	return out
}
