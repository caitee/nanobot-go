# 思考内容显示优化

## 修改说明

在 agent 交互模式下，当模型的思考内容（reasoning）超过 3 行时，现在会自动折叠，只显示最后 3 行内容，并在顶部显示一个滚动指示器。

## 实现细节

### 修改的文件
- `cmd/nanobot/agent_tui.go` - `renderRound()` 方法

### 主要改动

1. **行数限制**: 设置常量 `maxReasoningLines = 3`，限制思考内容的可见行数
2. **空行过滤**: 自动过滤掉渲染后的尾部空行，确保计数准确
3. **滚动指示器**: 当内容超过 3 行时，显示 `⋮ (N more lines)` 提示
4. **保留最后 N 行**: 只显示思考内容的最后 3 行

### 代码示例

```go
if len(lines) > maxReasoningLines {
    // 显示滚动指示器和最后 N 行
    hidden := len(lines) - maxReasoningLines
    s.WriteString(reasoningStyle.Render(fmt.Sprintf("  ⋮ (%d more lines)", hidden)))
    s.WriteString("\n")
    s.WriteString(strings.Join(lines[len(lines)-maxReasoningLines:], "\n"))
} else {
    s.WriteString(strings.Join(lines, "\n"))
}
```

## 效果展示

### 之前
```
思考内容第 1 行
思考内容第 2 行
思考内容第 3 行
思考内容第 4 行
思考内容第 5 行
思考内容第 6 行
思考内容第 7 行
```

### 之后
```
  ⋮ (4 more lines)
思考内容第 5 行
思考内容第 6 行
思考内容第 7 行
```

## 配置

如需修改显示的行数，可以调整 `agent_tui.go` 中的 `maxReasoningLines` 常量：

```go
const maxReasoningLines = 3  // 修改为你想要的行数
```

## 注意事项

- 此功能仅影响实时交互模式（TUI）的显示
- 最终输出到终端的完整消息不受影响
- 滚动指示器使用灰色斜体样式（`reasoningStyle`）以保持视觉一致性
