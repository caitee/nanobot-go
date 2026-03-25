# Bubbletea 与 Bubbles 终端 UI 库使用指南

## 设计思想

### Bubbletea：Elm 架构的 TUI 框架

Bubbletea 是 Charm 生态的核心框架，采用 **Elm 架构**（Model-Update-View），这是一种纯粹的函数式编程模式：

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
| **Update** | 处理消息并返回新的 Model + 命令 |
| **View** | 根据当前 Model 渲染界面 |
| **Msg** | 用户输入、系统事件（键盘、鼠标、计时器） |
| **Cmd** | 命令，用于执行副作用（IO、网络请求） |

**设计原则：**
- **单向数据流**：数据总是从 Model → View，事件通过 Msg 回传到 Update
- **不可变更新**：Update 返回新的 Model，不直接修改状态
- **命令式渲染**：View 是 Model 的纯函数，每次都完整重绘

### Bubbles：可复用组件库

Bubbles 是基于 Bubbletea 架构的 **组件库**，提供开箱即用的 UI 元素：

| 组件 | 用途 |
|------|------|
| `list` | 可滚动列表（文件浏览、菜单） |
| `textinput` | 单行文本输入 |
| `textarea` | 多行文本输入 |
| `progress` | 进度条 |
| `spinner` | 加载动画 |
| `table` | 表格 |
| `pager` | 分页器 |
| `viewport` | 可滚动视口 |
| `keypair` | 键值对输入 |

**使用哲学：** Bubbles 组件本身就是完整的 Bubbletea 程序——它们有自己的 Model、Update、View，可以直接嵌入到你的应用程序中。

---

## 核心依赖

```go
github.com/charmbracelet/bubbletea    // 框架
github.com/charmbracelet/lipgloss     // 样式系统
github.com/charmbracelet/bubbles      // 组件库
```

---

## 最小示例：计数器

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

---

## 四条黄金法则

Bubbletea 布局的核心规则，防止 90% 的 TUI 布局 bug：

### 法则 1：始终计算边框

**高度计算时要减去边框占用的 2 行。**

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

### 法则 2：禁止自动换行

**始终显式截断文本，防止面板高度不一致。**

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

### 法则 3：鼠标检测匹配布局方向

| 布局方向 | 使用坐标 |
|---------|---------|
| 水平并列 | `msg.X` |
| 垂直堆叠 | `msg.Y` |

```go
func (m model) handleLeftClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
    if m.shouldUseVerticalStack() {
        // 垂直模式：用 Y 坐标
        relY := msg.Y - contentStartY
        if relY < topHeight {
            m.focusedPanel = "top"
        }
    } else {
        // 水平模式：用 X 坐标
        if msg.X < leftWidth {
            m.focusedPanel = "left"
        }
    }
    return m, nil
}
```

### 法则 4：使用权重而非像素

**比例布局在终端缩放时完美适应。**

```go
// 聚焦面板获得更大权重 (2:1 = 66%/33%)
leftWeight, rightWeight := 2, 1
totalWeight := leftWeight + rightWeight

leftWidth := (availableWidth * leftWeight) / totalWeight
rightWidth := availableWidth - leftWidth
```

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

## 项目结构模板

```
your-app/
├── main.go              # 入口 (最小化，约 20 行)
├── types.go             # 类型定义、结构体
├── model.go             # Model 初始化和布局计算
├── update.go            # 消息分发器
├── update_keyboard.go   # 键盘处理
├── update_mouse.go      # 鼠标处理
├── view.go              # 视图渲染
├── styles.go            # Lipgloss 样式定义
└── config.go            # 配置管理
```

**main.go 最小示例：**
```go
package main

import (
    "os"
    "github.com/charmbracelet/bubbletea"
)

func main() {
    m := NewModel()
    p := tea.NewProgram(m, tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "错误: %v", err)
        os.Exit(1)
    }
}
```

---

## 常用 Tea 选项

```go
tea.NewProgram(m,
    tea.WithAltScreen(),       // 使用替代屏幕（不闪烁）
    tea.WithMouseCellMotion(), // 启用鼠标移动检测
    tea.With CrispEdges(),     // 禁用抗锯齿
)
```

---

## Bubbletea vs Bubbles 选择

| 场景 | 推荐 |
|------|------|
| 需要基础 UI 元素（列表、输入框、进度条） | Bubbles 组件 |
| 需要自定义渲染和复杂布局 | Bubbletea + Lipgloss |
| 需要快速原型 | Bubbles 组合 |
| 需要完全控制渲染 | 纯 Bubbletea + Lipgloss |

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

1. **高度不对？** 检查是否减去 2 行的边框
2. **文本换行？** 显式截断到 `maxWidth = panelWidth - 4`
3. **鼠标点击失效？** 确认布局方向与坐标匹配
4. **布局不响应？** 使用权重而非固定像素

---

## 参考资源

- [Bubbletea 官方文档](https://github.com/charmbracelet/bubbletea)
- [Lipgloss 文档](https://github.com/charmbracelet/lipgloss)
- [Bubbles 组件](https://github.com/charmbracelet/bubbles)
- [Charm 生态](https://charm.sh/)
