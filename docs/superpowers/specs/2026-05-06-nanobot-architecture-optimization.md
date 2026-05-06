---
title: Nanobot-Go 架构优化设计
date: 2026-05-06
author: Claude Opus 4.7
status: draft
---

# Nanobot-Go 架构优化设计

## 上下文

### 为什么需要这次优化？

nanobot-go 是一个 Go 实现的 AI 代理框架，当前架构受 pi-mono（TypeScript monorepo）启发，已经实现了基本的模块化设计。但在实际使用中发现以下问题：

1. **遗留代码并存**：存在两套并行的抽象层
   - 遗留层：`internal/providers/` 和 `internal/tools/`
   - 新抽象层：`internal/llm/` 和 `internal/tool/`
   - 桥接层：`bridge.go` 和 `adapter.go`
   - 导致代码重复、维护困难、测试覆盖复杂

2. **错误处理不统一**：使用标准 Go error，缺少结构化错误类型
   - 难以区分错误类别和严重程度
   - 缺少错误恢复机制
   - 用户友好的错误消息不足

3. **插件系统静态**：所有插件编译时链接
   - 无法动态加载第三方插件
   - 无法热重载
   - 缺少依赖管理和版本控制

4. **扩展系统有限**：钩子类型固定，功能受限
   - 无法动态注册新钩子点
   - 缺少事件拦截和修改机制
   - 无法持久化扩展状态

### 优化目标

通过参考 pi-mono 的成熟设计，对 nanobot-go 进行全面架构优化：

1. 完全移除遗留代码，统一到新抽象层
2. 建立清晰的错误类型体系和恢复机制
3. 实现动态插件加载和热重载
4. 增强扩展系统的灵活性和能力

### 实施策略

采用**渐进式重构**方案，分 4 个阶段完成：
- 阶段 1：遗留代码迁移 + 错误处理统一化（2-3 周）
- 阶段 2：插件系统完善（2 周）
- 阶段 3：扩展系统增强（1-2 周）
- 阶段 4：测试和文档（1 周）

**总时间**：6-8 周

**兼容性**：不考虑向后兼容（个人实验性项目）

---

## 阶段 1：遗留代码迁移 + 错误处理统一化

### 目标

1. 完全移除 `internal/providers/` 和 `internal/tools/` 遗留实现
2. 删除 `bridge.go` 和 `adapter.go` 桥接层
3. 建立统一的错误类型体系
4. 实现错误恢复机制

### 架构设计

#### 1.1 目录结构重组

**当前状态**：
```
internal/
├── providers/          # 遗留提供商实现
├── llm/                # 新抽象层
│   └── bridge.go       # 临时桥接
├── tools/              # 遗留工具实现
└── tool/               # 新抽象层
    └── adapter.go      # 临时适配器
```

**目标状态**：
```
internal/
├── llm/                # 统一的 LLM 抽象
│   ├── types.go
│   ├── registry.go
│   └── providers/      # 所有提供商实现
│       ├── openai.go
│       ├── anthropic.go
│       ├── minimax.go
│       └── ...
├── tool/               # 统一的工具抽象
│   ├── types.go
│   ├── registry.go
│   └── builtin/        # 内置工具实现
│       ├── shell.go
│       ├── filesystem.go
│       └── ...
└── errors/             # 新增：错误类型体系
    ├── types.go
    ├── codes.go
    └── format.go
```

#### 1.2 错误类型体系

参考 pi-mono 的分层错误处理机制，设计结构化错误类型：

**核心类型**（`internal/errors/types.go`）：

```go
type Error struct {
    Category    Category               // 错误类别（provider/tool/runtime/config/plugin）
    Severity    Severity               // 严重程度（info/warning/error/critical）
    Code        string                 // 错误代码（如 "provider.api_key_missing"）
    Message     string                 // 人类可读的错误消息
    Cause       error                  // 原始错误
    Context     map[string]interface{} // 上下文信息
    Recoverable bool                   // 是否可恢复
}
```

**预定义错误代码**（`internal/errors/codes.go`）：
- Provider: `api_key_missing`, `context_overflow`, `rate_limited`, `invalid_response`
- Tool: `not_found`, `execution_failed`, `execution_timeout`, `permission_denied`
- Runtime: `loop_failed`, `hook_failed`, `invalid_state`
- Config: `load_failed`, `invalid_format`, `missing_required`
- Plugin: `load_failed`, `init_failed`, `incompatible`

**错误处理器**（`internal/runtime/error_handler.go`）：
- 根据错误类型决定处理策略
- 可恢复错误：记录日志后继续
- 上下文溢出：触发压缩
- 速率限制：等待后重试
- 严重错误：停止循环

#### 1.3 迁移步骤

1. **创建错误类型体系**
   - 新增 `internal/errors/` 目录
   - 实现 `types.go`, `codes.go`, `format.go`
   - 添加单元测试

2. **迁移提供商实现**
   - 移动 `internal/providers/*.go` → `internal/llm/providers/*.go`
   - 更新所有提供商返回结构化错误
   - 删除 `internal/llm/bridge.go`
   - 更新 `internal/llm/registry.go`

3. **迁移工具实现**
   - 移动 `internal/tools/*.go` → `internal/tool/builtin/*.go`
   - 更新所有工具返回结构化错误
   - 删除 `internal/tool/adapter.go`
   - 更新 `internal/tool/registry.go`

4. **集成错误处理器**
   - 在 `internal/runtime/loop.go` 中集成错误处理器
   - 更新 `internal/app/dispatcher.go` 格式化错误消息

5. **更新测试**
   - 更新现有测试以使用新路径
   - 添加错误处理测试

### 关键文件

**新增**：
- `internal/errors/types.go` - 错误类型定义
- `internal/errors/codes.go` - 错误代码常量
- `internal/errors/format.go` - 错误格式化
- `internal/runtime/error_handler.go` - 错误处理器

**移动**：
- `internal/providers/*.go` → `internal/llm/providers/*.go`
- `internal/tools/*.go` → `internal/tool/builtin/*.go`

**删除**：
- `internal/llm/bridge.go`
- `internal/tool/adapter.go`
- `internal/providers/` 目录
- `internal/tools/` 目录

**修改**：
- `internal/llm/registry.go`
- `internal/tool/registry.go`
- `internal/runtime/loop.go`
- `internal/app/defaults.go`
- 所有提供商和工具实现文件

---

## 阶段 2：插件系统完善

### 目标

1. 实现动态插件加载（基于 hashicorp/go-plugin）
2. 添加插件元数据和依赖声明
3. 实现插件热重载机制
4. 为插件市场打好基础

### 架构设计

#### 2.1 插件加载机制

**选择 hashicorp/go-plugin 的理由**：
- 跨平台支持（Windows/macOS/Linux）
- 进程隔离（插件崩溃不影响主进程）
- 语言无关（可用任何语言编写插件）
- 成熟稳定（Terraform、Vault 等项目使用）
- 版本兼容（通过 gRPC 协议版本管理）

#### 2.2 插件接口设计

**插件类型**：
- `PluginTypeProvider` - LLM 提供商
- `PluginTypeChannel` - 渠道适配器
- `PluginTypeTool` - 工具
- `PluginTypeHook` - 钩子扩展

**插件元数据**（`internal/plugin/types.go`）：
```go
type Metadata struct {
    Name           string       // 插件名称
    Version        string       // 语义化版本
    Type           PluginType   // 插件类型
    Description    string       // 描述
    Author         string       // 作者
    Dependencies   []Dependency // 依赖的其他插件
    MinCoreVersion string       // 最小核心版本
    Capabilities   []string     // 插件能力列表
    ConfigSchema   map[string]interface{} // JSON Schema
}
```

**插件生命周期接口**：
```go
type Plugin interface {
    Metadata() (*Metadata, error)
    Init(ctx context.Context, config map[string]interface{}) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Health(ctx context.Context) error
}
```

#### 2.3 插件管理器

**职责**（`internal/plugin/manager.go`）：
1. 插件发现和加载
2. 依赖解析和验证
3. 生命周期管理
4. 热重载支持

**核心方法**：
- `Discover()` - 发现插件目录中的可执行文件
- `Load(pluginPath, config)` - 加载插件
- `Unload(name)` - 卸载插件
- `Reload(name)` - 重载插件
- `Get(name)` - 获取已加载的插件
- `List()` - 列出所有插件
- `Shutdown()` - 关闭所有插件

**版本验证**：
- 使用 `github.com/Masterminds/semver/v3` 进行版本约束检查
- 验证插件的 `MinCoreVersion` 与当前核心版本兼容
- 验证插件依赖的版本约束

#### 2.4 插件目录结构

```
~/.nanobot/plugins/
├── openai-provider/
│   ├── plugin.json          # 元数据
│   └── openai-provider      # 可执行文件
├── telegram-channel/
│   ├── plugin.json
│   └── telegram-channel
└── custom-tool/
    ├── plugin.json
    └── custom-tool
```

**plugin.json 示例**：
```json
{
  "name": "openai-provider",
  "version": "1.2.0",
  "type": "provider",
  "description": "OpenAI GPT models provider",
  "author": "Nanobot Team",
  "dependencies": [],
  "min_core_version": "0.5.0",
  "capabilities": ["streaming", "function_calling", "vision"],
  "config_schema": {
    "type": "object",
    "properties": {
      "api_key": {"type": "string", "description": "OpenAI API key"}
    },
    "required": ["api_key"]
  }
}
```

#### 2.5 热重载机制

**实现方式**（`internal/plugin/watcher.go`）：
- 使用 `fsnotify` 监控插件目录变化
- 检测到文件修改时触发重载
- 优雅重载：停止旧实例 → 加载新实例 → 切换
- 回滚机制：新插件加载失败时回滚到旧版本

### 关键文件

**新增**：
- `internal/plugin/types.go` - 插件类型定义
- `internal/plugin/manager.go` - 插件管理器
- `internal/plugin/watcher.go` - 文件监控器
- `internal/plugin/grpc.go` - gRPC 插件实现

**修改**：
- `internal/app/app.go` - 添加 PluginManager 字段
- `internal/app/defaults.go` - 从插件管理器获取插件
- `internal/config/schema.go` - 添加 plugins 配置节

**依赖**：
- `github.com/hashicorp/go-plugin` - 插件框架
- `github.com/Masterminds/semver/v3` - 版本管理
- `github.com/fsnotify/fsnotify` - 文件监控

---

## 阶段 3：扩展系统增强

### 目标

1. 增强钩子系统的灵活性
2. 实现事件拦截和修改机制
3. 添加扩展状态持久化
4. 支持扩展间通信

### 架构设计

#### 3.1 增强的钩子系统

**新的钩子注册机制**（`internal/runtime/hooks/registry.go`）：

**特性**：
- 支持钩子优先级（Lowest/Low/Normal/High/Highest）
- 按优先级排序执行
- 支持动态注册/注销钩子
- 支持启用/禁用钩子

**新增钩子点**：
- `agent_start` / `agent_end` - 代理生命周期
- `turn_start` / `turn_end` - 回合生命周期
- `message_start` / `message_update` / `message_end` - 消息生命周期
- `tool_start` / `tool_end` - 工具执行生命周期
- `error` - 错误发生时

#### 3.2 事件拦截和修改机制

**事件系统**（`internal/runtime/events/system.go`）：

**核心概念**：
```go
type Event struct {
    Type      EventType              // 事件类型
    Timestamp time.Time              // 时间戳
    Data      interface{}            // 事件数据
    Metadata  map[string]interface{} // 元数据
    Cancelled bool                   // 是否取消后续处理
    Modified  bool                   // 数据是否被修改
}
```

**能力**：
- 订阅/发布事件
- 拦截事件（设置 `Cancelled = true`）
- 修改事件数据（设置 `Modified = true`）
- 事件链式传播

**使用示例**：
```go
// 拦截消息更新事件
eventSystem.Subscribe(EventMessageUpdate, func(ctx context.Context, event *Event) error {
    data := event.Data.(*MessageUpdateData)
    // 修改消息内容
    if strings.Contains(data.Content, "sensitive") {
        data.Content = strings.ReplaceAll(data.Content, "sensitive", "[REDACTED]")
        event.Modified = true
    }
    return nil
})
```

#### 3.3 扩展状态持久化

**状态存储接口**（`internal/extensions/state.go`）：
```go
type StateStore interface {
    Get(ctx context.Context, key string) (interface{}, error)
    Set(ctx context.Context, key string, value interface{}) error
    Delete(ctx context.Context, key string) error
    Keys(ctx context.Context) ([]string, error)
    Clear(ctx context.Context) error
}
```

**实现**：
- `FileStateStore` - 基于文件系统的状态存储
- 使用 JSON 格式存储
- 内存缓存提升性能
- 支持并发访问

**存储位置**：`~/.nanobot/extensions/<extension-name>/state/`

#### 3.4 扩展间通信机制

**增强的消息总线**（`internal/bus/enhanced.go`）：

**特性**：
- 发布/订阅模式
- 请求/响应模式
- 主题订阅
- 超时控制

**使用示例**：
```go
// 扩展 A 订阅主题
bus.Subscribe("data_request", func(ctx context.Context, message interface{}) error {
    // 处理请求
    return nil
})

// 扩展 B 发布消息
bus.Publish(ctx, "data_request", &DataRequest{...})

// 扩展 C 请求-响应
response, err := bus.Request(ctx, "get_config", &ConfigRequest{...})
```

### 关键文件

**新增**：
- `internal/runtime/hooks/registry.go` - 增强的钩子注册表
- `internal/runtime/events/system.go` - 事件系统
- `internal/extensions/state.go` - 状态存储接口
- `internal/bus/enhanced.go` - 增强的消息总线

**修改**：
- `internal/runtime/loop.go` - 集成事件系统
- `internal/app/app.go` - 添加事件系统和状态存储

---

## 阶段 4：测试和文档

### 测试策略

**测试覆盖目标**：
- 新增代码：80%+ 覆盖率
- 核心模块：90%+ 覆盖率
- 集成测试：覆盖主要用户场景

**测试文件组织**：
```
internal/
├── errors/
│   ├── types_test.go
│   ├── codes_test.go
│   └── format_test.go
├── plugin/
│   ├── manager_test.go
│   ├── watcher_test.go
│   └── integration_test.go
├── runtime/
│   ├── hooks/registry_test.go
│   ├── events/system_test.go
│   └── error_handler_test.go
└── extensions/
    └── state_test.go
```

**测试类型**：
1. **单元测试**：测试单个函数和方法
2. **集成测试**：测试模块间协作
3. **性能测试**：对比优化前后的性能
4. **兼容性测试**：验证现有功能正常工作

### 文档计划

**新增文档**：

1. **架构文档更新** (`docs/ARCHITECTURE.md`)
   - 更新架构图
   - 添加错误处理流程
   - 添加插件系统说明
   - 添加扩展系统说明

2. **插件开发指南** (`docs/PLUGIN_DEVELOPMENT.md`)
   - 插件类型介绍
   - 开发环境设置
   - 插件接口说明
   - 示例插件代码
   - 测试和调试
   - 发布和分发

3. **扩展开发指南** (`docs/EXTENSION_DEVELOPMENT.md`)
   - 钩子系统介绍
   - 事件系统介绍
   - 状态持久化
   - 扩展间通信
   - 最佳实践

4. **迁移指南** (`docs/MIGRATION.md`)
   - 从旧架构迁移到新架构
   - API 变更列表
   - 代码示例对比
   - 常见问题

5. **API 参考** (`docs/API_REFERENCE.md`)
   - 核心接口文档
   - 错误类型文档
   - 插件接口文档
   - 钩子接口文档

### 验证计划

**验证步骤**：
1. 单元测试：运行所有单元测试，确保覆盖率达标
2. 集成测试：运行集成测试，验证各模块协同工作
3. 性能测试：对比优化前后的性能指标
4. 兼容性测试：验证现有功能是否正常工作
5. 文档审查：确保文档完整、准确、易懂

**性能基准**：
- 插件加载时间
- 插件重载时间
- 钩子执行开销
- 事件传播延迟

---

## 验证方式

### 端到端测试场景

1. **场景 1：动态加载插件**
   - 启动 nanobot
   - 将新插件放入插件目录
   - 验证插件自动加载
   - 验证插件功能正常工作

2. **场景 2：插件热重载**
   - 加载插件
   - 修改插件代码并重新编译
   - 验证插件自动重载
   - 验证新功能生效

3. **场景 3：错误恢复**
   - 触发可恢复错误（如速率限制）
   - 验证系统自动重试
   - 触发严重错误（如 API 密钥缺失）
   - 验证系统优雅停止并显示友好错误消息

4. **场景 4：扩展拦截事件**
   - 加载扩展
   - 扩展拦截消息更新事件
   - 验证消息内容被修改
   - 验证修改后的消息正确显示

5. **场景 5：扩展状态持久化**
   - 扩展保存状态
   - 重启 nanobot
   - 验证扩展状态恢复

### 性能指标

**优化前**（基准）：
- 启动时间：~500ms
- 插件加载：N/A（编译时链接）
- 钩子执行：~10μs/钩子

**优化后**（目标）：
- 启动时间：~600ms（+20%，可接受）
- 插件加载：~100ms/插件
- 插件重载：~150ms/插件
- 钩子执行：~15μs/钩子（+50%，可接受）
- 事件传播：~5μs/监听器

---

## 风险和缓解措施

### 风险 1：插件系统性能开销

**风险**：gRPC 插件通信可能引入显著延迟

**缓解措施**：
- 使用本地 Unix socket 通信（而非 TCP）
- 批量调用减少往返次数
- 缓存插件元数据
- 性能测试验证开销可接受

### 风险 2：插件兼容性问题

**风险**：插件版本不兼容导致加载失败

**缓解措施**：
- 严格的版本检查
- 清晰的错误消息
- 插件开发文档详细说明兼容性要求
- 提供插件模板和示例

### 风险 3：热重载稳定性

**风险**：热重载可能导致状态不一致

**缓解措施**：
- 优雅停止旧插件实例
- 等待正在进行的请求完成
- 回滚机制
- 充分的集成测试

### 风险 4：扩展系统复杂度

**风险**：钩子和事件系统过于复杂，难以理解和使用

**缓解措施**：
- 详细的开发文档
- 丰富的示例代码
- 清晰的最佳实践指南
- 简化的 API 设计

---

## 实施时间表

| 阶段 | 任务 | 时间 | 里程碑 |
|------|------|------|--------|
| 阶段 1 | 遗留代码迁移 + 错误处理 | 2-3 周 | 代码统一，错误处理完善 |
| 阶段 2 | 插件系统完善 | 2 周 | 动态插件加载，热重载 |
| 阶段 3 | 扩展系统增强 | 1-2 周 | 钩子增强，事件系统 |
| 阶段 4 | 测试和文档 | 1 周 | 测试覆盖，文档完整 |
| **总计** | | **6-8 周** | **架构优化完成** |

---

## 成功标准

1. **代码质量**
   - 遗留代码完全移除
   - 测试覆盖率达到 80%+
   - 无编译警告和错误

2. **功能完整性**
   - 动态插件加载正常工作
   - 热重载稳定可靠
   - 错误处理机制完善
   - 扩展系统功能齐全

3. **性能指标**
   - 启动时间增加不超过 20%
   - 插件加载时间 < 200ms
   - 钩子执行开销 < 20μs

4. **文档完整性**
   - 所有新功能有文档
   - 迁移指南清晰
   - API 参考完整
   - 示例代码丰富

5. **开发者体验**
   - 插件开发简单直观
   - 扩展开发文档清晰
   - 错误消息友好
   - 调试工具完善
