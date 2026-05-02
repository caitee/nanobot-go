# nanobot-go 模块化/插件化重构总结

## 重构目标

参考 Pi (https://github.com/badlogic/pi-mono) 的架构设计，对 nanobot-go 进行模块化和插件化重构，**不添加新功能，仅重构现有代码**。

## 完成的工作

### 1. 创建插件系统核心 (`internal/plugin/`)

- **`plugin.go`**: 定义 `Plugin` 接口，所有插件（Provider、Channel、Tool）都实现此接口
  - `Name()`: 插件唯一标识
  - `Type()`: 插件类型（provider/channel/tool）
  - `Init(ctx, app)`: 初始化插件
  - `Close()`: 关闭插件
  - `AppContext` 接口：插件可访问的应用上下文

- **`registry.go`**: 插件注册表
  - `Register(plugin)`: 注册插件
  - `Get(name)`: 获取插件
  - `GetByType(type)`: 按类型获取插件
  - `InitAll(ctx, app)`: 按注册顺序初始化所有插件
  - `CloseAll()`: 按逆序关闭所有插件

### 2. 创建应用生命周期管理 (`internal/app/`)

- **`app.go`**: `App` 结构体，统一管理所有组件
  - 持有 Config、Bus、SessionStore、ToolRegistry、ProviderRegistry、ChannelManager、CronService、PluginRegistry、AgentLoop、SubagentManager
  - 实现 `plugin.AppContext` 接口，供插件初始化时访问
  - `New(cfg)`: 创建 App 实例
  - `Start(ctx)`: 启动应用（初始化插件、启动 agent loop、channels、cron）
  - `Stop()`: 优雅关闭
  - `Context()` / `Done()`: 上下文管理

- **`defaults.go`**: 注册所有内置插件
  - 5 个 Provider 插件：openai, anthropic, azure, minimax, openrouter
  - 11 个 Channel 插件：telegram, discord, slack, feishu, dingtalk, wecom, qq, whatsapp, email, matrix, mochat
  - 7 个 Tool 插件：message, filesystem, shell, web, cron, spawn, mcp
  - 每个插件实现 `Plugin` 接口，在 `Init()` 中从配置读取参数并注册到相应的 Registry

### 3. 重构 Gateway (`cmd/gateway/main.go`)

**之前**：硬编码 `providers.NewOpenAIProvider`，手动初始化所有组件

**之后**：
```go
app, _ := app.New(cfg)
app.Start(ctx)
<-app.Done()
```

所有组件通过插件系统自动初始化，无需手动创建。

### 4. 重构 CLI (`cmd/nanobot/`)

- **`cmd_gateway.go`**: 使用 `app.New()` 和 `app.Start()` 替代手动初始化
- **`cmd_agent.go`**: 
  - 创建 App 实例并初始化插件
  - 从 `app.ProviderRegistry` 获取 provider，而非大 switch 语句
  - 复用 `app.ToolRegistry`

### 5. 更新配置 Schema (`internal/config/schema.go`)

添加 `PluginsConfig` 字段（为未来扩展预留）：
```go
type PluginsConfig struct {
    Providers []string `mapstructure:"providers"`
    Channels  []string `mapstructure:"channels"`
    Tools     []string `mapstructure:"tools"`
}
```

## 架构对比

### 重构前

```
cmd/gateway/main.go
├─ 硬编码 NewOpenAIProvider
├─ 手动创建 bus, session, tools, channels, cron
├─ 手动注册每个 tool
└─ 手动启动 agent loop

cmd/nanobot/cmd_agent.go
├─ 大 switch 语句选择 provider
├─ 重复的 tool 注册代码
└─ 与 gateway 逻辑重复
```

### 重构后

```
internal/plugin/
├─ plugin.go (Plugin 接口)
└─ registry.go (插件注册表)

internal/app/
├─ app.go (App 生命周期)
└─ defaults.go (内置插件注册)

cmd/gateway/main.go
└─ app.New() → app.Start() → <-app.Done()

cmd/nanobot/cmd_agent.go
└─ app.New() → 从 registry 获取 provider/tools
```

## 设计原则

1. **单一职责**：每个插件只负责一个 provider/channel/tool 的初始化
2. **依赖注入**：插件通过 `AppContext` 访问共享组件，避免循环依赖
3. **配置驱动**：插件从配置读取参数，支持环境变量 fallback
4. **优雅关闭**：插件按逆序关闭，确保资源正确释放
5. **向后兼容**：保持所有现有功能不变，API 兼容

## 验证

- ✅ `go build ./...` 编译通过
- ✅ `go vet ./...` 静态分析通过
- ✅ `go test ./...` 所有测试通过（2/13 包有测试）
- ✅ 保持现有功能不变

## 扩展性

### 添加新 Provider

1. 在 `internal/app/defaults.go` 添加插件：
```go
type newProviderPlugin struct{}
func (p *newProviderPlugin) Init(ctx, app) error {
    // 从 config 读取配置
    // 创建 provider
    // 注册到 app.ProviderRegistry
}
```

2. 在 `RegisterDefaults()` 中注册：
```go
reg.Register(&newProviderPlugin{})
```

### 添加新 Channel/Tool

同理，实现 `Plugin` 接口并注册即可。

## 未来改进方向

1. **动态插件加载**：支持从外部 `.so` 文件加载插件
2. **插件配置**：`config.Plugins` 字段控制启用哪些插件
3. **插件依赖**：支持插件间依赖声明
4. **热重载**：支持运行时加载/卸载插件
5. **插件市场**：类似 Pi 的 npm 包分发机制

## 参考

- Pi Mono: https://github.com/badlogic/pi-mono
- Pi 架构设计: https://pi.dev
- OpenClaw 插件系统: https://docs.openclaw.ai
