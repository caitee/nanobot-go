# 阶段 1：遗留代码迁移 + 错误处理统一化 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完全移除遗留代码（internal/providers/ 和 internal/tools/），建立统一的错误类型体系，为后续插件和扩展系统优化打好基础。

**Architecture:** 将所有提供商实现迁移到 internal/llm/providers/，所有工具实现迁移到 internal/tool/builtin/，删除 bridge 和 adapter 桥接层，建立结构化错误类型体系（Category、Severity、Code），实现错误处理器和恢复机制。

**Tech Stack:** Go 1.24+, 标准库 errors 包

---

## 文件结构规划

### 新增文件
- `internal/errors/types.go` - 错误类型定义（Error 结构体、Category、Severity）
- `internal/errors/codes.go` - 预定义错误代码常量
- `internal/errors/format.go` - 错误格式化函数
- `internal/errors/types_test.go` - 错误类型测试
- `internal/errors/codes_test.go` - 错误代码测试
- `internal/errors/format_test.go` - 错误格式化测试
- `internal/runtime/error_handler.go` - 错误处理器
- `internal/runtime/error_handler_test.go` - 错误处理器测试

### 移动文件
- `internal/providers/openai.go` → `internal/llm/providers/openai.go`
- `internal/providers/anthropic.go` → `internal/llm/providers/anthropic.go`
- `internal/providers/minimax.go` → `internal/llm/providers/minimax.go`
- `internal/providers/fallback.go` → `internal/llm/providers/fallback.go`
- `internal/providers/openrouter.go` → `internal/llm/providers/openrouter.go`
- `internal/tools/shell.go` → `internal/tool/builtin/shell.go`
- `internal/tools/filesystem.go` → `internal/tool/builtin/filesystem.go`
- `internal/tools/web.go` → `internal/tool/builtin/web.go`
- `internal/tools/cron.go` → `internal/tool/builtin/cron.go`
- `internal/tools/message.go` → `internal/tool/builtin/message.go`
- `internal/tools/spawn.go` → `internal/tool/builtin/spawn.go`
- `internal/tools/mcp.go` → `internal/tool/builtin/mcp.go`

### 删除文件
- `internal/llm/bridge.go` - 提供商桥接适配器
- `internal/tool/adapter.go` - 工具适配器
- `internal/providers/registry.go` - 遗留提供商注册表
- `internal/tools/base.go` - 遗留工具基类

### 修改文件
- `internal/llm/registry.go` - 更新提供商注册逻辑
- `internal/tool/types.go` - 更新工具接口
- `internal/runtime/loop.go` - 集成错误处理器
- `internal/app/defaults.go` - 更新插件注册路径
- `internal/app/app.go` - 添加错误处理器字段

---

## Task 1: 创建错误类型体系

**Files:**
- Create: `internal/errors/types.go`
- Create: `internal/errors/types_test.go`

- [ ] **Step 1: 编写错误类型测试**

创建 `internal/errors/types_test.go`:

```go
package errors

import (
	"errors"
	"testing"
)

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "error without cause",
			err: &Error{
				Category: CategoryProvider,
				Code:     "api_key_missing",
				Message:  "API key is missing",
			},
			want: "[provider:api_key_missing] API key is missing",
		},
		{
			name: "error with cause",
			err: &Error{
				Category: CategoryProvider,
				Code:     "network_error",
				Message:  "Failed to connect",
				Cause:    errors.New("connection refused"),
			},
			want: "[provider:network_error] Failed to connect: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &Error{
		Category: CategoryRuntime,
		Code:     "loop_failed",
		Message:  "Loop execution failed",
		Cause:    cause,
	}

	if unwrapped := err.Unwrap(); unwrapped != cause {
		t.Errorf("Error.Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestNew(t *testing.T) {
	err := New(CategoryTool, "execution_failed", "Tool execution failed")

	if err.Category != CategoryTool {
		t.Errorf("Category = %v, want %v", err.Category, CategoryTool)
	}
	if err.Code != "execution_failed" {
		t.Errorf("Code = %v, want %v", err.Code, "execution_failed")
	}
	if err.Message != "Tool execution failed" {
		t.Errorf("Message = %v, want %v", err.Message, "Tool execution failed")
	}
	if err.Severity != SeverityError {
		t.Errorf("Severity = %v, want %v", err.Severity, SeverityError)
	}
	if err.Recoverable {
		t.Error("Recoverable should be false by default")
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("original error")
	err := Wrap(cause, CategoryConfig, "load_failed", "Failed to load config")

	if err.Cause != cause {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}
	if err.Category != CategoryConfig {
		t.Errorf("Category = %v, want %v", err.Category, CategoryConfig)
	}
}

func TestIsContextOverflow(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "context overflow error",
			err: &Error{
				Category: CategoryProvider,
				Code:     ErrProviderContextOverflow,
			},
			want: true,
		},
		{
			name: "other error",
			err: &Error{
				Category: CategoryProvider,
				Code:     "api_key_missing",
			},
			want: false,
		},
		{
			name: "standard error",
			err:  errors.New("some error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContextOverflow(tt.err); got != tt.want {
				t.Errorf("IsContextOverflow() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
go test ./internal/errors -v
```

预期输出：FAIL - 包不存在或类型未定义

- [ ] **Step 3: 实现错误类型**

创建 `internal/errors/types.go`:

```go
package errors

import (
	"errors"
	"fmt"
)

// Category 错误类别
type Category string

const (
	CategoryProvider   Category = "provider"   // LLM 提供商错误
	CategoryTool       Category = "tool"       // 工具执行错误
	CategoryRuntime    Category = "runtime"    // 运行时错误
	CategoryConfig     Category = "config"     // 配置错误
	CategoryPlugin     Category = "plugin"     // 插件错误
	CategoryNetwork    Category = "network"    // 网络错误
	CategoryValidation Category = "validation" // 验证错误
)

// Severity 错误严重程度
type Severity string

const (
	SeverityInfo     Severity = "info"     // 信息性错误
	SeverityWarning  Severity = "warning"  // 警告
	SeverityError    Severity = "error"    // 错误
	SeverityCritical Severity = "critical" // 严重错误
)

// Error 结构化错误
type Error struct {
	Category    Category               // 错误类别
	Severity    Severity               // 严重程度
	Code        string                 // 错误代码
	Message     string                 // 人类可读的错误消息
	Cause       error                  // 原始错误
	Context     map[string]interface{} // 上下文信息
	Recoverable bool                   // 是否可恢复
}

// Error 实现 error 接口
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Category, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Category, e.Code, e.Message)
}

// Unwrap 返回原始错误
func (e *Error) Unwrap() error {
	return e.Cause
}

// New 创建新错误
func New(category Category, code string, message string) *Error {
	return &Error{
		Category:    category,
		Severity:    SeverityError,
		Code:        code,
		Message:     message,
		Context:     make(map[string]interface{}),
		Recoverable: false,
	}
}

// Wrap 包装现有错误
func Wrap(err error, category Category, code string, message string) *Error {
	return &Error{
		Category:    category,
		Severity:    SeverityError,
		Code:        code,
		Message:     message,
		Cause:       err,
		Context:     make(map[string]interface{}),
		Recoverable: false,
	}
}

// IsContextOverflow 检查是否为上下文溢出错误
func IsContextOverflow(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == ErrProviderContextOverflow
	}
	return false
}

// IsAPIKeyMissing 检查是否为 API 密钥缺失错误
func IsAPIKeyMissing(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == ErrProviderAPIKeyMissing
	}
	return false
}

// IsToolExecutionTimeout 检查是否为工具执行超时错误
func IsToolExecutionTimeout(err error) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Code == ErrToolExecutionTimeout
	}
	return false
}

// 错误代码常量（在 codes.go 中定义）
const (
	ErrProviderAPIKeyMissing   = "provider.api_key_missing"
	ErrProviderContextOverflow = "provider.context_overflow"
	ErrToolExecutionTimeout    = "tool.execution_timeout"
)
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
go test ./internal/errors -v
```

预期输出：PASS

- [ ] **Step 5: 提交**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
git add internal/errors/types.go internal/errors/types_test.go
git commit -m "feat(errors): add structured error types

添加结构化错误类型体系，包括：
- Error 结构体（Category、Severity、Code、Message）
- 错误类别和严重程度常量
- New/Wrap 构造函数
- IsContextOverflow/IsAPIKeyMissing 检测函数

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: 创建错误代码常量

**Files:**
- Create: `internal/errors/codes.go`
- Create: `internal/errors/codes_test.go`

- [ ] **Step 1: 编写错误代码测试**

创建 `internal/errors/codes_test.go`:

```go
package errors

import (
	"testing"
)

func TestErrorCodes(t *testing.T) {
	// 验证错误代码格式正确
	tests := []struct {
		name string
		code string
		want string
	}{
		{"provider api key missing", ErrProviderAPIKeyMissing, "provider.api_key_missing"},
		{"provider context overflow", ErrProviderContextOverflow, "provider.context_overflow"},
		{"tool not found", ErrToolNotFound, "tool.not_found"},
		{"runtime loop failed", ErrRuntimeLoopFailed, "runtime.loop_failed"},
		{"config load failed", ErrConfigLoadFailed, "config.load_failed"},
		{"plugin load failed", ErrPluginLoadFailed, "plugin.load_failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.want {
				t.Errorf("code = %v, want %v", tt.code, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
go test ./internal/errors -run TestErrorCodes -v
```

预期输出：FAIL - 常量未定义

- [ ] **Step 3: 实现错误代码常量**

创建 `internal/errors/codes.go`:

```go
package errors

// Provider 错误代码
const (
	ErrProviderAPIKeyMissing   = "provider.api_key_missing"
	ErrProviderContextOverflow = "provider.context_overflow"
	ErrProviderRateLimited     = "provider.rate_limited"
	ErrProviderInvalidResponse = "provider.invalid_response"
	ErrProviderNetworkError    = "provider.network_error"
)

// Tool 错误代码
const (
	ErrToolNotFound          = "tool.not_found"
	ErrToolExecutionFailed   = "tool.execution_failed"
	ErrToolExecutionTimeout  = "tool.execution_timeout"
	ErrToolInvalidParameters = "tool.invalid_parameters"
	ErrToolPermissionDenied  = "tool.permission_denied"
)

// Runtime 错误代码
const (
	ErrRuntimeLoopFailed    = "runtime.loop_failed"
	ErrRuntimeHookFailed    = "runtime.hook_failed"
	ErrRuntimeInvalidState  = "runtime.invalid_state"
)

// Config 错误代码
const (
	ErrConfigLoadFailed      = "config.load_failed"
	ErrConfigInvalidFormat   = "config.invalid_format"
	ErrConfigMissingRequired = "config.missing_required"
)

// Plugin 错误代码
const (
	ErrPluginLoadFailed   = "plugin.load_failed"
	ErrPluginInitFailed   = "plugin.init_failed"
	ErrPluginIncompatible = "plugin.incompatible"
)
```

- [ ] **Step 4: 更新 types.go 移除临时常量**

编辑 `internal/errors/types.go`，删除文件末尾的临时常量定义：

```go
// 删除这些行：
// const (
// 	ErrProviderAPIKeyMissing   = "provider.api_key_missing"
// 	ErrProviderContextOverflow = "provider.context_overflow"
// 	ErrToolExecutionTimeout    = "tool.execution_timeout"
// )
```

- [ ] **Step 5: 运行所有测试验证通过**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
go test ./internal/errors -v
```

预期输出：PASS（所有测试）

- [ ] **Step 6: 提交**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
git add internal/errors/codes.go internal/errors/codes_test.go internal/errors/types.go
git commit -m "feat(errors): add error code constants

添加预定义错误代码常量：
- Provider 错误（api_key_missing, context_overflow, rate_limited 等）
- Tool 错误（not_found, execution_failed, execution_timeout 等）
- Runtime 错误（loop_failed, hook_failed, invalid_state）
- Config 错误（load_failed, invalid_format, missing_required）
- Plugin 错误（load_failed, init_failed, incompatible）

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: 创建错误格式化函数

**Files:**
- Create: `internal/errors/format.go`
- Create: `internal/errors/format_test.go`

- [ ] **Step 1: 编写错误格式化测试**

创建 `internal/errors/format_test.go`:

```go
package errors

import (
	"testing"
)

func TestFormatUserMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "api key missing",
			err: &Error{
				Category: CategoryProvider,
				Code:     ErrProviderAPIKeyMissing,
				Message:  "API key missing",
				Context: map[string]interface{}{
					"provider": "openai",
				},
			},
			want: "API key for openai is missing. Please set it in config.json or environment variable.",
		},
		{
			name: "context overflow",
			err: &Error{
				Category: CategoryProvider,
				Code:     ErrProviderContextOverflow,
				Message:  "Context overflow",
			},
			want: "Context window exceeded. Consider using a model with larger context or enabling compaction.",
		},
		{
			name: "tool execution timeout",
			err: &Error{
				Category: CategoryTool,
				Code:     ErrToolExecutionTimeout,
				Message:  "Execution timeout",
				Context: map[string]interface{}{
					"tool":    "shell",
					"timeout": "30s",
				},
			},
			want: "Tool 'shell' execution timed out after 30s.",
		},
		{
			name: "generic error",
			err: &Error{
				Category: CategoryRuntime,
				Code:     "unknown_error",
				Message:  "Something went wrong",
			},
			want: "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatUserMessage(tt.err); got != tt.want {
				t.Errorf("FormatUserMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
go test ./internal/errors -run TestFormatUserMessage -v
```

预期输出：FAIL - FormatUserMessage 未定义

- [ ] **Step 3: 实现错误格式化函数**

创建 `internal/errors/format.go`:

```go
package errors

import (
	"errors"
	"fmt"
)

// FormatUserMessage 格式化用户友好的错误消息
func FormatUserMessage(err error) string {
	var e *Error
	if !errors.As(err, &e) {
		return err.Error()
	}

	switch e.Code {
	case ErrProviderAPIKeyMissing:
		provider := "the provider"
		if p, ok := e.Context["provider"].(string); ok {
			provider = p
		}
		return fmt.Sprintf("API key for %s is missing. Please set it in config.json or environment variable.", provider)

	case ErrProviderContextOverflow:
		return "Context window exceeded. Consider using a model with larger context or enabling compaction."

	case ErrProviderRateLimited:
		return "Rate limit exceeded. Please wait a moment and try again."

	case ErrToolExecutionTimeout:
		tool := "the tool"
		if t, ok := e.Context["tool"].(string); ok {
			tool = fmt.Sprintf("'%s'", t)
		}
		timeout := "the specified time"
		if to, ok := e.Context["timeout"]; ok {
			timeout = fmt.Sprintf("%v", to)
		}
		return fmt.Sprintf("Tool %s execution timed out after %s.", tool, timeout)

	case ErrToolNotFound:
		tool := "unknown"
		if t, ok := e.Context["tool"].(string); ok {
			tool = t
		}
		return fmt.Sprintf("Tool '%s' not found. Please check the tool name.", tool)

	case ErrConfigLoadFailed:
		return "Failed to load configuration. Please check your config file."

	case ErrPluginLoadFailed:
		plugin := "unknown"
		if p, ok := e.Context["plugin"].(string); ok {
			plugin = p
		}
		return fmt.Sprintf("Failed to load plugin '%s'. Please check the plugin installation.", plugin)

	default:
		return e.Message
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
go test ./internal/errors -v
```

预期输出：PASS（所有测试）

- [ ] **Step 5: 提交**

```bash
cd /Users/caite/Documents/code/ai-agent/nanobot-go
git add internal/errors/format.go internal/errors/format_test.go
git commit -m "feat(errors): add user-friendly error formatting

添加 FormatUserMessage 函数，为常见错误提供用户友好的消息：
- API 密钥缺失提示
- 上下文溢出建议
- 工具执行超时信息
- 插件加载失败提示

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
```

---

## 执行选项

计划已完成并保存到 `docs/superpowers/plans/2026-05-06-phase1-legacy-migration-error-handling.md`。

**两种执行方式：**

**1. Subagent-Driven（推荐）** - 为每个任务派发新的子代理，任务间审查，快速迭代

**2. Inline Execution** - 在当前会话中使用 executing-plans 执行任务，批量执行带检查点

您希望使用哪种方式？