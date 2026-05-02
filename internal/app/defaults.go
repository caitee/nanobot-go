package app

import (
	"context"
	"os"

	"nanobot-go/internal/bus"
	"nanobot-go/internal/channels"
	"nanobot-go/internal/config"
	"nanobot-go/internal/cron"
	"nanobot-go/internal/plugin"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/tools"
)

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

	reg.Register(newToolPlugin("message", func(_ context.Context, _ plugin.AppContext) (tools.Tool, error) {
		return tools.NewMessageTool(), nil
	}))
	reg.Register(newToolPlugin("filesystem", func(_ context.Context, appCtx plugin.AppContext) (tools.Tool, error) {
		cfg := appCtx.GetConfig().(*config.Config)
		return tools.NewFilesystemTool(cfg.Tools.Workspace.AllowedDirs), nil
	}))
	reg.Register(newToolPlugin("shell", func(_ context.Context, appCtx plugin.AppContext) (tools.Tool, error) {
		cfg := appCtx.GetConfig().(*config.Config)
		return tools.NewShellTool(cfg.Tools.Exec.Enabled, cfg.Tools.Exec.Allow, cfg.Tools.Exec.Deny), nil
	}))
	reg.Register(newToolPlugin("web", func(_ context.Context, _ plugin.AppContext) (tools.Tool, error) {
		return tools.NewWebTool(), nil
	}))
	reg.Register(newToolPlugin("cron", func(_ context.Context, appCtx plugin.AppContext) (tools.Tool, error) {
		msgBus := appCtx.GetBus().(bus.MessageBus)
		cronSvc := appCtx.GetCronService().(*cron.CronService)
		return tools.NewCronTool(cronSvc, msgBus), nil
	}))
	// Spawn tool gets nil manager; App.Start() wires up the subagent manager later.
	reg.Register(newToolPlugin("spawn", func(_ context.Context, _ plugin.AppContext) (tools.Tool, error) {
		return tools.NewSpawnTool(nil), nil
	}))
	reg.Register(newToolPlugin("mcp", func(_ context.Context, _ plugin.AppContext) (tools.Tool, error) {
		return tools.NewMCPTool(), nil
	}))
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
func (p *channelPlugin) Type() plugin.Type  { return plugin.TypeChannel }
func (p *channelPlugin) Close() error       { return nil }
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

// toolPlugin is a generic plugin for tools.
type toolPlugin struct {
	name    string
	factory func(context.Context, plugin.AppContext) (tools.Tool, error)
}

func newToolPlugin(name string, factory func(context.Context, plugin.AppContext) (tools.Tool, error)) *toolPlugin {
	return &toolPlugin{name: name, factory: factory}
}

func (p *toolPlugin) Name() string      { return "tool." + p.name }
func (p *toolPlugin) Type() plugin.Type  { return plugin.TypeTool }
func (p *toolPlugin) Close() error       { return nil }
func (p *toolPlugin) Init(ctx context.Context, appCtx plugin.AppContext) error {
	tool, err := p.factory(ctx, appCtx)
	if err != nil {
		return err
	}
	reg := appCtx.GetToolRegistry().(tools.ToolRegistry)
	reg.Register(tool)
	return nil
}

// --- Provider Plugins ---

type openaiProviderPlugin struct{}

func (p *openaiProviderPlugin) Name() string      { return "provider.openai" }
func (p *openaiProviderPlugin) Type() plugin.Type  { return plugin.TypeProvider }
func (p *openaiProviderPlugin) Close() error       { return nil }
func (p *openaiProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	reg := appCtx.GetProviderRegistry().(*providers.Registry)

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
	reg.Register("openai", providers.NewOpenAIProvider(apiBase, apiKey, model))
	return nil
}

type anthropicProviderPlugin struct{}

func (p *anthropicProviderPlugin) Name() string      { return "provider.anthropic" }
func (p *anthropicProviderPlugin) Type() plugin.Type  { return plugin.TypeProvider }
func (p *anthropicProviderPlugin) Close() error       { return nil }
func (p *anthropicProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	reg := appCtx.GetProviderRegistry().(*providers.Registry)

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
	reg.Register("anthropic", providers.NewAnthropicProvider(apiKey, apiBase, model))
	return nil
}

type azureProviderPlugin struct{}

func (p *azureProviderPlugin) Name() string      { return "provider.azure" }
func (p *azureProviderPlugin) Type() plugin.Type  { return plugin.TypeProvider }
func (p *azureProviderPlugin) Close() error       { return nil }
func (p *azureProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	reg := appCtx.GetProviderRegistry().(*providers.Registry)

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
	reg.Register("azure", providers.NewAzureProvider(apiBase, apiKey, apiVersion, model))
	return nil
}

type minimaxProviderPlugin struct{}

func (p *minimaxProviderPlugin) Name() string      { return "provider.minimax" }
func (p *minimaxProviderPlugin) Type() plugin.Type  { return plugin.TypeProvider }
func (p *minimaxProviderPlugin) Close() error       { return nil }
func (p *minimaxProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	reg := appCtx.GetProviderRegistry().(*providers.Registry)

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
	reg.Register("minimax", providers.NewMinimaxProvider(apiKey, apiBase, model))
	return nil
}

type openrouterProviderPlugin struct{}

func (p *openrouterProviderPlugin) Name() string      { return "provider.openrouter" }
func (p *openrouterProviderPlugin) Type() plugin.Type  { return plugin.TypeProvider }
func (p *openrouterProviderPlugin) Close() error       { return nil }
func (p *openrouterProviderPlugin) Init(_ context.Context, appCtx plugin.AppContext) error {
	cfg := appCtx.GetConfig().(*config.Config)
	reg := appCtx.GetProviderRegistry().(*providers.Registry)

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
	reg.Register("openrouter", providers.NewOpenRouterProvider(apiKey, model))
	return nil
}
