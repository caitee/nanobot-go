# MiniMax Provider 工具调用参数解析修复

## 问题描述

MiniMax provider 在流式响应中解析工具调用参数时存在问题，导致模型返回的工具调用参数无法正确执行。

### 根本原因

在 `finalizeMinimaxStreamToolCalls` 函数中，存在以下逻辑缺陷：

1. 当 `content_block_start` 事件中包含空的 `input` 对象时，会将 `Arguments` 初始化为空的 `map[string]any{}`
2. 后续的 `content_block_delta` 事件会累积 `InputJSON` 字符串（包含实际的参数）
3. 但在最终化工具调用时，只有当 `Arguments == nil` 时才会解析 `InputJSON`
4. 由于 `Arguments` 已经被初始化为空 map（不是 nil），导致 `InputJSON` 永远不会被解析

### 修复方案

修改 `finalizeMinimaxStreamToolCalls` 函数的逻辑：

- **优先使用 `InputJSON`**：如果 `InputJSON` 不为空，优先解析它作为参数
- **降级到 `Arguments`**：只有当 `InputJSON` 为空或解析失败时，才使用 `Arguments`
- **确保非 nil**：如果两者都为空，返回空的 map 而不是 nil

## 修改的文件

- `internal/providers/minimax.go` - 修复 `finalizeMinimaxStreamToolCalls` 函数
- `internal/providers/minimax_test.go` - 添加全面的测试用例

## 测试覆盖

添加了以下测试用例：

1. `TestFinalizeMinimaxStreamToolCallsPrefersStreamedInputJSON` - 验证优先使用 InputJSON
2. `TestFinalizeMinimaxStreamToolCallsNilArguments` - 验证 Arguments 为 nil 时的行为
3. `TestFinalizeMinimaxStreamToolCallsNoInputJSON` - 验证没有 InputJSON 时使用 Arguments
4. `TestFinalizeMinimaxStreamToolCallsInvalidJSON` - 验证 JSON 解析失败时的降级行为
5. `TestFinalizeMinimaxStreamToolCallsMultiple` - 验证多个工具调用的正确解析

## 影响范围

- 仅影响 MiniMax provider 的流式响应工具调用解析
- 非流式响应不受影响（直接从响应中提取 input 字段）
- 其他 provider（Anthropic、OpenAI、Azure 等）不受影响

## 验证

```bash
# 运行测试
go test ./internal/providers/... -v

# 编译验证
go build ./...
```

所有测试通过，编译成功。
