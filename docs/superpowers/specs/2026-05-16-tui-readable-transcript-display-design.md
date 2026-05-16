# TUI Readable Transcript Display Design

## 背景

`ori agent` 当前已经采用 transcript-first TUI 架构：runtime event、command result
和 session replay 先进入 transcript block/segment，再由 renderer 投影到 viewport。
这保证了可见历史只有一份数据源，也避免重新引入 `printAbove` 或终端 scrollback
作为第二套输出平面。

当前问题不在模型最终回答本身，而在默认 TUI 展示密度：reasoning 和 tool segment
作为辅助过程信息，占用了接近正文的视觉权重。尤其是 reasoning 的尾部原文摘要会紧贴
assistant final text，用户容易把它误认为最终回答的一部分。

## 目标

- 保持 transcript-first 架构，不改变 runtime、llm、tool、session 数据模型。
- 默认视图面向日常阅读，让 assistant final text 成为主视觉内容。
- 保留 reasoning/tool 的可观测性，但把完整过程细节放到显式详细视图中。
- 不把模型输出风格问题放进 renderer 修复；renderer 只负责不同语义层的投影方式。

## 非目标

- 不修改模型 prompt、provider 行为或最终回答内容。
- 不复用 `/reasoning` 控制 TUI 展示密度；它仍只控制模型是否产生 reasoning。
- 不新增 overlay 面板，不改变 viewport 滚动模型。
- 不把 tool result 解析成业务摘要，避免 renderer 理解工具语义。

## 展示模式

新增 TUI 展示密度概念，命名为 `normal` 和 `detail`：

- `normal`：默认阅读模式。thinking 只显示每段标题，tool 显示紧凑摘要和一行输出预览。
- `detail`：详细视图。展开 reasoning 尾部摘要、tool 参数和 preview/result/error。

推荐命令为：

```text
/view normal
/view detail
```

后续如果需要快捷切换，可以支持 `/view` 在两种模式间 toggle；第一版优先实现显式参数，
避免命令语义含糊。

## Reasoning 渲染

完整 reasoning 文本仍保存在 transcript 的 reasoning segment 中。renderer 根据展示模式
决定投影方式。

`normal` 模式：

```text
thinking · 38 lines summarized
```

- 保留 reasoning segment 的真实边界和顺序，每段只显示一个标题。
- 标题里的行数是该 segment 内非空 reasoning 行总数。
- 不展示最后 3/5 行原文。
- 标题继续使用 `reasoningHeaderStyle`，保持与 assistant final text 的层级差异。
- 这是 renderer 投影规则，不合并或改写 transcript segment；工具前后的 reasoning 边界在 `normal` 和 `detail` 都必须保留。

`detail` 模式：

- 保留当前策略：live 展示最后 5 条非空 reasoning 行，completed/final 展示最后 3 条非空
  reasoning 行。
- 仍走统一的 reasoning renderer，避免 live、completed、final 三条路径出现不一致。

## Tool 渲染

完整 tool args、partial、result、error 仍保存在 tool segment 中。`normal` 展示扫描友好的
紧凑摘要，并在完成态保留一行输出预览。

`normal` 模式示例：

```text
✓ list_dir . · 0ms · 114 chars
  │ Result: AGENTS.md
● shell running 1.2s
✗ read_file 4ms · 1.8 KB
  │ Error: no such file
```

规则：

- 头部保留状态图标、工具名、耗时、result/partial size。
- 如果存在稳定、短的关键参数，可在工具名后追加一个值，例如 `list_dir .`。第一版只使用
  单值参数或常见键 `path` / `cmd` / `command`，且必须截断到终端宽度内。
- 成功态最多展示 1 行 result preview，用于保留 `tool call -> output -> tool call` 的真实顺序线索。
- 错误态最多展示 1 行 error preview，因为错误对用户有直接操作价值。

`detail` 模式：

- 保留当前结构化多行块。
- 参数按 key 排序。
- running preview、result/error preview 继续限制最多 4 条非空行，并显示隐藏行数。
- 保持已有宽度预算与 `lipgloss.Width` 测试方式。

## 状态位置

展示模式属于 `cmd/ori` TUI shell 状态，建议放在 `interactiveModel` 中，例如：

```go
type transcriptDetailMode string

const (
    transcriptDetailNormal transcriptDetailMode = "normal"
    transcriptDetailDetail transcriptDetailMode = "detail"
)
```

`renderContext` 增加 detail mode 字段，由 `refreshTranscriptViewport` 传入 renderer。
这样 renderer 保持纯函数，`View()` 仍只组合 viewport、overlay、footer 和 input。

## 命令边界

`/view normal|detail` 是展示层命令，归 TUI 处理更合适：

- 不改变 dispatcher/runtime 行为。
- 不修改模型 reasoning 开关。
- 只更新 `interactiveModel` 的展示模式并刷新 viewport。

如果未来需要在 session 间持久化展示模式，再考虑接入 config 或 local TUI preference。
第一版保持 session-local，降低配置面复杂度。

## 文档更新

需要同步更新 `docs/TUI-GUIDE.md`：

- 活动流分层仍保持 user、assistant header、reasoning、tool、text/final、command/system、footer。
- Reasoning 摘要章节改为：默认 `normal` 按 segment header-only，`detail` 按 segment 展示尾部摘要。
- 工具调用块章节改为：默认 `normal` 紧凑摘要加一行输出预览，`detail` 使用结构化多行块。
- 测试重点增加展示模式切换、normal/detail 两种 renderer 输出。

## 测试计划

- `tui_renderer_test.go`：`normal` reasoning 保留多段边界，每段只包含 `thinking · N lines summarized`，不包含尾部原文。
- `tui_renderer_test.go`：`detail` reasoning 保持 live 5 行、completed/final 3 行策略。
- `tui_renderer_test.go`：`normal` tool 渲染紧凑摘要和一行 result/error preview，不渲染 args 和多行 result。
- `tui_renderer_test.go`：`detail` tool 保留参数、preview、result/error。
- `tui_render_test.go`：`/view normal|detail` 更新展示模式并刷新 viewport，不修改 transcript 数据。
- `tui_render_test.go`：未知参数返回 command block 中的用法提示。
- `go test ./cmd/ori`
- `make fmt`
- `make check`
- 真实 `go run ./cmd/ori agent --session cli:tui-view-smoke` smoke，验证默认输出更紧凑。

## 验收标准

- 默认 TUI 对话中，thinking 不再展示尾部原文。
- 默认 tool 块不再展示参数和多行 result preview，成功和失败工具都保留一行输出线索。
- `detail` 模式可以看到当前调试所需的 reasoning/tool 细节。
- `/reasoning` 与 `/view` 语义不混淆。
- 所有可见历史仍来自 transcript viewport，没有恢复第二输出平面。
