# TUI 渲染重构设计

日期：2026-05-15

## 背景

当前 `ori agent` 的交互式 TUI 已经能工作，但渲染路径不稳定，主要原因不是单个字符串拼接函数的问题，而是可见输出被拆成了两套平面：

- Bubble Tea `View()` 负责显示当前活动窗口，包括 `currentRound`、`displayedText`、状态行、输入框、补全和管理面板。
- `printAbove` / `program.Println` 负责把用户输入、assistant header、完成的 reasoning/tool round、最终回复和命令结果提交到终端 scrollback。

这种结构让业务状态、渲染状态、终端提交策略和缓存策略互相缠绕。历史上多次出现的重复输出、顺序错乱、stream/final 去重、tool 状态残留，都集中在 `currentRound`、`displayedText`、`flushedText`、`formatFinalMessage` 和 `maybeFlushStreamWindow` 的边界上。

本设计面向实验性项目，优先追求架构合理性和后续可维护性。不为多客户端复用设计公共 API，也不把 TUI 渲染逻辑上提到 `internal/app` 或 `internal/runtime`。

## 目标

1. 使用 Bubble Tea 管理完整 transcript viewport，让所有可见历史都来自 TUI 模型。
2. 删除正常路径上的 `printAbove` 双平面提交，消除 stream/final 跨平面去重。
3. 将事件归并、transcript 状态、渲染、viewport shell、overlay 管理拆成清晰边界。
4. 允许重新设计 TUI 呈现，只保留“终端可读、稳定、可测试”的用户体验要求。
5. 让 slash command、management panel、session replay、reasoning、tool call 都通过统一 block/segment 模型扩展。

## 非目标

1. 不为 Web、HTTP channel、外部客户端抽象通用 UI API。
2. 不修改 `internal/runtime` 的事件语义，也不把 provider/tool 特性放进 runtime。
3. 不要求兼容当前 TUI 样式、留白、颜色和 header 位置。
4. 不在第一阶段追求复杂动效。打字机效果可以先取消，后续作为 segment-level display length 再加回。

## 当前实现问题

### 双平面输出

`View()` 只知道当前活动内容，完成内容被打印到 TUI 上方。于是同一条 assistant 回复被拆成：

- live text：`displayedText` 和 `typewriterQueue`
- 已 flush 文本：`flushedText`
- 当前 reasoning/tool：`currentRound`
- 最终 assistant 内容：`responseMsg` 或 `EventAgentEnd`

这要求 finalize 时手动判断哪些内容已经打印过，哪些内容还在 live window 中，哪些 reasoning/tool round 已经 flush。任何事件顺序变化都会放大复杂度。

### 渲染与状态修改混杂

`tui_update.go` 在处理 runtime event 时会直接调用 formatter、提交输出、清状态、调整状态行和缓存版本。`tui_view.go` 又负责业务内容拼接。`output.go`、`tui_command.go`、`tui_management.go` 各自渲染不同输出类型，缺少统一的 render contract。

### 扩展点不清晰

新增一个可见元素时，需要判断它应该进 `View()`、`printAbove`、命令结果、management panel 还是 session replay。不同路径的宽度预算、Markdown fallback、颜色和截断规则也不完全一致。

## 推荐架构

采用 `event reducer -> transcript model -> renderer -> viewport shell` 四层结构，全部留在 `cmd/ori` 内。

```text
runtime.Event / bus.OutboundMessage / key input
        |
        v
tui_reducer.go
        |
        +-- transcript mutation
        +-- viewport follow-tail decision
        +-- overlay open/close/update
        +-- optional effects: dispatch prompt, clear, quit
        v
Bubble Tea model state
        |
        v
tui_renderer.go + viewport.Model
```

建议文件职责：

- `tui_model.go`：Bubble Tea shell 状态、dispatcher wiring、input、viewport、focus、overlay 引用。
- `tui_transcript.go`：TUI 私有 transcript/block/segment 数据结构和基础 mutation helpers。
- `tui_reducer.go`：把 runtime event、outbound final、slash command result 和部分 key action 转成 transcript mutation。
- `tui_renderer.go`：渲染 transcript block、reasoning、tool、command、system block，统一 Markdown、宽度和 fallback。
- `tui_overlay.go`：管理 `/mcp`、`/skills`、`/config`、`/sessions` 的交互面板。
- `tui_view.go`：只组合 viewport、overlay、status、slash suggestions、input。
- `output.go`：迁移期间保留少量 legacy formatter；重构完成后只保留非交互 CLI 需要的 formatter 或通用小工具。

## 核心数据模型

TUI 模型维护一个完整可见 transcript。每个用户输入、assistant 回复、命令结果、系统提示都是 block。assistant block 内部使用 segment 表示 reasoning、tool call、text 等顺序内容。

```go
type Transcript struct {
    Blocks            []Block
    ActiveAssistantID string
}

type Block struct {
    ID        string
    Kind      BlockKind
    CreatedAt time.Time

    User      *UserBlock
    Assistant *AssistantBlock
    Command   *CommandBlock
    System    *SystemBlock
}

type AssistantBlock struct {
    Status         AssistantStatus
    Segments       []AssistantSegment
    FinalText      string
    FinalReasoning string
    FinalSource    FinalSource
    RenderCursor   bool
}

type AssistantSegment struct {
    Kind      SegmentKind
    Reasoning *ReasoningSegment
    Tool      *ToolCallSegment
    Text      *TextSegment
}

type ToolCallSegment struct {
    ID        string
    Name      string
    Args      map[string]any
    Status    ToolStatus
    Partial   string
    Result    string
    StartedAt time.Time
    EndedAt   time.Time
    Expanded  bool
}
```

状态迁移后删除这些旧字段的正常路径职责：

- `flushedText`：删除。历史由 transcript viewport 持有，不再需要记录已打印前缀。
- `currentRound`：删除。reasoning/tool/text 都是 active assistant block 的 segments。
- `displayedText` / `typewriterQueue`：第一阶段取消打字机效果，直接用 text segment 内容渲染。后续如需恢复，可以在 `TextSegment` 加 `VisibleRunes`。
- `streamText`：可被最后一个 text segment 替代。必要时保留为 helper，不作为独立渲染状态。

## 事件归并规则

### 普通 prompt

用户按 Enter 提交普通 prompt 时：

1. 追加 `UserBlock`。
2. 追加 `AssistantBlock{Status: waiting}`。
3. 设置 `Transcript.ActiveAssistantID`。
4. 发布 inbound message。
5. viewport 如果处于底部，则 follow tail。

### Runtime events

- `EventAgentStart`：active assistant 状态变为 `thinking`。
- `EventTurnStart`：不 flush 内容。若需要可见分轮，后续可追加轻量 separator segment；第一版不显示 separator。
- `ThinkingDelta`：追加或合并到当前 assistant 的最后一个 reasoning segment。
- `TextDelta`：追加或合并到当前 assistant 的最后一个 text segment，状态变为 `responding`。
- `ToolExecutionStart`：追加 tool segment，状态为 `running`，assistant 状态变为 `running_tools`。
- `ToolExecUpdate`：更新对应 tool segment 的 `Partial` 和更新时间。
- `ToolExecutionEnd`：更新 tool segment 的 `Result`、`Status`、`EndedAt` 和 duration；若没有其他 running tool，assistant 状态回到 `thinking` 或保持 `responding`，取决于最近 segment 类型。
- `EventAgentEnd`：用 `appcore.ExtractFinalAssistant` 得到最终文本和 reasoning，将 assistant 标记为 `done`，并合并 stream/final 文本。

### Outbound fallback

`bus.OutboundMessage` 只作为 fallback finalize：

- 如果 active assistant 已经由 `EventAgentEnd` 标记为 done，忽略 outbound final。
- 如果 runtime event 丢失或延迟，使用 outbound content/reasoning finalize active assistant，并设置 `FinalSource: fallback`。

### Ctrl-C / cancel

活跃响应期间 Ctrl-C：

- 将 active assistant 标记为 `cancelled`。
- 保留已收到的 reasoning、tool、text segments。
- 停止 waiting/active 状态。
- 不再调用 `formatCurrentState()` 拼临时输出。

### Orphan tool events

如果 tool update/end 找不到对应 start segment，不静默丢弃。创建 orphan tool segment，填入可得的 name/id/result，并在 tool segment 上标记 `Orphan: true`。第一版不追加 system warning，避免用诊断信息污染 transcript；renderer 可以在该 tool 行上显示轻量 orphan 标记。

## Final 合并规则

stream/final 合并发生在同一个 assistant block 内，不再依赖 `flushedText`。核心 helper：

```go
func mergeFinalText(streamed, final string) string
```

规则：

1. `final == ""`：保留 `streamed`。
2. `streamed == ""`：使用 `final`。
3. `strings.HasPrefix(final, streamed)`：使用 `final`。
4. `strings.HasPrefix(streamed, final)`：保留 `streamed`。
5. 其他不一致情况：优先 `final`，设置 `FinalSource` 为实际来源，并将 `MergeConflict` 标记为 true，供测试和诊断。

## Renderer 设计

Renderer 是纯函数层，不修改状态，不调用 dispatcher，不发送 Bubble Tea command。

```go
type RenderContext struct {
    Width  int
    Focus  FocusArea
    Active bool
    Now    time.Time
}

type TranscriptRenderer struct {
    markdown MarkdownRenderer
    styles   TUIStyles
}

func (r *TranscriptRenderer) RenderTranscript(t Transcript, ctx RenderContext) string
func (r *TranscriptRenderer) RenderBlock(b Block, ctx RenderContext) string
func (r *TranscriptRenderer) RenderOverlay(o Overlay, ctx RenderContext) string
```

渲染约定：

- User block：使用稳定前缀或轻量背景，不手动 padding 到整屏宽。
- Assistant block：只在 block 顶部显示一次 assistant header；内部 segment 按顺序渲染。
- Reasoning segment：默认折叠显示最近 N 行摘要，保留后续展开入口。
- Text segment：Markdown 渲染失败时回退 plain text。
- Tool segment：统一渲染 `running/done/error`，args/result 使用 key-value 和 preview 组件。
- Command block：记录命令和结果；`CommandResult.Text` 按 plain text 渲染，只有 `Markdown` 字段走 Markdown。
- System block：用于错误、取消、session 切换、clear 后 banner 等状态消息。

宽度策略统一由 renderer 管理。所有截断和预览使用 `lipgloss.Width`，避免 CJK/unicode 溢出。

## Viewport 和焦点

使用 Bubble Tea `viewport.Model` 管理 transcript 历史。

- 每次 transcript 变化后重新渲染 viewport content。
- 如果用户在底部，自动 follow tail。
- 如果用户滚动查看历史，保持滚动位置，并在 status bar 显示 new output。
- input、status bar、slash suggestions、overlay 不进入 transcript viewport。
- `/clear` 清空 transcript 并追加 fresh system/banner block，不清终端 scrollback 作为主要语义。

焦点建议分为：

- `FocusInput`
- `FocusTranscript`
- `FocusOverlay`

普通输入时焦点在 input；PageUp/PageDown 或类似快捷键可滚动 transcript；打开 management panel 后焦点转 overlay，Esc 返回 input。

## Command 和 Overlay

slash command 分为 transcript 记录和交互 overlay 两部分。

- `/help`、`/status`、`/reasoning` 等纯输出命令追加 `CommandBlock`。
- `/mcp`、`/skills`、`/config`、`/sessions` 先追加 `CommandBlock`，再打开对应 overlay。
- overlay 拥有独立 selection/page/edit 状态。
- overlay 渲染不修改 transcript。
- Esc 关闭 overlay，焦点回 input。
- session resume 是 overlay action：清空 transcript，加载目标 session 成 blocks，追加 system block 表示已切换 session，并更新 runtime subscription。

## Session Replay

session replay 不再使用单独 formatter。它应复用 transcript 构造逻辑：

- user message -> `UserBlock`
- assistant message with text/thinking/tool calls -> `AssistantBlock`
- tool result message -> 合并到对应 tool segment；找不到对应 tool call 时创建 orphan tool segment
- standalone system or unsupported role -> `SystemBlock`

这样历史回放、实时响应和最终输出共用 renderer，减少“现场看起来正常，resume 后又另一套样式”的问题。

## 错误处理

- reducer 遇到异常状态时更新 block 状态或追加 `SystemBlock{Level: warning/error}`。
- Markdown 渲染失败只在 renderer 内回退 plain text，不污染 transcript。
- runtime final 丢失时由 outbound fallback finalize，并记录 `FinalSource: fallback`。
- tool orphan 不丢弃，保证用户至少能看到工具结果。
- renderer 不返回业务错误；业务错误由 reducer 或 command handler 转成 block 状态。

## 迁移计划

1. 新增 `Transcript`、block、segment 类型和 reducer helper，先写纯单元测试。
2. 将普通 prompt、thinking/text/tool/final 路径写入 transcript，同时保留旧 renderer 做行为对照。
3. 引入 `viewport.Model`，把 `View()` 改为 transcript viewport + status + input。
4. 正常响应路径停止使用 `printAbove`。
5. 将 slash command 输出迁移为 `CommandBlock`，`UIRequest` 同时打开 overlay。
6. 将 management panel 渲染迁移到 overlay，去掉它对主 `View()` 的直接拼接。
7. 将 session replay 改为构造 transcript blocks。
8. 删除 `flushedText`、`currentRound`、`displayedText`、`typewriterQueue`、`formatCurrentState`、`formatFinalMessage`、`maybeFlushStreamWindow` 等旧路径。
9. 整理 `output.go`，保留非交互 CLI 所需函数或迁移到 renderer helpers。
10. 做终端人工验证，确认长回复、tool call、多轮、命令、overlay、resume 都不会重复或错序。

## 测试策略

### Reducer tests

覆盖：

- prompt start 创建 user + assistant block
- thinking/text deltas 合并到正确 segment
- tool start/update/end 生命周期
- 多 turn 事件不会丢失前一轮 segment
- agent_end final 合并
- outbound fallback finalize
- Ctrl-C cancel 保留已有内容
- orphan tool end 可见

### Final merge tests

覆盖：

- streamed 为空
- final 为空
- final 以 streamed 为前缀
- streamed 以 final 为前缀
- 两者不一致时优先 final 并可诊断

### Renderer tests

覆盖：

- block 顺序稳定
- reasoning 折叠
- tool preview 和 hidden line count
- command text 不误走 Markdown
- explicit Markdown command 走 Markdown
- 窄终端下 tool args/result 不溢出

测试断言继续避免依赖 ANSI 完整输出，优先使用 plain output、结构化状态和 `lipgloss.Width`。

### Viewport tests

覆盖：

- 在底部时新输出 follow tail
- 用户滚动离底后不抢滚动位置
- 新输出提示出现
- clear 清空 transcript 并追加 fresh system/banner block

### Command / Overlay tests

覆盖：

- `/mcp` 追加 command block 并打开 overlay
- Esc 关闭 overlay
- overlay selection/page/edit 状态独立
- session resume 清空并加载 transcript blocks

### 回归验证

开发期间使用：

```sh
go test ./cmd/ori
```

完成代码改动后运行：

```sh
make fmt
make check
```

收尾时确认顶层 `ori`、`gateway` 等生成二进制未混入源码 diff，除非任务明确要求更新构建产物。

## 验收标准

1. 正常对话、长流式回复、tool call、多 turn、final fallback 不再依赖 `flushedText` 去重。
2. `View()` 不直接拼业务内容，只组合 renderer 输出、overlay、status 和 input。
3. reducer 可用纯单元测试覆盖主要事件顺序。
4. slash command 输出、management overlay、session replay 使用同一 transcript/renderer 体系。
5. `go test ./cmd/ori` 通过；实现完成时 `make fmt` 和 `make check` 通过或明确记录与本任务无关的既有失败。
