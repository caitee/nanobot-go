package app

import (
	"context"
	"os"

	"ori/internal/bus"
	"ori/internal/channels"
	"ori/internal/config"
	"ori/internal/cron"
	"ori/internal/llm"
	"ori/internal/plugin"
	"ori/internal/providers"
	"ori/internal/tool"
	legacytools "ori/internal/tools"
)

// RegisterDefaults installs the stock set of providers, channels, and tools
// on the given plugin registry. Each plugin's Init decides whether to
// actually register itself (based on env / config) when the App starts.
func RegisterDefaults(reg *plugin.Registry) {
	reg.Register(&openaiProviderPlugin{})
	reg.Register(&anthropicProviderPlugin{})
	reg.Register(&azureProviderPlugin{})
	reg.Register(&minimaxProviderPlugin{})
	reg.Register(&openrouterProviderPlugin{})

	reg.Register(newChannelPlugin("telegram", "TELEGRAM_BOT_TOKEN", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewTelegramChannel(channels.TelegramConfig{Token: val}, b)
	}))
	reg.Register(newChannelPlugin("discord", "DISCORD_BOT_TOKEN", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewDiscordChannel(channels.DiscordConfig{Token: val}, b)
	}))
	reg.Register(newChannelPlugin("slack", "SLACK_BOT_TOKEN", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewSlackChannel(channels.SlackConfig{Token: val}, b)
	}))
	reg.Register(newChannelPlugin("feishu", "FEISHU_APP_ID", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewFeishuChannel(channels.FeishuConfig{AppID: val}, b)
	}))
	reg.Register(newChannelPlugin("dingtalk", "DINGTALK_CLIENT_ID", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewDingTalkChannel(channels.DingTalkConfig{ClientID: val}, b)
	}))
	reg.Register(newChannelPlugin("wecom", "WECOM_CORP_ID", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewWeComChannel(channels.WeComConfig{CorpID: val}, b)
	}))
	reg.Register(newChannelPlugin("qq", "QQ_APP_ID", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewQQChannel(channels.QQConfig{AppID: val}, b)
	}))
	reg.Register(newChannelPlugin("whatsapp", "WHATSAPP_BRIDGE_URL", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewWhatsAppChannel(channels.WhatsAppConfig{BridgeURL: val}, b)
	}))
	reg.Register(newChannelPlugin("email", "EMAIL_IMAP_HOST", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewEmailChannel(channels.EmailConfig{IMAPHost: val}, b)
	}))
	reg.Register(newChannelPlugin("matrix", "MATRIX_HOMESERVER", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewMatrixChannel(channels.MatrixConfig{Homeserver: val}, b)
	}))
	reg.Register(newChannelPlugin("mochat", "MOCHAT_API_URL", func(val string, b bus.MessageBus) channels.Channel {
		return channels.NewMochatChannel(channels.MochatConfig{APIURL: val}, b)
	}))

	reg.Register(newToolPlugin("message", "Send Message", func(_ context.Context, _ plugin.AppContext) (legacytools.Tool, error) {
		return legacytools.NewMessageTool(), nil
	}))
	reg.Register(newToolPlugin("read_file", "Read File", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		ws, allowed := filesystemDirs(appCtx)
		return legacytools.NewReadFileTool(ws, allowed), nil
	}))
	reg.Register(newToolPlugin("write_file", "Write File", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		ws, allowed := filesystemDirs(appCtx)
		return legacytools.NewWriteFileTool(ws, allowed), nil
	}))
	reg.Register(newToolPlugin("edit_file", "Edit File", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		ws, allowed := filesystemDirs(appCtx)
		return legacytools.NewEditFileTool(ws, allowed), nil
	}))
	reg.Register(newToolPlugin("list_dir", "List Directory", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		ws, allowed := filesystemDirs(appCtx)
		return legacytools.NewListDirTool(ws, allowed), nil
	}))
	reg.Register(newToolPlugin("find", "Find", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		ws, allowed := filesystemDirs(appCtx)
		return legacytools.NewGlobTool(ws, allowed), nil
	}))
	reg.Register(newToolPlugin("grep", "Grep", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		ws, allowed := filesystemDirs(appCtx)
		return legacytools.NewFindTool(ws, allowed), nil
	}))
	reg.Register(newAgentToolPlugin("shell", func(_ context.Context, appCtx plugin.AppContext) (tool.AgentTool, error) {
		cfg := appCtx.GetConfig().(*config.Config)
		if !cfg.Tools.Exec.Enabled {
			return nil, nil
		}
		var hook legacytools.ShellSpawnHook
		if len(cfg.Tools.Exec.Allow) > 0 || len(cfg.Tools.Exec.Deny) > 0 {
			hook = legacytools.AllowDenyHook(cfg.Tools.Exec.Allow, cfg.Tools.Exec.Deny)
		}
		return legacytools.NewShellTool(legacytools.ShellToolOptions{
			SpawnHook: hook,
		}), nil
	}))
	reg.Register(newToolPlugin("web", "Web", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		cfg := appCtx.GetConfig().(*config.Config)
		webCfg := cfg.Tools.Web
		return legacytools.NewWebToolWithConfig(&legacytools.WebSearchConfig{
			Provider:   webCfg.SearchProvider,
			APIKey:     webCfg.SearchAPIKey,
			BaseURL:    webCfg.SearchBaseURL,
			MaxResults: webCfg.SearchMaxResults,
		}), nil
	}))
	reg.Register(newToolPlugin("cron", "Cron", func(_ context.Context, appCtx plugin.AppContext) (legacytools.Tool, error) {
		msgBus := appCtx.GetBus().(bus.MessageBus)
		cronSvc := appCtx.GetCronService().(*cron.CronService)
		return legacytools.NewCronTool(cronSvc, msgBus), nil
	}))
	// Spawn tool: the legacy implementation expects a SubagentSpawner at
	// construction time. We pass nil and App.Start later swaps it via the
	// spawnAdapter below. Keeping the tool inside the registry is still
	// valuable because the model sees its schema immediately.
	reg.Register(newToolPlugin("spawn", "Spawn", func(_ context.Context, _ plugin.AppContext) (legacytools.Tool, error) {
		return legacytools.NewSpawnTool(nil), nil
	}))
	reg.Register(&mcpToolPlugin{})
}

// channelPlugin is a generic plugin for channels that check a single env var.
type channelPlugin struct {
	name    string
	envVar  string
	factory func(string, bus.MessageBus) channels.Channel
}

func newChannelPlugin(name, envVar string, factory func(string, bus.MessageBus) channels.Channel) *channelPlugin {
	return &channelPlugin{name: name, envVar: envVar, factory: factory}
}

func (p *channelPlugin) Name() string      { return "channel." + p.name }
func (p *channelPlugin) Type() plugin.Type { return plugin.TypeChannel }
func (p *channelPlugin) Close() error      { return nil }
func (p *channelPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	val := os.Getenv(p.envVar)
	if val == "" {
		return nil
	}
	msgBus := appCtx.GetBus().(bus.MessageBus)
	mgr := appCtx.GetChannelManager().(*channels.Manager)
	mgr.Register(p.factory(val, msgBus))
	return nil
}

// toolPlugin wraps a legacy tool factory and registers the produced tool as
// an AgentTool via tool.FromLegacy. The legacy tool's runtime behavior is
// preserved unchanged; only the interface shape is adapted.
type toolPlugin struct {
	name    string
	label   string
	factory func(context.Context, plugin.AppContext) (legacytools.Tool, error)
}

// mcpToolPlugin registers the native MCP proxy plus any cached direct MCP
// tools. The runtime still sees ordinary AgentTool values.
type mcpToolPlugin struct {
	manager *legacytools.MCPManager
}

func (p *mcpToolPlugin) Name() string                     { return "tool.mcp" }
func (p *mcpToolPlugin) Type() plugin.Type                { return plugin.TypeTool }
func (p *mcpToolPlugin) Manager() *legacytools.MCPManager { return p.manager }
func (p *mcpToolPlugin) Close() error {
	if p.manager == nil {
		return nil
	}
	return p.manager.Close()
}
func (p *mcpToolPlugin) Init(ctx context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	workspace := cfg.Agents.Workspace
	if workspace == "" {
		workspace = "."
	}
	mcpCfg, err := legacytools.LoadMCPConfig(legacytools.MCPConfigLoadOptions{
		Workspace: workspace,
		Inline:    cfg.Tools.MCP,
	})
	if err != nil {
		return err
	}
	cache, err := legacytools.LoadMCPMetadataCache(mcpCfg.Settings.CachePath)
	if err != nil {
		return err
	}
	p.manager = legacytools.NewMCPManager(legacytools.MCPManagerOptions{
		Config:    mcpCfg,
		Cache:     cache,
		CachePath: mcpCfg.Settings.CachePath,
	})
	if err := p.manager.Start(ctx); err != nil {
		return err
	}

	reg := appCtx.GetToolRegistry().(tool.Registry)
	reg.Register(legacytools.NewMCPProxyTool(p.manager))
	for _, direct := range p.manager.DirectTools() {
		reg.Register(direct)
	}
	return nil
}

func newToolPlugin(name, label string, factory func(context.Context, plugin.AppContext) (legacytools.Tool, error)) *toolPlugin {
	return &toolPlugin{name: name, label: label, factory: factory}
}

func (p *toolPlugin) Name() string      { return "tool." + p.name }
func (p *toolPlugin) Type() plugin.Type { return plugin.TypeTool }
func (p *toolPlugin) Close() error      { return nil }
func (p *toolPlugin) Init(ctx context.Context, appCtx plugin.AppContext) error {
	legacy, err := p.factory(ctx, appCtx)
	if err != nil {
		return err
	}
	if legacy == nil {
		return nil
	}
	reg := appCtx.GetToolRegistry().(tool.Registry)
	reg.Register(tool.FromLegacy(legacy, p.label))
	return nil
}

// filesystemDirs resolves (workspace, allowedDir) from config for file tools.
// Falls back to empty strings when no allowed dirs are configured.
func filesystemDirs(appCtx plugin.AppContext) (workspace, allowedDir string) {
	cfg := appCtx.GetConfig().(*config.Config)
	dirs := cfg.Tools.Workspace.AllowedDirs
	if len(dirs) == 0 {
		return "", ""
	}
	return dirs[0], dirs[0]
}

// agentToolPlugin registers a tool that already implements tool.AgentTool
// directly, bypassing the legacy adapter. Use this for tools that need
// streaming output, Result terminate hints, or other features the legacy
// interface cannot express.
type agentToolPlugin struct {
	name    string
	factory func(context.Context, plugin.AppContext) (tool.AgentTool, error)
}

func newAgentToolPlugin(name string, factory func(context.Context, plugin.AppContext) (tool.AgentTool, error)) *agentToolPlugin {
	return &agentToolPlugin{name: name, factory: factory}
}

func (p *agentToolPlugin) Name() string      { return "tool." + p.name }
func (p *agentToolPlugin) Type() plugin.Type { return plugin.TypeTool }
func (p *agentToolPlugin) Close() error      { return nil }
func (p *agentToolPlugin) Init(ctx context.Context, appCtx plugin.AppContext) error {
	t, err := p.factory(ctx, appCtx)
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	reg := appCtx.GetToolRegistry().(tool.Registry)
	reg.Register(t)
	return nil
}

// --- Provider plugins ---
//
// Each plugin reads its config slot + env var, constructs the legacy provider,
// wraps it with llm.FromLegacy, and registers it directly in llm.Registry.

type openaiProviderPlugin struct{}

func (p *openaiProviderPlugin) Name() string      { return "provider.openai" }
func (p *openaiProviderPlugin) Type() plugin.Type { return plugin.TypeProvider }
func (p *openaiProviderPlugin) Close() error      { return nil }
func (p *openaiProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	llmReg := appCtx.GetLLMRegistry().(*llm.Registry)

	var apiKey, apiBase string
	if cfg.Providers.OpenAI != nil {
		apiKey, _ = cfg.Providers.OpenAI["api_key"].(string)
		apiBase, _ = cfg.Providers.OpenAI["api_base"].(string)
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil
	}
	if apiBase == "" {
		apiBase = "https://api.openai.com/v1"
	}

	model := cfg.Agents.Model
	if model == "" {
		model = "gpt-4"
	}
	legacy := providers.NewOpenAIProvider(apiBase, apiKey, model)
	llmReg.Register("openai", llm.FromLegacy(legacy), "plugin:openai")
	return nil
}

type anthropicProviderPlugin struct{}

func (p *anthropicProviderPlugin) Name() string      { return "provider.anthropic" }
func (p *anthropicProviderPlugin) Type() plugin.Type { return plugin.TypeProvider }
func (p *anthropicProviderPlugin) Close() error      { return nil }
func (p *anthropicProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	llmReg := appCtx.GetLLMRegistry().(*llm.Registry)

	var apiKey, apiBase string
	if cfg.Providers.Anthropic != nil {
		apiKey, _ = cfg.Providers.Anthropic["api_key"].(string)
		apiBase, _ = cfg.Providers.Anthropic["api_base"].(string)
	}
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil
	}
	if apiBase == "" {
		apiBase = "https://api.anthropic.com"
	}

	model := cfg.Agents.Model
	if model == "" {
		model = "claude-opus-4-5"
	}
	legacy := providers.NewAnthropicProvider(apiKey, apiBase, model)
	llmReg.Register("anthropic", llm.FromLegacy(legacy), "plugin:anthropic")
	return nil
}

type azureProviderPlugin struct{}

func (p *azureProviderPlugin) Name() string      { return "provider.azure" }
func (p *azureProviderPlugin) Type() plugin.Type { return plugin.TypeProvider }
func (p *azureProviderPlugin) Close() error      { return nil }
func (p *azureProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	llmReg := appCtx.GetLLMRegistry().(*llm.Registry)

	var apiKey, apiBase string
	if cfg.Providers.Azure != nil {
		apiKey, _ = cfg.Providers.Azure["api_key"].(string)
		apiBase, _ = cfg.Providers.Azure["api_base"].(string)
	}
	if apiKey == "" {
		return nil
	}

	model := cfg.Agents.Model
	if model == "" {
		model = "gpt-4"
	}
	apiVersion := ""
	if cfg.Providers.Azure != nil {
		apiVersion, _ = cfg.Providers.Azure["api_version"].(string)
	}
	legacy := providers.NewAzureProvider(apiBase, apiKey, apiVersion, model)
	llmReg.Register("azure", llm.FromLegacy(legacy), "plugin:azure")
	return nil
}

type minimaxProviderPlugin struct{}

func (p *minimaxProviderPlugin) Name() string      { return "provider.minimax" }
func (p *minimaxProviderPlugin) Type() plugin.Type { return plugin.TypeProvider }
func (p *minimaxProviderPlugin) Close() error      { return nil }
func (p *minimaxProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	llmReg := appCtx.GetLLMRegistry().(*llm.Registry)

	var apiKey, apiBase string
	if cfg.Providers.Minimax != nil {
		apiKey, _ = cfg.Providers.Minimax["api_key"].(string)
		apiBase, _ = cfg.Providers.Minimax["api_base"].(string)
	}
	if apiKey == "" {
		return nil
	}

	model := cfg.Agents.Model
	if model == "" {
		model = "MiniMax-M2.5"
	}
	legacy := providers.NewMinimaxProvider(apiKey, apiBase, model)
	llmReg.Register("minimax", llm.FromLegacy(legacy), "plugin:minimax")
	return nil
}

type openrouterProviderPlugin struct{}

func (p *openrouterProviderPlugin) Name() string      { return "provider.openrouter" }
func (p *openrouterProviderPlugin) Type() plugin.Type { return plugin.TypeProvider }
func (p *openrouterProviderPlugin) Close() error      { return nil }
func (p *openrouterProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	llmReg := appCtx.GetLLMRegistry().(*llm.Registry)

	var apiKey string
	if cfg.Providers.OpenRouter != nil {
		apiKey, _ = cfg.Providers.OpenRouter["api_key"].(string)
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if apiKey == "" {
		return nil
	}

	model := cfg.Agents.Model
	if model == "" {
		model = "anthropic/claude-opus-4-5"
	}
	legacy := providers.NewOpenRouterProvider(apiKey, model)
	llmReg.Register("openrouter", llm.FromLegacy(legacy), "plugin:openrouter")
	return nil
}
