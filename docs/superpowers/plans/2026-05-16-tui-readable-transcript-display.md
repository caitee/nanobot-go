# TUI Readable Transcript Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `normal/detail` TUI display modes so `ori agent` defaults to a readable transcript while preserving detailed reasoning/tool observability on demand.

**Architecture:** Keep all behavior inside the transcript-first TUI boundary. Runtime events continue to mutate transcript block/segment data; `renderContext` carries the display mode into the pure renderer; `/view normal|detail` updates only `interactiveModel` TUI state and refreshes the viewport.

**Tech Stack:** Go 1.24.2, Bubble Tea, Bubbles viewport, Lip Gloss, Glamour, `cmd/ori` transcript renderer and slash-command shell.

---

## File Structure

- Modify `cmd/ori/tui_renderer.go`: define display mode type, normalize render context, render reasoning/tool differently for `normal` and `detail`.
- Modify `cmd/ori/tui_renderer_test.go`: cover default readable rendering and detailed rendering.
- Modify `cmd/ori/tui_model.go`: store session-local TUI display mode and include it in view cache key.
- Modify `cmd/ori/tui_view.go`: pass display mode into `renderContext`.
- Modify `cmd/ori/tui_command.go`: intercept `/view normal|detail`, update display mode, and expose local command metadata.
- Modify `cmd/ori/tui_render_test.go`: cover `/view` command behavior and update existing detail-oriented tests to opt into `detail`.
- Modify `internal/app/commands.go`: register `/view` as TUI-scoped command metadata so help/completion know it exists while TUI still handles the command.
- Modify `internal/app/dispatcher_test.go`: assert `/view` appears in default command metadata.
- Modify `docs/TUI-GUIDE.md`: document `normal/detail`, reasoning header-only default, and tool single-line default.

---

### Task 1: Renderer Display Mode And Reasoning

**Files:**
- Modify: `cmd/ori/tui_renderer.go`
- Modify: `cmd/ori/tui_renderer_test.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Write failing tests for normal/detail reasoning**

Add these tests near the existing reasoning tests in `cmd/ori/tui_renderer_test.go`:

```go
func TestTranscriptRendererNormalReasoningIsHeaderOnly(t *testing.T) {
	now := time.Unix(120, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendReasoningDelta(strings.Join([]string{
		"hidden one",
		"hidden two",
		"tail one",
		"tail two",
		"tail three",
	}, "\n"), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewNormal,
	}))
	if !strings.Contains(out, "thinking · 5 lines summarized") {
		t.Fatalf("expected reasoning header in normal mode, got:\n%s", out)
	}
	for _, hidden := range []string{"hidden one", "hidden two", "tail one", "tail two", "tail three"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal mode should hide reasoning body %q, got:\n%s", hidden, out)
		}
	}
}

func TestTranscriptRendererDetailReasoningKeepsTailSummary(t *testing.T) {
	now := time.Unix(121, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.appendReasoningDelta(strings.Join([]string{
		"hidden one",
		"hidden two",
		"tail one",
		"tail two",
		"tail three",
	}, "\n"), now)

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    100,
		now:      now,
		viewMode: transcriptViewDetail,
	}))
	if strings.Contains(out, "hidden one") || strings.Contains(out, "hidden two") {
		t.Fatalf("detail mode should still hide older reasoning lines, got:\n%s", out)
	}
	for _, visible := range []string{"tail one", "tail two", "tail three"} {
		if !strings.Contains(out, visible) {
			t.Fatalf("detail mode should include reasoning tail %q, got:\n%s", visible, out)
		}
	}
}
```

Update the existing detail-oriented reasoning tests in `cmd/ori/tui_renderer_test.go` so they explicitly pass `viewMode: transcriptViewDetail`:

```go
out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
	width:    100,
	now:      now,
	viewMode: transcriptViewDetail,
}))
```

Apply that replacement in:

- `TestTranscriptRendererReasoningUsesLiveOnlyForActiveAssistant`
- `TestTranscriptRendererTerminalActiveAssistantUsesCompletedReasoning`

Update the direct reasoning helper test in `cmd/ori/tui_render_test.go` to call the new helper signature:

```go
live := plainView(renderReasoningBlockForWidth(reasoning, reasoningModeLive, 80, transcriptViewDetail))
completed := plainView(renderReasoningBlockForWidth(reasoning, reasoningModeCompleted, 80, transcriptViewDetail))
```

- [ ] **Step 2: Run reasoning tests and verify they fail**

Run:

```bash
go test ./cmd/ori -run 'TestTranscriptRenderer(NormalReasoningIsHeaderOnly|DetailReasoningKeepsTailSummary|ReasoningUsesLiveOnlyForActiveAssistant|TerminalActiveAssistantUsesCompletedReasoning)|TestTranscriptRendererReasoningBlockSummarizesLiveAndCompleted'
```

Expected: FAIL with errors such as `undefined: transcriptViewNormal`, `undefined: transcriptViewDetail`, or wrong `renderReasoningBlockForWidth` argument count.

- [ ] **Step 3: Implement display mode and reasoning projection**

In `cmd/ori/tui_renderer.go`, add the display mode type above `renderContext`:

```go
type transcriptViewMode string

const (
	transcriptViewNormal transcriptViewMode = "normal"
	transcriptViewDetail transcriptViewMode = "detail"
)

func normalizeTranscriptViewMode(mode transcriptViewMode) transcriptViewMode {
	if mode == transcriptViewDetail {
		return transcriptViewDetail
	}
	return transcriptViewNormal
}
```

Update `renderContext`:

```go
type renderContext struct {
	width    int
	focus    focusArea
	active   bool
	now      time.Time
	viewMode transcriptViewMode
}
```

Update `normalizeRenderContext`:

```go
func normalizeRenderContext(ctx renderContext) renderContext {
	if ctx.width <= 0 {
		ctx.width = getTerminalWidth()
	}
	if ctx.now.IsZero() {
		ctx.now = time.Now()
	}
	ctx.viewMode = normalizeTranscriptViewMode(ctx.viewMode)
	return ctx
}
```

Update the reasoning branch in `renderSegment`:

```go
case segmentKindReasoning:
	if seg.reasoning == nil {
		return ""
	}
	mode := reasoningModeCompleted
	if ctx.active {
		mode = reasoningModeLive
	}
	return renderReasoningBlockForWidth(seg.reasoning.text, mode, ctx.width, ctx.viewMode)
```

Replace `renderReasoningBlockForWidth` with:

```go
func renderReasoningBlockForWidth(reasoning string, mode reasoningRenderMode, width int, viewMode transcriptViewMode) string {
	lines := nonEmptyLines(reasoning)
	if len(lines) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(reasoningHeaderStyle.Render(fitLine(fmt.Sprintf("  thinking · %d lines summarized", len(lines)), width)))
	if normalizeTranscriptViewMode(viewMode) == transcriptViewNormal {
		return b.String()
	}
	visible := 3
	if mode == reasoningModeLive {
		visible = 5
	}
	if len(lines) < visible {
		visible = len(lines)
	}
	preview := strings.Join(lines[len(lines)-visible:], "\n")
	if rendered := renderReasoningMarkdownForWidth(preview, width); rendered != "" {
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	return b.String()
}
```

- [ ] **Step 4: Run reasoning tests and verify they pass**

Run:

```bash
go test ./cmd/ori -run 'TestTranscriptRenderer(NormalReasoningIsHeaderOnly|DetailReasoningKeepsTailSummary|ReasoningUsesLiveOnlyForActiveAssistant|TerminalActiveAssistantUsesCompletedReasoning)|TestTranscriptRendererReasoningBlockSummarizesLiveAndCompleted'
```

Expected: PASS.

- [ ] **Step 5: Commit renderer reasoning mode**

```bash
git add cmd/ori/tui_renderer.go cmd/ori/tui_renderer_test.go cmd/ori/tui_render_test.go
git commit -m "feat: add readable reasoning view mode"
```

---

### Task 2: Tool Normal And Detail Rendering

**Files:**
- Modify: `cmd/ori/tui_renderer.go`
- Modify: `cmd/ori/tui_renderer_test.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Write failing tests for tool display modes**

Add these tests near the existing tool renderer tests in `cmd/ori/tui_renderer_test.go`:

```go
func TestTranscriptRendererNormalToolShowsCompactResultPreview(t *testing.T) {
	now := time.Unix(122, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.upsertToolStart("call-1", "list_dir", map[string]any{"path": "."}, now)
	asst.finishTool("call-1", "list_dir", "alpha\nbeta", false, now.Add(3*time.Millisecond))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(3 * time.Millisecond),
		viewMode: transcriptViewNormal,
	}))
	for _, want := range []string{"✓ list_dir .", "3ms", "10 chars"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal tool summary to contain %q, got:\n%s", want, out)
		}
	}
	for _, want := range []string{"Result", "alpha"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal tool summary to include compact result preview %q, got:\n%s", want, out)
		}
	}
	for _, hidden := range []string{"│ path", "beta"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("normal tool summary should hide %q, got:\n%s", hidden, out)
		}
	}
}

func TestTranscriptRendererDetailToolKeepsArgumentsAndResultPreview(t *testing.T) {
	now := time.Unix(123, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.upsertToolStart("call-1", "list_dir", map[string]any{"path": "."}, now)
	asst.finishTool("call-1", "list_dir", "alpha\nbeta", false, now.Add(3*time.Millisecond))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(3 * time.Millisecond),
		viewMode: transcriptViewDetail,
	}))
	for _, want := range []string{"✓ list_dir", "│ path", "Result", "alpha", "beta"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected detail tool block to contain %q, got:\n%s", want, out)
		}
	}
}

func TestTranscriptRendererNormalToolErrorShowsOnePreviewLine(t *testing.T) {
	now := time.Unix(124, 0)
	var tr transcript
	asst := tr.appendAssistantBlock("asst-1", now)
	asst.upsertToolStart("call-1", "read_file", map[string]any{"path": "/tmp/missing"}, now)
	asst.finishTool("call-1", "read_file", "first error line\nsecond error line", true, now.Add(time.Millisecond))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{
		width:    80,
		now:      now.Add(time.Millisecond),
		viewMode: transcriptViewNormal,
	}))
	for _, want := range []string{"✗ read_file /tmp/missing", "Error", "first error line"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected normal error summary to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "second error line") {
		t.Fatalf("normal error summary should show only one error preview line, got:\n%s", out)
	}
}
```

Update existing tests that intentionally assert detailed tool args/result/preview to opt into `detail`:

```go
renderContext{width: 80, viewMode: transcriptViewDetail}
```

Apply this to the direct renderer calls in:

- `TestTranscriptRendererToolLinesFitNarrowWidth`
- `TestTranscriptRendererToolTinyWidthsFit`
- `TestTranscriptRendererAssistantStatusAndOrphanMarker`
- `TestTranscriptRendererFinalConflictUsesFinalTextOnce`
- `TestTranscriptRendererToolDetailLinesFitTerminalWidth`
- `TestHandleRuntimeEvent_TurnStartKeepsPreviousRoundInTranscript`
- `TestAgentEnd_FinalizesTranscriptWithToolCallsFromSameTurn`
- `TestTranscriptFromSessionMessagesBuildsBlocks`
- `TestTranscriptFromSessionMessagesMatchesDelayedToolResultByID`
- `TestAgentEnd_FinalizesTranscriptWithToolCallsFromPreviousTurn`
- `TestRenderToolArgumentLinesFitNarrowTerminal`
- `TestRenderToolResultShowsPreviewAndHiddenLineCount`

Update `View()`-level tests that expect detailed tool args or running preview by setting the model mode before rendering:

```go
m.viewMode = transcriptViewDetail
m.refreshTranscriptViewport()
```

Apply this to:

- `TestRenderToolCallUsesStableMultilineArguments`
- `TestHandleRuntimeEvent_ToolUpdateRendersRunningPreview`

- [ ] **Step 2: Run tool tests and verify they fail**

Run:

```bash
go test ./cmd/ori -run 'TestTranscriptRenderer(NormalToolShowsCompactResultPreview|DetailToolKeepsArgumentsAndResultPreview|NormalToolErrorShowsOnePreviewLine)|TestRenderTool(CallUsesStableMultilineArguments|ResultShowsPreviewAndHiddenLineCount|ArgumentLinesFitNarrowTerminal)|TestHandleRuntimeEvent_ToolUpdateRendersRunningPreview'
```

Expected: FAIL because normal mode still renders multiline args/result and helper functions do not exist.

- [ ] **Step 3: Implement normal tool summary and detail fallback**

In `cmd/ori/tui_renderer.go`, update `renderToolSegment` immediately after `ctx = normalizeRenderContext(ctx)`:

```go
if ctx.viewMode == transcriptViewNormal {
	return r.renderToolSummarySegment(tool, ctx)
}
```

Add these helper functions below `renderToolSegment`:

```go
func (r transcriptRenderer) renderToolSummarySegment(tool *toolCallSegment, ctx renderContext) string {
	icon, status, iconStyle := toolSegmentStatusParts(tool, ctx)
	name := toolSummaryName(tool)
	header := fmt.Sprintf("  %s %s%s", icon, name, status)
	var b strings.Builder
	b.WriteString(iconStyle.Render(fitLine(header, ctx.width)))
	if tool.status == toolStatusError && tool.result != "" {
		if rendered := renderToolPreviewForWidth("Error", tool.result, toolErrorStyle, ctx.width, 1); rendered != "" {
			b.WriteString("\n")
			b.WriteString(strings.TrimRight(rendered, "\n"))
		}
	}
	return b.String()
}

func toolSummaryName(tool *toolCallSegment) string {
	name := firstNonEmpty(tool.name, "tool")
	if tool.orphan {
		name += " (orphan)"
	}
	if arg := toolSummaryArgument(tool.args); arg != "" {
		name += " " + truncateStr(arg, 24)
	}
	return name
}

func toolSummaryArgument(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	for _, key := range []string{"path", "cmd", "command"} {
		if v, ok := args[key]; ok {
			return formatArgValue(v)
		}
	}
	if len(args) == 1 {
		for _, v := range args {
			return formatArgValue(v)
		}
	}
	return ""
}
```

Keep the existing multiline argument and preview rendering below this branch; it becomes the `detail` rendering path.

- [ ] **Step 4: Run tool tests and verify they pass**

Run:

```bash
go test ./cmd/ori -run 'TestTranscriptRenderer(NormalToolShowsCompactResultPreview|DetailToolKeepsArgumentsAndResultPreview|NormalToolErrorShowsOnePreviewLine|ToolLinesFitNarrowWidth|ToolTinyWidthsFit|AssistantStatusAndOrphanMarker|FinalConflictUsesFinalTextOnce)|TestRenderTool(CallUsesStableMultilineArguments|ResultShowsPreviewAndHiddenLineCount|ArgumentLinesFitNarrowTerminal)|TestHandleRuntimeEvent_ToolUpdateRendersRunningPreview'
```

Expected: PASS.

- [ ] **Step 5: Commit tool display modes**

```bash
git add cmd/ori/tui_renderer.go cmd/ori/tui_renderer_test.go cmd/ori/tui_render_test.go
git commit -m "feat: compact tool output in normal view"
```

---

### Task 3: `/view normal|detail` TUI Command

**Files:**
- Modify: `cmd/ori/tui_model.go`
- Modify: `cmd/ori/tui_view.go`
- Modify: `cmd/ori/tui_command.go`
- Modify: `cmd/ori/tui_render_test.go`
- Modify: `internal/app/commands.go`
- Modify: `internal/app/dispatcher_test.go`

- [ ] **Step 1: Write failing tests for command behavior and metadata**

Add these tests near the slash-command tests in `cmd/ori/tui_render_test.go`:

```go
func TestViewCommandSwitchesTranscriptViewMode(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	asst := m.transcript.appendAssistantBlock(m.nextBlockID("assistant"), time.Unix(1, 0))
	asst.appendReasoningDelta("hidden\nvisible", time.Unix(1, 0))
	m.refreshTranscriptViewport()
	if strings.Contains(plainView(m.transcriptViewportText), "visible") {
		t.Fatalf("normal mode should hide reasoning body before /view detail, got:\n%s", plainView(m.transcriptViewportText))
	}

	cmd := m.applyViewCommand("/view detail")
	if cmd != nil {
		t.Fatalf("/view detail returned unexpected command")
	}
	if m.viewMode != transcriptViewDetail {
		t.Fatalf("viewMode = %q, want %q", m.viewMode, transcriptViewDetail)
	}
	out := plainView(m.transcriptViewportText)
	for _, want := range []string{"visible", "/view detail", "View mode: detail"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected detail viewport to contain %q, got:\n%s", want, out)
		}
	}
}

func TestViewCommandRejectsUnknownMode(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	m.viewMode = transcriptViewDetail

	cmd := m.applyViewCommand("/view verbose")
	if cmd != nil {
		t.Fatalf("/view verbose returned unexpected command")
	}
	if m.viewMode != transcriptViewDetail {
		t.Fatalf("invalid /view should preserve current mode, got %q", m.viewMode)
	}
	out := plainView(m.transcriptViewportText)
	if !strings.Contains(out, "Usage: /view normal|detail") {
		t.Fatalf("expected usage message for invalid /view, got:\n%s", out)
	}
}

func TestAvailableSlashCommandsIncludesView(t *testing.T) {
	m := newTestModel()
	names := map[string]bool{}
	for _, cmd := range m.availableSlashCommands() {
		names[cmd.Name] = true
	}
	if !names["view"] {
		t.Fatalf("expected /view in TUI slash command completions")
	}
}
```

Update `internal/app/dispatcher_test.go` in `TestDispatcherListsDefaultSlashCommands`:

```go
for _, name := range []string{"help", "clear", "new", "status", "stop", "reasoning", "sessions", "view"} {
	if !names[name] {
		t.Fatalf("expected default command %q in command list: %+v", name, commands)
	}
}
```

- [ ] **Step 2: Run command tests and verify they fail**

Run:

```bash
go test ./cmd/ori ./internal/app -run 'TestViewCommand|TestAvailableSlashCommandsIncludesView|TestDispatcherListsDefaultSlashCommands'
```

Expected: FAIL with `undefined: applyViewCommand`, missing `viewMode`, or missing `/view` command metadata.

- [ ] **Step 3: Add model state and render context wiring**

In `cmd/ori/tui_model.go`, add `viewMode` to `interactiveModel` after `focus`:

```go
	viewMode transcriptViewMode
```

Add `viewMode` to `viewCacheKey`:

```go
	viewMode transcriptViewMode
```

In `newInteractiveModel`, initialize the mode:

```go
		viewMode:          transcriptViewNormal,
```

In `cmd/ori/tui_view.go`, update `initTranscriptViewport` after setting `m.renderer`:

```go
	m.viewMode = normalizeTranscriptViewMode(m.viewMode)
```

In `View()`, add mode to the cache key:

```go
		viewMode:        normalizeTranscriptViewMode(m.viewMode),
```

In `renderTranscriptViewportContentForWidth`, pass the mode:

```go
return m.renderer.renderTranscript(m.transcript, renderContext{
	width:    width,
	focus:    m.focus,
	active:   m.active,
	now:      time.Now(),
	viewMode: normalizeTranscriptViewMode(m.viewMode),
})
```

- [ ] **Step 4: Add `/view` command handling**

In `cmd/ori/tui_command.go`, update `handleSlashCommand`:

```go
func (m *interactiveModel) handleSlashCommand(input string) (bool, tea.Cmd) {
	name := slashCommandName(input)
	switch name {
	case "quit", "exit":
		m.quitting = true
		m.shutdown()
		return true, tea.Quit
	case "view":
		return true, m.applyViewCommand(input)
	}
	if m.dispatcher == nil {
		return false, nil
	}
	// existing dispatcher path remains unchanged
```

Add these helpers in `cmd/ori/tui_command.go` near `applySlashCommandResult`:

```go
func (m *interactiveModel) applyViewCommand(input string) tea.Cmd {
	mode, ok := parseViewCommandMode(input)
	if !ok {
		return m.applySlashCommandResult(input, &appcore.CommandResult{
			Text: "Usage: /view normal|detail",
		})
	}
	m.viewMode = mode
	return m.applySlashCommandResult(input, &appcore.CommandResult{
		Text: fmt.Sprintf("View mode: %s", mode),
	})
}

func parseViewCommandMode(input string) (transcriptViewMode, bool) {
	fields := strings.Fields(strings.TrimSpace(input))
	if len(fields) != 2 || strings.TrimPrefix(fields[0], "/") != "view" {
		return transcriptViewNormal, false
	}
	switch strings.ToLower(fields[1]) {
	case "normal":
		return transcriptViewNormal, true
	case "detail":
		return transcriptViewDetail, true
	default:
		return transcriptViewNormal, false
	}
}
```

Update fallback local commands in `availableSlashCommands`:

```go
		{Name: "view", Description: "Set transcript display mode", ArgumentHint: "normal|detail"},
```

In `internal/app/commands.go`, add `/view` metadata in `RegisterDefaultCommands` before `/quit`:

```go
d.RegisterSlashCommand(Command{Name: "view", Description: "Set transcript display mode", ArgumentHint: "normal|detail", Scope: CommandScopeTUI, Handler: handleTUIOnly})
```

- [ ] **Step 5: Run command tests and verify they pass**

Run:

```bash
go test ./cmd/ori ./internal/app -run 'TestViewCommand|TestAvailableSlashCommandsIncludesView|TestDispatcherListsDefaultSlashCommands'
```

Expected: PASS.

- [ ] **Step 6: Commit `/view` command wiring**

```bash
git add cmd/ori/tui_model.go cmd/ori/tui_view.go cmd/ori/tui_command.go cmd/ori/tui_render_test.go internal/app/commands.go internal/app/dispatcher_test.go
git commit -m "feat: add transcript view mode command"
```

---

### Task 4: Documentation Alignment

**Files:**
- Modify: `docs/TUI-GUIDE.md`
- Modify: `docs/superpowers/specs/2026-05-16-tui-readable-transcript-display-design.md` only if implementation deliberately diverges from the approved design.

- [ ] **Step 1: Update activity hierarchy language**

In `docs/TUI-GUIDE.md`, replace the activity hierarchy bullets for reasoning/tool with:

```markdown
3. reasoning segment：按 transcript 顺序展示；默认只显示每段 `thinking · N lines summarized` 标题，详细视图展示该段 reasoning 尾部摘要。
4. tool segment：默认显示工具名、关键参数、状态、耗时、size 和一行结果预览；详细视图展示参数、运行中 preview、最终 result/error。
```

- [ ] **Step 2: Replace Reasoning 摘要 section**

Replace the body under `### Reasoning 摘要` with:

```markdown
Reasoning 默认不全量展示。完整内容保留在 runtime/session 数据里，TUI 只改变
transcript reasoning segment 的投影方式，不修改 transcript 数据：

- `normal` 默认模式保留 reasoning segment 边界和顺序，每段只显示标题：`thinking · N lines summarized`。`N` 是该 segment 内非空 reasoning 行总数。
- `detail` 详细视图同样保留 segment 顺序，并展示每段尾部摘要：live 模式展示最后 5 条非空 reasoning 行，completed/final 模式展示最后 3 条非空 reasoning 行。
- live、completed、final 的 detail 尾部摘要必须保持同一套行数和宽度处理规则。
- `/reasoning on|off` 只控制模型是否产生 reasoning；`/view normal|detail` 才控制 TUI 展示密度。
```

- [ ] **Step 3: Replace 工具调用块 section**

Replace the opening and conventions under `### 工具调用块` with:

```markdown
工具调用在 `normal` 默认模式中使用紧凑摘要，并在完成后保留一行输出预览：

```text
  ✓ list_dir . · 0ms · 114 chars
    │ Result: AGENTS.md
  ● shell go test ./cmd/ori running 1.2s
```

`detail` 详细视图使用结构化多行块：

```text
  ● shell running 1.2s
    │ command    go test ./cmd/ori
    │ timeout    30
    │ Preview
    │ ok   ori/cmd/ori  0.48s
```

约定：

- `normal` 保留状态图标、工具名、短关键参数、耗时和 result/partial size。
- `normal` 成功态最多展示 1 条 result preview；错误态最多展示 1 条 error preview。这样单次用户输入里的 `tool call -> output -> tool call` 顺序在默认视图里也可见。
- `detail` 参数按 key 排序，保证 snapshot 和测试稳定。
- `detail` 单值和多值参数都走 key/value 行，长 key 和长 value 必须截断到终端宽度内。
- `tool_execution_update` 用于运行中 preview；没有 partial update 的工具只显示 start/end。
- `detail` result/error 默认展示最多 4 条非空行，超出时显示 `... N more lines`。
- 工具块只做扫读预览，不替代完整工具结果。
```

- [ ] **Step 4: Update testing checklist**

In the testing checklist, replace the reasoning/tool bullets with:

```markdown
- normal 模式保留多段 reasoning 的顺序和边界，每段只显示 `thinking · N lines summarized` 标题。
- detail 模式 reasoning live/completed/final 都只显示尾部摘要。
- normal 模式工具显示紧凑摘要，成功态和错误态都最多显示 1 条 preview。
- detail 模式工具参数按 key 稳定排序，并渲染为多行块。
- detail 模式长工具 result 显示 preview 和隐藏行数。
- `/view normal|detail` 只改变 TUI 展示密度，不改变 `/reasoning` 模型开关。
```

- [ ] **Step 5: Run documentation self-check**

Run:

```bash
rg -n "WithMouseCellMotion|flushedText|currentRound|printAbove|TODO|TBD" docs/TUI-GUIDE.md docs/superpowers/specs/2026-05-16-tui-readable-transcript-display-design.md
```

Expected: only intentional historical mentions of `printAbove` in architecture warnings and no `TODO` or `TBD`.

- [ ] **Step 6: Commit documentation**

```bash
git add docs/TUI-GUIDE.md docs/superpowers/specs/2026-05-16-tui-readable-transcript-display-design.md
git commit -m "docs: describe readable transcript view modes"
```

---

### Task 5: Final Verification

**Files:**
- Verify all changed files from Tasks 1-4.

- [ ] **Step 1: Format Go files**

Run:

```bash
make fmt
```

Expected: `go fmt ./...` completes successfully.

- [ ] **Step 2: Run targeted TUI tests**

Run:

```bash
go test ./cmd/ori
```

Expected: PASS.

- [ ] **Step 3: Run full project check**

Run:

```bash
make check
```

Expected: `go vet ./...` and `go test -race ./...` pass.

- [ ] **Step 4: Run real TUI smoke**

Run:

```bash
go run ./cmd/ori agent --session cli:tui-view-smoke
```

In the TUI:

```text
/view detail
/view normal
:q
```

Expected:

- TUI starts without alternate screen.
- `/view detail` appends `View mode: detail`.
- `/view normal` appends `View mode: normal`.
- Quit exits cleanly.

- [ ] **Step 5: Check generated binary churn**

Run:

```bash
git status --short --branch
```

Expected: changed source/docs files only. If tracked binaries `ori` or `gateway` changed and the user did not explicitly request build artifacts, restore only those generated binaries with:

```bash
git show HEAD:ori > ori
git show HEAD:gateway > gateway
```

- [ ] **Step 6: Final commit**

```bash
git status --short
git log --oneline -5
```

Expected: commits from Tasks 1-4 are present and no generated binary churn remains.
