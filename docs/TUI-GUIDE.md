# Ori TUI 设计与实现指南

本文档记录 `cmd/ori` 中交互式 TUI 的实现约定。它不是通用 Bubbletea
教程，而是维护 `ori agent`、onboarding wizard 和相关终端渲染代码时应遵守的
项目指南。

Ori 的 TUI 当前基于 Charm 生态：

```go
github.com/charmbracelet/bubbletea    // Elm-style TUI runtime
github.com/charmbracelet/bubbles      // textinput 等组件
github.com/charmbracelet/lipgloss     // 终端样式和显示宽度计算
github.com/charmbracelet/glamour      // Markdown 渲染
```

## 设计思想

### Bubbletea：Elm-style 数据流

Bubbletea 采用 Model-Update-View 数据流。Ori 的实现使用 Go 指针 receiver，
因此不是严格不可变模型；实际约定是：状态变更集中在 `Update`，副作用通过
`tea.Cmd` 或明确封装的打印函数执行，`View` 保持只读渲染。

```
┌─────────────────────────────────────────────────────┐
│                     Elm 架构                         │
├─────────────────────────────────────────────────────┤
│                                                     │
│   ┌─────────┐    ┌──────────┐    ┌──────────────┐   │
│   │  用户   │───▶│  Update  │───▶│    View      │   │
│   │  输入   │    │   函数   │    │   (渲染)     │   │
│   └─────────┘    └──────────┘    └──────────────┘   │
│                      │                              │
│                      ▼                              │
│               ┌──────────┐                          │
│               │  Model   │                          │
│               │  (状态)  │                          │
│               └──────────┘                          │
│                                                     │
└─────────────────────────────────────────────────────┘
```

**核心概念：**

| 概念 | 说明 |
|------|------|
| **Model** | 应用程序的全部状态（唯一数据源） |
| **Update** | 处理消息，更新 Model，并返回后续命令 |
| **View** | 根据当前 Model 渲染界面，不做 IO |
| **Msg** | 用户输入、runtime event、outbound message、计时器 tick |
| **Cmd** | 延迟执行副作用，例如 tick、quit |

**设计原则：**
- **单向数据流**：数据总是从 Model → View，事件通过 Msg 回传到 Update
- **集中状态转换**：runtime event、键盘输入、spinner tick 都在 `Update` 中转换为 Model 状态
- **View 只读**：除缓存字段外，`View` 不应触发业务状态变化或外部副作用
- **副作用显式化**：退出程序、定时 tick 都通过 `tea.Cmd` 返回；可见输出进入 transcript viewport，不绕到终端历史区

### Bubbles：只用于必要组件

Bubbles 提供可嵌入的 UI 组件。Ori 当前主要使用 `textinput` 和 `viewport`，
不要为了简单状态引入额外组件；只有当组件能明显减少复杂交互代码时再使用。

| 组件 | 用途 |
|------|------|
| `textinput` | `ori agent` 输入行、onboarding 字段编辑 |
| `list` | 可滚动菜单或选择器，当前主 TUI 未使用 |
| `textarea` | 多行文本输入 |
| `progress` | 进度条 |
| `spinner` | 加载动画 |
| `table` | 表格 |
| `pager` | 分页器 |
| `viewport` | 可滚动视口 |

---

## 最小 Bubbletea 示例

```go
package main

import (
    "fmt"
    "github.com/charmbracelet/bubbletea"
)

type Model struct { count int }

func (m Model) Init() bubbletea.Cmd { return nil }

func (m Model) Update(msg bubbletea.Msg) (bubbletea.Model, bubbletea.Cmd) {
    switch msg := msg.(type) {
    case bubbletea.KeyMsg:
        switch msg.String() {
        case "q", "ctrl+c":
            return m, bubbletea.Quit
        case "+":
            m.count++
        case "-":
            m.count--
        }
    }
    return m, nil
}

func (m Model) View() string {
    return fmt.Sprintf("计数器: %d\n按 q 退出\n", m.count)
}

func main() {
    bubbletea.NewProgram(Model{count: 0}).Run()
}
```

Ori 的实际 `interactiveModel` 比这个例子多了 runtime event pump、outbound
fallback、transcript model、renderer、viewport、overlay 和 render cache。改动时
优先理解 `cmd/ori/tui_model.go`、`cmd/ori/tui_reducer.go`、`cmd/ori/tui_renderer.go`
和 `cmd/ori/tui_view.go` 的职责边界。

---

## 四条黄金法则

Ori TUI 布局和渲染的核心规则：

### 法则 1：始终计算边框

如果使用有边框的组件，高度计算时要减去边框占用的 2 行。

```
总高度: 25
- 标题栏: -3
- 状态栏: -1
- 面板边框: -2
─────────────
可用高度: 19 ✓
```

```go
func (m model) calculateLayout() (int, int) {
    contentWidth := m.width
    contentHeight := m.height

    if m.config.UI.ShowTitle {
        contentHeight -= 3 // 标题栏
    }
    if m.config.UI.ShowStatus {
        contentHeight -= 1 // 状态栏
    }

    // 关键：减去边框
    contentHeight -= 2

    return contentWidth, contentHeight
}
```

### 法则 2：显式约束长文本

不要依赖终端自动换行来控制工具详情、状态栏或固定格式 UI。Markdown 正文可以
由 glamour word wrap；工具参数、工具结果 preview、状态行必须显式截断到预算宽度。

```go
maxTextWidth := panelWidth - 4 // -2 边框, -2 内边距

func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen-1] + "…"
}

title = truncateString(title, maxTextWidth)
```

### 法则 3：事件语义先于视觉效果

`ori agent` 的渲染来源是 `runtime.Event`。不要在 View 层猜测业务阶段；应在
`Update` 中把事件交给 reducer，转换为 transcript block/segment 状态：

- `agent_start` / `turn_start` 确保有 active assistant block，并推进状态
- `message_update` 追加 reasoning 或 text segment
- `tool_execution_start/update/end` 更新 tool segment
- `agent_end` 合并 runtime final，结束 active assistant block
- outbound fallback 只在 runtime final 没先到达时补齐最终文本

### 法则 4：使用权重而非像素

需要多区域布局时使用比例或终端宽高计算，不写死像素或列数。`ori agent` 当前
主界面是 transcript viewport + overlay + footer + input 的纵向结构；新增面板时要
重新审视高度预算、focus routing 和 viewport scroll 行为。

```go
// 聚焦面板获得更大权重 (2:1 = 66%/33%)
leftWeight, rightWeight := 2, 1
totalWeight := leftWeight + rightWeight

leftWidth := (availableWidth * leftWeight) / totalWeight
rightWidth := availableWidth - leftWidth
```

---

## Ori Agent 活动流渲染约定

`ori agent` 的交互输出不是普通日志，而是运行中的活动流。渲染层只消费
`runtime.Event`，不改变 `internal/runtime`、`internal/llm`、`internal/tool`
的事件契约。当前实现位于 `cmd/ori`：

| 文件 | 职责 |
|------|------|
| `tui_model.go` | Bubbletea model、runtime pump、transcript/viewport/focus/overlay 状态 |
| `tui_transcript.go` | transcript block/segment 数据结构与局部 mutation helper |
| `tui_reducer.go` | runtime event、command result、session replay 到 transcript 的状态转换 |
| `tui_renderer.go` | transcript 到终端文本的纯渲染 |
| `tui_view.go` | viewport shell、底部状态栏、输入框整体 View |
| `output.go` | Markdown、reasoning、参数格式化等共享 helper |
| `styles.go` | Lipgloss 样式和 spinner frames |
| `tui_render_test.go` | 渲染行为测试 |

### 活动流分层

每次 assistant 响应按下面的视觉层级组织：

1. user block：用户输入。
2. assistant block header：`✦ ori` 与 assistant 状态。
3. reasoning segment：展示模型 reasoning 摘要。
4. tool segment：展示工具名、参数、运行中 preview、最终 result/error。
5. text segment / final text：展示 assistant 文本。
6. command/system block：展示 slash command 结果和系统提示。
7. footer status：展示全局状态和输入框。

不要把这些层级重新合并成一段自由文本。`View()` 渲染的是 viewport 中的
transcript 快照；已完成和进行中的内容都留在 transcript 内，不通过 `printAbove`
或终端 scrollback 形成第二套可见历史。

### Reasoning 摘要

Reasoning 默认不全量展示。完整内容保留在 runtime/session 数据里，TUI 只显示摘要。
当前 renderer 会先取非空原始行，再交给 reasoning Markdown renderer：

- live 模式展示最后 5 条非空 reasoning 行。
- completed/final 模式展示最后 3 条非空 reasoning 行。
- 三条路径都必须使用同一个 `renderReasoningBlock`，避免 live 截断但 final 全量展开。
- 标题格式为 `thinking · N lines summarized`。

如果要调整展示行数，必须同时更新 live、completed、final 的测试，确认三条路径一致。

### 工具调用块

工具调用默认使用结构化多行块，而不是单行 `Args:` / `Result:`：

```text
  ● shell running 1.2s
    │ command    go test ./cmd/ori
    │ timeout    30
    │ Preview
    │ ok   ori/cmd/ori  0.48s
```

约定：

- 参数按 key 排序，保证 snapshot 和测试稳定。
- 单值和多值参数都走 key/value 行，长 key 和长 value 必须截断到终端宽度内。
- `tool_execution_update` 用于运行中 preview；没有 partial update 的工具只显示 start/end。
- result/error 默认展示最多 4 条非空行，超出时显示 `... N more lines`。
- 完成态显示 `✓ tool duration · size`，错误态显示 `✗ tool duration · size`。
- 工具块只做扫读预览，不替代完整工具结果。

### 动画职责

底部状态栏和工具运行态必须使用不同视觉语言：

- footer 使用 `spinnerFrames` 表示全局 `waiting`、`thinking`、`responding`、`running tools`。
- 工具运行态使用静态 pulse/dot 和 `running <duration>`，不要复用 footer spinner。
- 工具结束后 footer 应回落到 `thinking`，除非还有其他工具仍在运行。

这样用户能区分“整个 agent 正在推进”和“某个工具正在运行”，避免两个相同 spinner
同时出现造成层级混淆。

### 宽度与缓存

渲染宽度一律来自 `renderContext.width` / `getTerminalWidth()`，并用 `lipgloss.Width`
计算 display width。新增工具详情行时要遵守：

- 不依赖终端自动换行。
- 所有 args/result/error 行在窄终端下也不能超过 terminal width。
- 长文本先归一化换行，再按 display width 截断。
- 尽量让 transcript repaint 由真实内容变化触发；spinner tick 只负责全局 spinner
  和 running tool elapsed 这种 timer-only repaint。

### 测试要求

修改 `cmd/ori` TUI 渲染时至少运行：

```bash
go test ./cmd/ori
```

涉及 Go 代码改动时最终运行：

```bash
make fmt
make check
```

重点覆盖：

- reasoning live/completed/final 都只显示摘要。
- final response 不重复打印已流式展示的 text segment。
- 工具参数按 key 稳定排序，并渲染为多行块。
- 长工具 result 显示 preview 和隐藏行数。
- `EventToolExecUpdate` 更新运行中 preview。
- 工具运行态不复用 footer spinner。
- 80 列和窄终端下工具详情行不溢出。
- PageUp/PageDown 和鼠标滚轮都驱动 transcript viewport；overlay 打开时滚轮不滚背后的历史。
- running tool elapsed 可重绘，但 timer-only repaint 不触发 new-output 提示。

---

## 组件使用示例

### List 列表

```go
import "github.com/charmbracelet/bubbles/list"

type item struct { title, desc string }
func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title }

type Model struct { list list.Model }

func (m Model) Init() tea.Cmd {
    items := []list.Item{
        item{title: "文件 1", desc: "描述1"},
        item{title: "文件 2", desc: "描述2"},
    }
    m.list = list.New(items, list.NewDefaultDelegate(), 0, 0)
    return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    m.list, cmd = m.list.Update(msg)
    return m, cmd
}
```

### TextInput 文本输入

```go
import "github.com/charmbracelet/bubbles/textinput"

type Model struct { input textinput.Model }

func (m Model) Init() tea.Cmd {
    m.input = textinput.New()
    m.input.Placeholder = "输入内容..."
    m.input.Focus()
    return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    m.input, cmd = m.input.Update(msg)
    return m, cmd
}

func (m Model) View() string {
    return m.input.View()
}
```

### Progress 进度条

```go
import "github.com/charmbracelet/bubbles/progress"

type Model struct { progress progress.Model }

func (m Model) Init() tea.Cmd {
    m.progress = progress.New(progress.WithScaledGradient("#61AFEF", "#98C379"))
    return nil
}

// 在 Update 中设置进度
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key.Matches(msg, tea.KeySpace) {
        m.progress.SetValue(m.progress.Value() + 0.1)
    }
    return m, nil
}
```

---

## 样式系统 (Lipgloss)

```go
import "github.com/charmbracelet/lipgloss"

var panelStyle = lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    Padding(1, 2).
    Foreground(lipgloss.Color("#61AFEF"))

// 在 View 中使用
func (m Model) View() string {
    return panelStyle.Render("面板内容")
}
```

**常用样式属性：**
- `Border()` - 边框样式（NormalBorder, RoundedBorder, DoubleBorder...）
- `Padding()` - 内边距
- `Margin()` - 外边距
- `Foreground()` / `Background()` - 颜色
- `Bold()` / `Italic()` / `Underline()` - 文本属性
- `Width()` / `Height()` - 尺寸约束

---

## Ori TUI 文件结构

`cmd/ori` 是 CLI/TUI 入口层，应保持薄入口，不放业务逻辑。当前 TUI 相关文件：

```text
cmd/ori/
├── cmd_agent.go       # agent 命令、配置加载、dispatcher wiring、program 启动
├── tui_model.go       # interactiveModel、runtime pump、transcript/viewport 状态
├── tui_transcript.go  # transcript block/segment model
├── tui_reducer.go     # runtime/command/session 到 transcript 的 reducer
├── tui_renderer.go    # transcript 纯渲染
├── tui_update.go      # Update、键盘输入、runtime event、finalize
├── tui_view.go        # View、viewport shell、状态栏和输入框
├── output.go          # Markdown、reasoning、参数格式化 helper
├── styles.go          # lipgloss 样式、terminal width/height、spinner frames
└── tui_render_test.go # TUI 渲染和 runtime event 行为测试
```

新增 TUI 行为时，优先把状态转换放进 `tui_reducer.go` 或 `tui_update.go`，
把纯渲染 helper 放进 `tui_renderer.go` 或 `output.go`。不要在命令、session resume
或 runtime final 路径里直接打印终端历史；这些内容都应进入 transcript block。

---

## 常用 Tea 选项

`ori agent` 保持普通终端历史可见：

```go
tea.NewProgram(m,
    tea.WithoutSignals(),
    tea.WithMouseCellMotion(),
)
```

`WithMouseCellMotion` 用于把鼠标滚轮送进 transcript viewport。它不等于 alt screen；
不要把对话历史重新拆回终端 scrollback。

onboarding wizard 更像独立配置界面，因此使用替代屏幕：

```go
tea.NewProgram(m,
    tea.WithAltScreen(),
)
```

不要在 `ori agent` 里随意切到 alt screen，否则会破坏历史区输出体验。

---

## Bubbletea vs Bubbles 选择

| 场景 | 推荐 |
|------|------|
| 输入框、列表、进度条等标准交互组件 | Bubbles 组件 |
| 需要自定义渲染和复杂布局 | Bubbletea + Lipgloss |
| runtime event 驱动的活动流 | 自定义 Model + renderer |
| Markdown assistant 输出 | Glamour + 缓存 |
| 固定格式工具详情 | Lipgloss + 显式截断 |

**组合使用：** 常见模式是在 Bubbletea 程序中使用 Bubbles 组件作为子模块：

```go
type Model struct {
    list   list.Model      // bubbles 组件
    input  textinput.Model // bubbles 组件
    custom string          // 自定义状态
}
```

---

## 调试技巧

1. **reasoning 又全量出现？** 检查 live、completed、final 是否都走 `renderReasoningBlock`。
2. **工具输出挤成一行？** 检查是否绕过了结构化 tool block renderer。
3. **工具动画和底部动画混淆？** 工具运行态不能使用 `spinnerFrames`。
4. **长输出导致布局跳动？** 使用 `lipgloss.Width`、`fitLine` 和 `fitPrefixedLine` 做显式截断。
5. **最终答案重复打印？** 检查 `mergeFinalText`、runtime final 与 outbound fallback 的合并顺序。
6. **长回答时 spinner 卡顿？** 检查是否在 tick 中重复运行完整 transcript render；只有 running tool elapsed 需要 timer-only repaint。

---

## 参考资源

- [Bubbletea 官方文档](https://github.com/charmbracelet/bubbletea)
- [Lipgloss 文档](https://github.com/charmbracelet/lipgloss)
- [Bubbles 组件](https://github.com/charmbracelet/bubbles)
- [Charm 生态](https://charm.sh/)
