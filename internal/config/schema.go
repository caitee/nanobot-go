package config

// Config is the root config structure
type Config struct {
    Agents    AgentDefaults    `mapstructure:"agents"`
    Channels  ChannelsConfig  `mapstructure:"channels"`
    Providers ProvidersConfig  `mapstructure:"providers"`
    Gateway   GatewayConfig   `mapstructure:"gateway"`
    Tools     ToolsConfig     `mapstructure:"tools"`
}

// AgentDefaults defines default agent settings
type AgentDefaults struct {
    Workspace          string  `mapstructure:"workspace"`
    Model              string  `mapstructure:"model"`
    Provider           string  `mapstructure:"provider"`
    MaxTokens          int     `mapstructure:"max_tokens"`
    ContextWindowTokens int    `mapstructure:"context_window_tokens"`
    Temperature        float64 `mapstructure:"temperature"`
    MaxToolIterations  int     `mapstructure:"max_tool_iterations"`
    ReasoningEffort    string  `mapstructure:"reasoning_effort"`
}

// ChannelsConfig defines channels settings
type ChannelsConfig struct {
    SendProgress  bool `mapstructure:"send_progress"`
    SendToolHints bool `mapstructure:"send_tool_hints"`
}

// ProvidersConfig defines providers settings
type ProvidersConfig struct {
    OpenAI     map[string]any `mapstructure:"openai"`
    Azure      map[string]any `mapstructure:"azure"`
    Anthropic  map[string]any `mapstructure:"anthropic"`
    OpenRouter map[string]any `mapstructure:"openrouter"`
}

// GatewayConfig defines gateway settings
type GatewayConfig struct {
    Host     string `mapstructure:"host"`
    Port     int    `mapstructure:"port"`
    Heartbeat int   `mapstructure:"heartbeat"`
}

// ToolsConfig defines tools settings
type ToolsConfig struct {
    Web        WebConfig        `mapstructure:"web"`
    Exec       ExecConfig       `mapstructure:"exec"`
    MCP        map[string]any   `mapstructure:"mcp"`
    Workspace  WorkspaceConfig  `mapstructure:"workspace"`
}

// WebConfig defines web tool settings
type WebConfig struct {
    SearchProvider string `mapstructure:"search_provider"`
    SearchAPIKey   string `mapstructure:"search_api_key"`
}

// ExecConfig defines execution restrictions
type ExecConfig struct {
    Enabled  bool     `mapstructure:"enabled"`
    Allow    []string `mapstructure:"allow"`
    Deny     []string `mapstructure:"deny"`
}

// WorkspaceConfig defines workspace restrictions
type WorkspaceConfig struct {
    AllowedDirs []string `mapstructure:"allowed_dirs"`
}
