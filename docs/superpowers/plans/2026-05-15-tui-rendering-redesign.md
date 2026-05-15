# TUI Rendering Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `ori agent` interactive mode's double-plane rendering with a transcript-first Bubble Tea viewport.

**Architecture:** Keep the redesign inside `cmd/ori`. Runtime and outbound events mutate a TUI-private transcript model through reducer helpers; a pure renderer turns transcript blocks into terminal text; `viewport.Model` owns visible history while overlay panels own management interactions.

**Tech Stack:** Go 1.24.2, Bubble Tea, `github.com/charmbracelet/bubbles/viewport`, Lip Gloss, Glamour, `internal/runtime.Event`, `internal/app.CommandResult`.

---

## File Structure

- Create `cmd/ori/tui_transcript.go`: transcript, block, segment, status enums, final merge helpers, and transcript mutation helpers.
- Create `cmd/ori/tui_transcript_test.go`: unit tests for merge helpers and transcript segment mutation.
- Create `cmd/ori/tui_reducer.go`: reducer methods that translate prompts, runtime events, outbound fallback, command results, cancel, and session replay into transcript mutations.
- Create `cmd/ori/tui_reducer_test.go`: reducer table tests for runtime event order and command/session behavior.
- Create `cmd/ori/tui_renderer.go`: pure transcript renderer and render context.
- Create `cmd/ori/tui_renderer_test.go`: renderer tests for block order, tool widths, command rendering, and reasoning folding.
- Modify `cmd/ori/tui_model.go`: add viewport, transcript, renderer, focus, overlay, and transcript id fields.
- Modify `cmd/ori/tui_update.go`: route runtime/outbound/key changes through reducers and refresh the viewport instead of printing completed content above the TUI.
- Modify `cmd/ori/tui_view.go`: render viewport + status + suggestions + input; keep business block rendering out of `View()`.
- Modify `cmd/ori/tui_command.go`: append command blocks and open overlays instead of printing command results above the TUI.
- Modify `cmd/ori/tui_management.go`: treat management panels as overlay state and convert session resume into transcript blocks.
- Modify `cmd/ori/output.go`: keep non-interactive CLI helpers and move interactive-only rendering behavior to `tui_renderer.go`.
- Modify `cmd/ori/tui_render_test.go`: remove assertions for `flushedText/currentRound/displayedText` and replace them with transcript/viewport assertions.

## Implementation Notes

- Keep all new types unexported because they are TUI-private.
- Do not move rendering code into `internal/app` or `internal/runtime`.
- Run focused tests after every task: `go test ./cmd/ori -run '<TestName>'`.
- Commit after each task. If `make fmt` or `make check` changes top-level binaries, restore those binaries before committing unless the task explicitly asks for build artifacts.

---

### Task 1: Add Transcript Model And Merge Helpers

**Files:**
- Create: `cmd/ori/tui_transcript.go`
- Create: `cmd/ori/tui_transcript_test.go`

- [ ] **Step 1: Write failing merge and segment tests**

Create `cmd/ori/tui_transcript_test.go` with:

```go
package main

import (
	"testing"
	"time"
)

func TestMergeFinalText(t *testing.T) {
	tests := []struct {
		name         string
		streamed     string
		final        string
		wantText     string
		wantConflict bool
	}{
		{name: "final empty keeps streamed", streamed: "hello", final: "", wantText: "hello"},
		{name: "streamed empty uses final", streamed: "", final: "hello", wantText: "hello"},
		{name: "final extends streamed", streamed: "hello", final: "hello world", wantText: "hello world"},
		{name: "streamed extends final", streamed: "hello world", final: "hello", wantText: "hello world"},
		{name: "mismatch prefers final", streamed: "draft", final: "final", wantText: "final", wantConflict: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotConflict := mergeFinalText(tt.streamed, tt.final)
			if gotText != tt.wantText || gotConflict != tt.wantConflict {
				t.Fatalf("mergeFinalText(%q, %q) = (%q, %v), want (%q, %v)",
					tt.streamed, tt.final, gotText, gotConflict, tt.wantText, tt.wantConflict)
			}
		})
	}
}

func TestTranscriptMergesAdjacentTextAndReasoningSegments(t *testing.T) {
	var tr transcript
	tr.appendUserBlock("u1", "hello", time.Unix(1, 0))
	asst := tr.appendAssistantBlock("a1", time.Unix(2, 0))

	asst.appendReasoningDelta("think")
	asst.appendReasoningDelta(" more")
	asst.appendTextDelta("answer")
	asst.appendTextDelta(" now")

	if len(tr.blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(tr.blocks))
	}
	if tr.activeAssistantID != "a1" {
		t.Fatalf("activeAssistantID = %q, want a1", tr.activeAssistantID)
	}
	if got := asst.segments[0].reasoning.text; got != "think more" {
		t.Fatalf("reasoning text = %q", got)
	}
	if got := asst.segments[1].text.text; got != "answer now" {
		t.Fatalf("text segment = %q", got)
	}
}

func TestAssistantUpsertsToolSegments(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	start := time.Unix(3, 0)
	end := start.Add(250 * time.Millisecond)

	tool := asst.upsertToolStart("call-1", "shell", map[string]any{"cmd": "date"}, start)
	tool.partial = "running"
	asst.finishTool("call-1", "shell", "done", false, end)

	if len(asst.segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(asst.segments))
	}
	got := asst.segments[0].tool
	if got == nil || got.name != "shell" || got.status != toolStatusDone || got.result != "done" {
		t.Fatalf("tool segment not finished correctly: %+v", got)
	}
	if got.durationMs != 250 {
		t.Fatalf("durationMs = %d, want 250", got.durationMs)
	}
}

func TestAssistantCreatesOrphanToolWhenEndArrivesFirst(t *testing.T) {
	asst := &assistantBlock{status: assistantStatusThinking}
	asst.finishTool("missing", "web", "late result", false, time.Unix(4, 0))

	if len(asst.segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(asst.segments))
	}
	got := asst.segments[0].tool
	if got == nil || !got.orphan || got.status != toolStatusDone || got.result != "late result" {
		t.Fatalf("orphan tool not rendered into transcript state: %+v", got)
	}
}
```

- [ ] **Step 2: Run the tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestMergeFinalText|TestTranscriptMergesAdjacentTextAndReasoningSegments|TestAssistantUpsertsToolSegments|TestAssistantCreatesOrphanToolWhenEndArrivesFirst'
```

Expected: FAIL with errors such as `undefined: transcript` and `undefined: mergeFinalText`.

- [ ] **Step 3: Add transcript model implementation**

Create `cmd/ori/tui_transcript.go` with:

```go
package main

import (
	"strings"
	"time"
)

type blockKind string

const (
	blockKindUser      blockKind = "user"
	blockKindAssistant blockKind = "assistant"
	blockKindCommand   blockKind = "command"
	blockKindSystem    blockKind = "system"
)

type assistantStatus string

const (
	assistantStatusWaiting      assistantStatus = "waiting"
	assistantStatusThinking     assistantStatus = "thinking"
	assistantStatusResponding   assistantStatus = "responding"
	assistantStatusRunningTools assistantStatus = "running_tools"
	assistantStatusDone         assistantStatus = "done"
	assistantStatusError        assistantStatus = "error"
	assistantStatusCancelled    assistantStatus = "cancelled"
)

type segmentKind string

const (
	segmentKindReasoning segmentKind = "reasoning"
	segmentKindTool      segmentKind = "tool"
	segmentKindText      segmentKind = "text"
)

type toolStatus string

const (
	toolStatusPending toolStatus = "pending"
	toolStatusRunning toolStatus = "running"
	toolStatusDone    toolStatus = "done"
	toolStatusError   toolStatus = "error"
)

type finalSource string

const (
	finalSourceNone     finalSource = ""
	finalSourceRuntime  finalSource = "runtime"
	finalSourceFallback finalSource = "fallback"
)

type systemLevel string

const (
	systemLevelInfo    systemLevel = "info"
	systemLevelWarning systemLevel = "warning"
	systemLevelError   systemLevel = "error"
)

type focusArea string

const (
	focusInput      focusArea = "input"
	focusTranscript focusArea = "transcript"
	focusOverlay    focusArea = "overlay"
)

type transcript struct {
	blocks            []block
	activeAssistantID string
}

type block struct {
	id        string
	kind      blockKind
	createdAt time.Time

	user      *userBlock
	assistant *assistantBlock
	command   *commandBlock
	system    *systemBlock
}

type userBlock struct {
	content string
}

type assistantBlock struct {
	status         assistantStatus
	segments       []assistantSegment
	finalText      string
	finalReasoning string
	finalSource    finalSource
	mergeConflict  bool
	renderCursor   bool
}

type assistantSegment struct {
	kind      segmentKind
	reasoning *reasoningSegment
	tool      *toolCallSegment
	text      *textSegment
}

type reasoningSegment struct {
	text string
}

type textSegment struct {
	text string
}

type toolCallSegment struct {
	id         string
	name       string
	args       map[string]any
	status     toolStatus
	partial    string
	result     string
	startedAt  time.Time
	endedAt    time.Time
	durationMs int64
	expanded   bool
	orphan     bool
}

type commandBlock struct {
	command string
	text    string
	markdown string
	status  string
}

type systemBlock struct {
	level   systemLevel
	message string
}

func (t *transcript) clear() {
	t.blocks = nil
	t.activeAssistantID = ""
}

func (t *transcript) appendUserBlock(id, content string, ts time.Time) *userBlock {
	b := block{
		id:        id,
		kind:      blockKindUser,
		createdAt: ts,
		user:      &userBlock{content: content},
	}
	t.blocks = append(t.blocks, b)
	return b.user
}

func (t *transcript) appendAssistantBlock(id string, ts time.Time) *assistantBlock {
	asst := &assistantBlock{status: assistantStatusWaiting, renderCursor: true}
	t.blocks = append(t.blocks, block{
		id:        id,
		kind:      blockKindAssistant,
		createdAt: ts,
		assistant: asst,
	})
	t.activeAssistantID = id
	return asst
}

func (t *transcript) appendCommandBlock(id, command string, text, markdown, status string, ts time.Time) *commandBlock {
	cmd := &commandBlock{command: command, text: text, markdown: markdown, status: status}
	t.blocks = append(t.blocks, block{
		id:        id,
		kind:      blockKindCommand,
		createdAt: ts,
		command:   cmd,
	})
	return cmd
}

func (t *transcript) appendSystemBlock(id string, level systemLevel, message string, ts time.Time) *systemBlock {
	sys := &systemBlock{level: level, message: message}
	t.blocks = append(t.blocks, block{
		id:        id,
		kind:      blockKindSystem,
		createdAt: ts,
		system:    sys,
	})
	return sys
}

func (t *transcript) activeAssistant() *assistantBlock {
	if t.activeAssistantID == "" {
		return nil
	}
	for i := len(t.blocks) - 1; i >= 0; i-- {
		b := &t.blocks[i]
		if b.id == t.activeAssistantID && b.assistant != nil {
			return b.assistant
		}
	}
	return nil
}

func (a *assistantBlock) appendReasoningDelta(delta string) {
	if delta == "" {
		return
	}
	a.status = assistantStatusThinking
	if len(a.segments) > 0 {
		last := &a.segments[len(a.segments)-1]
		if last.kind == segmentKindReasoning && last.reasoning != nil {
			last.reasoning.text += delta
			return
		}
	}
	a.segments = append(a.segments, assistantSegment{
		kind:      segmentKindReasoning,
		reasoning: &reasoningSegment{text: delta},
	})
}

func (a *assistantBlock) appendTextDelta(delta string) {
	if delta == "" {
		return
	}
	a.status = assistantStatusResponding
	if len(a.segments) > 0 {
		last := &a.segments[len(a.segments)-1]
		if last.kind == segmentKindText && last.text != nil {
			last.text.text += delta
			return
		}
	}
	a.segments = append(a.segments, assistantSegment{
		kind: segmentKindText,
		text: &textSegment{text: delta},
	})
}

func (a *assistantBlock) streamedText() string {
	var parts []string
	for i := range a.segments {
		seg := a.segments[i]
		if seg.kind == segmentKindText && seg.text != nil && seg.text.text != "" {
			parts = append(parts, seg.text.text)
		}
	}
	return strings.Join(parts, "")
}

func (a *assistantBlock) setFinalText(text string) {
	for i := len(a.segments) - 1; i >= 0; i-- {
		seg := &a.segments[i]
		if seg.kind == segmentKindText && seg.text != nil {
			seg.text.text = text
			return
		}
	}
	if text != "" {
		a.segments = append(a.segments, assistantSegment{
			kind: segmentKindText,
			text: &textSegment{text: text},
		})
	}
}

func (a *assistantBlock) upsertToolStart(id, name string, args map[string]any, ts time.Time) *toolCallSegment {
	if found := a.findTool(id, name); found != nil {
		found.name = firstNonEmpty(name, found.name)
		found.args = cloneToolArgs(args)
		found.status = toolStatusRunning
		found.startedAt = ts
		found.orphan = false
		a.status = assistantStatusRunningTools
		return found
	}
	tool := &toolCallSegment{
		id:        id,
		name:      name,
		args:      cloneToolArgs(args),
		status:    toolStatusRunning,
		startedAt: ts,
	}
	a.segments = append(a.segments, assistantSegment{kind: segmentKindTool, tool: tool})
	a.status = assistantStatusRunningTools
	return tool
}

func (a *assistantBlock) updateTool(id, name, partial string, ts time.Time) *toolCallSegment {
	tool := a.findTool(id, name)
	if tool == nil {
		tool = a.upsertToolStart(id, name, nil, ts)
		tool.orphan = true
	}
	tool.partial = partial
	return tool
}

func (a *assistantBlock) finishTool(id, name, result string, isError bool, ts time.Time) *toolCallSegment {
	tool := a.findTool(id, name)
	if tool == nil {
		tool = &toolCallSegment{id: id, name: name, orphan: true, startedAt: ts}
		a.segments = append(a.segments, assistantSegment{kind: segmentKindTool, tool: tool})
	}
	tool.result = result
	tool.endedAt = ts
	if isError {
		tool.status = toolStatusError
	} else {
		tool.status = toolStatusDone
	}
	if !tool.startedAt.IsZero() && !tool.endedAt.IsZero() {
		tool.durationMs = tool.endedAt.Sub(tool.startedAt).Milliseconds()
	}
	if !a.hasRunningTool() {
		a.status = assistantStatusThinking
	}
	return tool
}

func (a *assistantBlock) findTool(id, name string) *toolCallSegment {
	for i := range a.segments {
		tool := a.segments[i].tool
		if tool == nil {
			continue
		}
		if id != "" && tool.id == id {
			return tool
		}
		if id == "" && name != "" && tool.name == name {
			return tool
		}
	}
	return nil
}

func (a *assistantBlock) hasRunningTool() bool {
	for i := range a.segments {
		if tool := a.segments[i].tool; tool != nil && tool.status == toolStatusRunning {
			return true
		}
	}
	return false
}

func mergeFinalText(streamed, final string) (string, bool) {
	switch {
	case final == "":
		return streamed, false
	case streamed == "":
		return final, false
	case strings.HasPrefix(final, streamed):
		return final, false
	case strings.HasPrefix(streamed, final):
		return streamed, false
	default:
		return final, true
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
```

- [ ] **Step 4: Run the transcript tests and verify they pass**

Run:

```sh
go test ./cmd/ori -run 'TestMergeFinalText|TestTranscriptMergesAdjacentTextAndReasoningSegments|TestAssistantUpsertsToolSegments|TestAssistantCreatesOrphanToolWhenEndArrivesFirst'
```

Expected: PASS.

- [ ] **Step 5: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestMergeFinalText|TestTranscriptMergesAdjacentTextAndReasoningSegments|TestAssistantUpsertsToolSegments|TestAssistantCreatesOrphanToolWhenEndArrivesFirst'
git status --short
```

Expected: only `cmd/ori/tui_transcript.go` and `cmd/ori/tui_transcript_test.go` are changed.

Commit:

```sh
git add cmd/ori/tui_transcript.go cmd/ori/tui_transcript_test.go
git commit -m "feat: add TUI transcript model"
```

---

### Task 2: Add Runtime Reducer For Assistant Events

**Files:**
- Create: `cmd/ori/tui_reducer.go`
- Create: `cmd/ori/tui_reducer_test.go`
- Modify: `cmd/ori/tui_model.go`
- Modify: `cmd/ori/tui_update.go`

- [ ] **Step 1: Write failing reducer tests**

Create `cmd/ori/tui_reducer_test.go` with:

```go
package main

import (
	"testing"
	"time"

	"ori/internal/llm"
	"ori/internal/runtime"
)

func TestReducerBuildsAssistantSegmentsFromRuntimeEvents(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))

	m.reduceRuntimeEvent(runtime.Event{Kind: runtime.EventAgentStart, Timestamp: time.Unix(2, 0)})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(3, 0),
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventThinkingDelta,
			Delta: "thinking",
		}},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventMessageUpdate,
		Timestamp: time.Unix(4, 0),
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventTextDelta,
			Delta: "answer",
		}},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionStart,
		Timestamp: time.Unix(5, 0),
		Data: runtime.ToolStartData{
			ToolCallID: "tool-1",
			ToolName:   "shell",
			Args:       map[string]any{"cmd": "date"},
		},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind:      runtime.EventToolExecutionEnd,
		Timestamp: time.Unix(6, 0),
		Data: runtime.ToolEndData{
			ToolCallID: "tool-1",
			ToolName:   "shell",
			Result:     []llm.Content{llm.TextContent{Text: "ok"}},
		},
	})

	asst := m.transcript.activeAssistant()
	if asst == nil {
		t.Fatal("expected active assistant")
	}
	if asst.status != assistantStatusThinking {
		t.Fatalf("status = %q, want %q", asst.status, assistantStatusThinking)
	}
	if len(asst.segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(asst.segments))
	}
	if got := asst.segments[0].reasoning.text; got != "thinking" {
		t.Fatalf("reasoning = %q", got)
	}
	if got := asst.segments[1].text.text; got != "answer" {
		t.Fatalf("text = %q", got)
	}
	if got := asst.segments[2].tool.result; got != "ok" {
		t.Fatalf("tool result = %q", got)
	}
}

func TestReducerFinalizesFromAgentEndAndIgnoresLaterFallback(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind: runtime.EventMessageUpdate,
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventTextDelta,
			Delta: "answer",
		}},
	})
	m.reduceRuntimeEvent(runtime.Event{
		Kind: runtime.EventAgentEnd,
		Data: runtime.AgentEndData{Messages: []runtime.AgentMessage{
			runtime.WrapLLM(llm.AssistantMessage{
				Content: []llm.Content{llm.TextContent{Text: "answer final"}},
			}),
		}},
	})

	m.finalizeTranscriptFromOutbound("fallback duplicate", "", true)

	asst := m.transcript.activeAssistant()
	if asst == nil {
		t.Fatal("expected active assistant")
	}
	if asst.status != assistantStatusDone {
		t.Fatalf("status = %q, want done", asst.status)
	}
	if got := asst.segments[0].text.text; got != "answer final" {
		t.Fatalf("final text = %q", got)
	}
	if asst.finalSource != finalSourceRuntime {
		t.Fatalf("finalSource = %q, want runtime", asst.finalSource)
	}
}

func TestReducerUsesOutboundFallbackWhenRuntimeFinalIsMissing(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind: runtime.EventMessageUpdate,
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventTextDelta,
			Delta: "partial",
		}},
	})

	m.finalizeTranscriptFromOutbound("partial final", "because", true)

	asst := m.transcript.activeAssistant()
	if asst == nil {
		t.Fatal("expected active assistant")
	}
	if asst.finalSource != finalSourceFallback {
		t.Fatalf("finalSource = %q, want fallback", asst.finalSource)
	}
	if asst.finalReasoning != "because" {
		t.Fatalf("finalReasoning = %q", asst.finalReasoning)
	}
	if got := asst.segments[0].text.text; got != "partial final" {
		t.Fatalf("text = %q", got)
	}
}

func TestReducerCancelsActiveAssistantWithoutDroppingContent(t *testing.T) {
	m := &interactiveModel{}
	m.beginTranscriptPrompt("hello", time.Unix(1, 0))
	m.reduceRuntimeEvent(runtime.Event{
		Kind: runtime.EventMessageUpdate,
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventTextDelta,
			Delta: "partial",
		}},
	})

	m.cancelActiveAssistant()

	asst := m.transcript.activeAssistant()
	if asst == nil {
		t.Fatal("expected active assistant")
	}
	if asst.status != assistantStatusCancelled {
		t.Fatalf("status = %q, want cancelled", asst.status)
	}
	if got := asst.segments[0].text.text; got != "partial" {
		t.Fatalf("partial text was dropped: %q", got)
	}
}
```

- [ ] **Step 2: Run reducer tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestReducerBuildsAssistantSegmentsFromRuntimeEvents|TestReducerFinalizesFromAgentEndAndIgnoresLaterFallback|TestReducerUsesOutboundFallbackWhenRuntimeFinalIsMissing|TestReducerCancelsActiveAssistantWithoutDroppingContent'
```

Expected: FAIL with errors such as `m.beginTranscriptPrompt undefined` and `m.reduceRuntimeEvent undefined`.

- [ ] **Step 3: Add transcript fields to the model**

Modify `cmd/ori/tui_model.go` inside `interactiveModel`:

```go
	transcript       transcript
	nextTranscriptID int
```

Add this method in `cmd/ori/tui_reducer.go`:

```go
package main

import (
	"fmt"
	"time"

	appcore "ori/internal/app"
	"ori/internal/llm"
	"ori/internal/runtime"
)

func (m *interactiveModel) nextBlockID(prefix string) string {
	m.nextTranscriptID++
	return fmt.Sprintf("%s-%d", prefix, m.nextTranscriptID)
}
```

- [ ] **Step 4: Implement prompt and runtime reducers**

Continue `cmd/ori/tui_reducer.go` with:

```go
func (m *interactiveModel) beginTranscriptPrompt(content string, ts time.Time) {
	m.transcript.appendUserBlock(m.nextBlockID("user"), content, ts)
	m.transcript.appendAssistantBlock(m.nextBlockID("assistant"), ts)
	m.active = true
	m.waiting = true
	m.responseReceived = false
	m.status = "waiting"
}

func (m *interactiveModel) ensureTranscriptAssistant(ts time.Time) *assistantBlock {
	if asst := m.transcript.activeAssistant(); asst != nil {
		return asst
	}
	return m.transcript.appendAssistantBlock(m.nextBlockID("assistant"), ts)
}

func (m *interactiveModel) reduceRuntimeEvent(ev runtime.Event) bool {
	asst := m.ensureTranscriptAssistant(ev.Timestamp)
	switch ev.Kind {
	case runtime.EventAgentStart:
		asst.status = assistantStatusThinking
		m.status = "thinking"
		return true
	case runtime.EventTurnStart:
		m.status = "thinking"
		return true
	case runtime.EventMessageUpdate:
		data, ok := ev.MessageUpdate()
		if !ok {
			return false
		}
		switch data.StreamEvent.Kind {
		case llm.StreamEventThinkingDelta:
			asst.appendReasoningDelta(data.StreamEvent.Delta)
			m.status = "thinking"
			return data.StreamEvent.Delta != ""
		case llm.StreamEventTextDelta:
			asst.appendTextDelta(data.StreamEvent.Delta)
			m.status = "responding"
			return data.StreamEvent.Delta != ""
		}
	case runtime.EventToolExecutionStart:
		data, ok := ev.ToolStart()
		if !ok {
			return false
		}
		asst.upsertToolStart(data.ToolCallID, data.ToolName, data.Args, ev.Timestamp)
		m.status = "running tools"
		return true
	case runtime.EventToolExecUpdate:
		data, ok := ev.ToolUpdate()
		if !ok {
			return false
		}
		asst.updateTool(data.ToolCallID, data.ToolName, contentsToString(data.Partial), ev.Timestamp)
		m.status = "running tools"
		return true
	case runtime.EventToolExecutionEnd:
		data, ok := ev.ToolEnd()
		if !ok {
			return false
		}
		asst.finishTool(data.ToolCallID, data.ToolName, contentsToString(data.Result), data.IsError, ev.Timestamp)
		if asst.hasRunningTool() {
			m.status = "running tools"
		} else {
			m.status = "thinking"
		}
		return true
	case runtime.EventAgentEnd:
		data, _ := ev.AgentEnd()
		text, reasoning := appcore.ExtractFinalAssistant(data.Messages)
		m.finalizeTranscriptAssistant(text, reasoning, finalSourceRuntime)
		return true
	}
	return false
}

func (m *interactiveModel) finalizeTranscriptAssistant(content, reasoning string, source finalSource) {
	asst := m.ensureTranscriptAssistant(time.Now())
	if asst.status == assistantStatusDone && asst.finalSource == finalSourceRuntime {
		return
	}
	merged, conflict := mergeFinalText(asst.streamedText(), content)
	asst.setFinalText(merged)
	asst.finalText = merged
	asst.finalReasoning = reasoning
	asst.finalSource = source
	asst.mergeConflict = conflict
	asst.status = assistantStatusDone
	asst.renderCursor = false
	m.waiting = false
	m.active = false
	m.responseReceived = true
	m.status = "done"
}

func (m *interactiveModel) finalizeTranscriptFromOutbound(content, reasoning string, fromAgentEventFinal bool) {
	asst := m.transcript.activeAssistant()
	if asst != nil && asst.status == assistantStatusDone && asst.finalSource == finalSourceRuntime {
		return
	}
	source := finalSourceFallback
	if fromAgentEventFinal {
		source = finalSourceFallback
	}
	m.finalizeTranscriptAssistant(content, reasoning, source)
}

func (m *interactiveModel) cancelActiveAssistant() {
	if asst := m.transcript.activeAssistant(); asst != nil {
		asst.status = assistantStatusCancelled
		asst.renderCursor = false
	}
	m.active = false
	m.waiting = false
	m.status = "cancelled"
}
```

- [ ] **Step 5: Keep existing runtime handler compiling while reducer tests pass**

Do not remove the old `handleRuntimeEvent` body yet. At the top of `handleRuntimeEvent` in `cmd/ori/tui_update.go`, add no call to `reduceRuntimeEvent` in this task. The reducer is tested directly first so behavior is isolated before the UI shell changes.

- [ ] **Step 6: Run reducer tests and verify they pass**

Run:

```sh
go test ./cmd/ori -run 'TestReducerBuildsAssistantSegmentsFromRuntimeEvents|TestReducerFinalizesFromAgentEndAndIgnoresLaterFallback|TestReducerUsesOutboundFallbackWhenRuntimeFinalIsMissing|TestReducerCancelsActiveAssistantWithoutDroppingContent'
```

Expected: PASS.

- [ ] **Step 7: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestMergeFinalText|TestReducer'
git status --short
```

Expected: the new reducer files and `cmd/ori/tui_model.go` are changed.

Commit:

```sh
git add cmd/ori/tui_model.go cmd/ori/tui_reducer.go cmd/ori/tui_reducer_test.go
git commit -m "feat: add TUI transcript reducer"
```

---

### Task 3: Add Pure Transcript Renderer

**Files:**
- Create: `cmd/ori/tui_renderer.go`
- Create: `cmd/ori/tui_renderer_test.go`
- Modify: `cmd/ori/output.go`

- [ ] **Step 1: Write failing renderer tests**

Create `cmd/ori/tui_renderer_test.go` with:

```go
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestTranscriptRendererOrdersBlocks(t *testing.T) {
	var tr transcript
	tr.appendUserBlock("u1", "hello", time.Unix(1, 0))
	asst := tr.appendAssistantBlock("a1", time.Unix(2, 0))
	asst.appendReasoningDelta("first\nsecond\nthird\nfourth")
	asst.appendTextDelta("answer")
	tr.appendCommandBlock("c1", "/status", "ready", "", "ready", time.Unix(3, 0))
	tr.appendSystemBlock("s1", systemLevelInfo, "session switched", time.Unix(4, 0))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80, now: time.Unix(5, 0)}))
	for _, want := range []string{"you", "hello", "ori", "thinking", "answer", "/status", "ready", "session switched"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered transcript missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "hello") > strings.Index(out, "answer") {
		t.Fatalf("user block rendered after assistant block:\n%s", out)
	}
}

func TestTranscriptRendererKeepsCommandTextPlain(t *testing.T) {
	var tr transcript
	tr.appendCommandBlock("c1", "/skills", "**not markdown**", "", "", time.Unix(1, 0))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80}))
	if !strings.Contains(out, "**not markdown**") {
		t.Fatalf("plain command text was not preserved:\n%s", out)
	}
}

func TestTranscriptRendererUsesMarkdownFieldForMarkdownCommands(t *testing.T) {
	var tr transcript
	tr.appendCommandBlock("c1", "/help", "", "**bold**", "", time.Unix(1, 0))

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80}))
	if !strings.Contains(out, "bold") {
		t.Fatalf("markdown command output missing rendered content:\n%s", out)
	}
}

func TestTranscriptRendererToolLinesFitNarrowWidth(t *testing.T) {
	var tr transcript
	asst := tr.appendAssistantBlock("a1", time.Unix(1, 0))
	tool := asst.upsertToolStart("tool-1", "shell", map[string]any{"cmd": strings.Repeat("x", 100)}, time.Unix(1, 0))
	tool.result = strings.Repeat("y", 100)
	tool.status = toolStatusDone

	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 40, now: time.Unix(2, 0)}))
	for _, line := range strings.Split(out, "\n") {
		if lipgloss.Width(line) > 40 {
			t.Fatalf("line width %d exceeds 40: %q\n%s", lipgloss.Width(line), line, out)
		}
	}
}
```

- [ ] **Step 2: Run renderer tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestTranscriptRenderer'
```

Expected: FAIL with `undefined: transcriptRenderer` and `undefined: renderContext`.

- [ ] **Step 3: Implement renderer context and block rendering**

Create `cmd/ori/tui_renderer.go` with:

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

type renderContext struct {
	width  int
	focus  focusArea
	active bool
	now    time.Time
}

type transcriptRenderer struct{}

func (r transcriptRenderer) renderTranscript(tr transcript, ctx renderContext) string {
	if ctx.width <= 0 {
		ctx.width = getTerminalWidth()
	}
	var b strings.Builder
	for i := range tr.blocks {
		rendered := r.renderBlock(tr.blocks[i], ctx)
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(rendered)
		if !strings.HasSuffix(rendered, "\n") {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r transcriptRenderer) renderBlock(b block, ctx renderContext) string {
	switch b.kind {
	case blockKindUser:
		if b.user == nil {
			return ""
		}
		return r.renderUserBlock(*b.user)
	case blockKindAssistant:
		if b.assistant == nil {
			return ""
		}
		return r.renderAssistantBlock(*b.assistant, ctx)
	case blockKindCommand:
		if b.command == nil {
			return ""
		}
		return r.renderCommandBlock(*b.command)
	case blockKindSystem:
		if b.system == nil {
			return ""
		}
		return r.renderSystemBlock(*b.system)
	default:
		return ""
	}
}

func (r transcriptRenderer) renderUserBlock(user userBlock) string {
	if strings.TrimSpace(user.content) == "" {
		return ""
	}
	return userPromptStyle.Render("you") + "\n" + contentStyle.Render(user.content)
}

func (r transcriptRenderer) renderAssistantBlock(asst assistantBlock, ctx renderContext) string {
	var b strings.Builder
	b.WriteString(spinnerStyle.Render("✦"))
	b.WriteString(" ")
	b.WriteString(assistantLabelStyle.Render("ori"))
	if asst.status != "" && asst.status != assistantStatusDone {
		b.WriteString(toolDurationStyle.Render(" " + string(asst.status)))
	}
	if asst.mergeConflict {
		b.WriteString(toolErrorStyle.Render(" merge-conflict"))
	}
	b.WriteString("\n")
	for i := range asst.segments {
		rendered := r.renderSegment(asst.segments[i], ctx)
		if rendered == "" {
			continue
		}
		b.WriteString(rendered)
		if !strings.HasSuffix(rendered, "\n") {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r transcriptRenderer) renderSegment(seg assistantSegment, ctx renderContext) string {
	switch seg.kind {
	case segmentKindReasoning:
		if seg.reasoning == nil {
			return ""
		}
		return renderReasoningBlock(seg.reasoning.text, reasoningModeLive)
	case segmentKindText:
		if seg.text == nil {
			return ""
		}
		return renderMarkdown(seg.text.text)
	case segmentKindTool:
		if seg.tool == nil {
			return ""
		}
		return r.renderToolSegment(*seg.tool, ctx)
	default:
		return ""
	}
}

func (r transcriptRenderer) renderToolSegment(tool toolCallSegment, ctx renderContext) string {
	entry := toolCallEntry{
		id:         tool.id,
		name:       tool.name,
		argsMap:    cloneToolArgs(tool.args),
		status:     string(tool.status),
		partial:    tool.partial,
		result:     tool.result,
		durationMs: tool.durationMs,
		startTime:  tool.startedAt,
	}
	out := renderToolCallBlock(entry, tool.status == toolStatusRunning)
	if tool.orphan {
		return strings.Replace(out, tool.name, tool.name+" "+toolArgsStyle.Render("(orphan)"), 1)
	}
	return out
}

func (r transcriptRenderer) renderCommandBlock(cmd commandBlock) string {
	var b strings.Builder
	b.WriteString(userPromptStyle.Render(cmd.command))
	if cmd.status != "" {
		b.WriteString(toolDurationStyle.Render(" " + cmd.status))
	}
	if cmd.text != "" {
		b.WriteString("\n")
		b.WriteString(cmd.text)
	}
	if cmd.markdown != "" {
		b.WriteString("\n")
		b.WriteString(renderMarkdown(cmd.markdown))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (r transcriptRenderer) renderSystemBlock(sys systemBlock) string {
	if sys.message == "" {
		return ""
	}
	label := "info"
	style := toolArgsStyle
	switch sys.level {
	case systemLevelWarning:
		label = "warning"
		style = waitingStyle
	case systemLevelError:
		label = "error"
		style = toolErrorStyle
	}
	return style.Render(fmt.Sprintf("%s: %s", label, sys.message))
}
```

- [ ] **Step 4: Run renderer tests and verify they pass**

Run:

```sh
go test ./cmd/ori -run 'TestTranscriptRenderer'
```

Expected: PASS.

- [ ] **Step 5: Run existing width tests**

Run:

```sh
go test ./cmd/ori -run 'TestRenderRoundToolDetailLinesFitTerminalWidth|TestRenderToolArgumentLinesFitNarrowTerminal|TestRenderToolResultShowsPreviewAndHiddenLineCount'
```

Expected: PASS. If a line exceeds width, update `renderToolSegment` to pass through the existing `renderToolCallBlock` helpers rather than duplicating tool formatting.

- [ ] **Step 6: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestTranscriptRenderer|TestRenderTool'
git status --short
```

Expected: renderer files are changed; no generated binaries are staged.

Commit:

```sh
git add cmd/ori/tui_renderer.go cmd/ori/tui_renderer_test.go
git commit -m "feat: render TUI transcript blocks"
```

---

### Task 4: Add Viewport Shell Without Removing Old Runtime Path

**Files:**
- Modify: `cmd/ori/tui_model.go`
- Modify: `cmd/ori/tui_view.go`
- Modify: `cmd/ori/tui_update.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Write failing viewport tests**

Append to `cmd/ori/tui_render_test.go`:

```go
func TestRefreshTranscriptViewportFollowsTailAtBottom(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 5)
	for i := 0; i < 12; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}

	m.refreshTranscriptViewport()

	if !m.viewport.AtBottom() {
		t.Fatalf("expected viewport to follow tail")
	}
	if !strings.Contains(plainView(m.viewport.View()), "line 11") {
		t.Fatalf("viewport did not include latest line:\n%s", plainView(m.viewport.View()))
	}
}

func TestRefreshTranscriptViewportPreservesScrollWhenAwayFromBottom(t *testing.T) {
	m := &interactiveModel{}
	m.initTranscriptViewport(40, 5)
	for i := 0; i < 12; i++ {
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, fmt.Sprintf("line %02d", i), time.Unix(int64(i), 0))
	}
	m.refreshTranscriptViewport()
	m.viewport.GotoTop()
	before := m.viewport.YOffset

	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "new output", time.Unix(20, 0))
	m.refreshTranscriptViewport()

	if m.viewport.YOffset != before {
		t.Fatalf("viewport YOffset changed from %d to %d", before, m.viewport.YOffset)
	}
	if !m.hasNewTranscriptOutput {
		t.Fatalf("expected new output indicator when user is away from bottom")
	}
}
```

Add imports to `cmd/ori/tui_render_test.go` if missing:

```go
import (
	"fmt"
	"strings"
	"time"
)
```

- [ ] **Step 2: Run viewport tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestRefreshTranscriptViewport'
```

Expected: FAIL with `m.initTranscriptViewport undefined` and `m.viewport undefined`.

- [ ] **Step 3: Add viewport fields and initialization**

Modify imports in `cmd/ori/tui_model.go`:

```go
import (
	"sync"
	"time"

	appcore "ori/internal/app"
	"ori/internal/bus"
	"ori/internal/runtime"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)
```

Add fields to `interactiveModel`:

```go
	viewport               viewport.Model
	renderer               transcriptRenderer
	focus                  focusArea
	hasNewTranscriptOutput bool
```

In `newInteractiveModel`, after constructing the text input:

```go
	vp := viewport.New(getTerminalWidth(), transcriptViewportHeight())
```

In the model literal:

```go
		viewport:      vp,
		renderer:      transcriptRenderer{},
		focus:         focusInput,
```

Add helpers in `cmd/ori/tui_view.go`:

```go
func transcriptViewportHeight() int {
	h := getTerminalHeight() - 4
	if h < 5 {
		return 5
	}
	return h
}

func (m *interactiveModel) initTranscriptViewport(width, height int) {
	if width <= 0 {
		width = getTerminalWidth()
	}
	if height <= 0 {
		height = transcriptViewportHeight()
	}
	m.viewport = viewport.New(width, height)
	m.renderer = transcriptRenderer{}
	m.focus = focusInput
}
```

- [ ] **Step 4: Implement viewport refresh**

Add to `cmd/ori/tui_view.go`:

```go
func (m *interactiveModel) refreshTranscriptViewport() {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		m.initTranscriptViewport(getTerminalWidth(), transcriptViewportHeight())
	}
	wasAtBottom := m.viewport.AtBottom()
	content := m.renderer.renderTranscript(m.transcript, renderContext{
		width:  m.viewport.Width,
		focus:  m.focus,
		active: m.active,
		now:    time.Now(),
	})
	m.viewport.SetContent(content)
	if wasAtBottom || m.viewport.YOffset == 0 && strings.TrimSpace(content) == "" {
		m.viewport.GotoBottom()
		m.hasNewTranscriptOutput = false
		return
	}
	m.hasNewTranscriptOutput = true
}
```

Update imports in `cmd/ori/tui_view.go` to include `time`.

- [ ] **Step 5: Update View to render viewport content**

Replace the business-content section of `View()` in `cmd/ori/tui_view.go` with:

```go
	width := getTerminalWidth()
	if m.viewport.Width != width {
		m.viewport.Width = width
	}
	m.viewport.Height = transcriptViewportHeight()
	m.refreshTranscriptViewport()

	if view := m.viewport.View(); strings.TrimSpace(view) != "" {
		s.WriteString(view)
		s.WriteString("\n")
	}
	if m.panel != nil {
		s.WriteString(m.renderManagementPanel())
	}
```

Leave the status line, slash command suggestions, and input rendering below it in place for this task.

- [ ] **Step 6: Route viewport update messages when focus is transcript**

In `cmd/ori/tui_update.go`, inside the `tea.KeyMsg` case before slash-command handling:

```go
		if m.focus == focusTranscript {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.viewVersion++
			return m, cmd
		}
```

Add PageUp/PageDown focus behavior before the input update fallback:

```go
		switch msg.Type {
		case tea.KeyPgUp, tea.KeyPgDown:
			m.focus = focusTranscript
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			m.viewVersion++
			return m, cmd
		case tea.KeyEsc:
			if m.focus == focusTranscript {
				m.focus = focusInput
				m.viewVersion++
				return m, nil
			}
		}
```

- [ ] **Step 7: Run viewport tests and existing view cache tests**

Run:

```sh
go test ./cmd/ori -run 'TestRefreshTranscriptViewport|TestViewCacheInvalidatesWhenDisplayedTextChanges|TestView_RendersLiveToolCall'
```

Expected: viewport tests PASS. Existing tests may still pass because old fields are not removed; if `TestView_RendersLiveToolCall` fails because viewport is now primary, update that test in Task 5 after runtime path migration.

- [ ] **Step 8: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestRefreshTranscriptViewport'
git status --short
```

Commit:

```sh
git add cmd/ori/tui_model.go cmd/ori/tui_view.go cmd/ori/tui_update.go cmd/ori/tui_render_test.go
git commit -m "feat: add TUI transcript viewport"
```

---

### Task 5: Migrate Prompt, Runtime, And Final Paths To Transcript Viewport

**Files:**
- Modify: `cmd/ori/tui_command.go`
- Modify: `cmd/ori/tui_update.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Write failing migration tests**

Append to `cmd/ori/tui_render_test.go`:

```go
func TestSubmitPromptAppendsTranscriptBlocksWithoutPrintAbove(t *testing.T) {
	calledPrintAbove := false
	m := &interactiveModel{
		printAboveFn: func(string) { calledPrintAbove = true },
		renderer:     transcriptRenderer{},
		focus:        focusInput,
	}
	m.initTranscriptViewport(80, 10)

	m.beginPromptForTranscript("hello")

	if calledPrintAbove {
		t.Fatalf("prompt path printed above the TUI")
	}
	if len(m.transcript.blocks) != 2 {
		t.Fatalf("blocks = %d, want user + assistant", len(m.transcript.blocks))
	}
	if m.transcript.blocks[0].kind != blockKindUser || m.transcript.blocks[1].kind != blockKindAssistant {
		t.Fatalf("unexpected blocks: %+v", m.transcript.blocks)
	}
}

func TestHandleRuntimeEventUsesTranscriptInsteadOfCurrentRound(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	m.beginPromptForTranscript("hello")

	cmd := m.handleRuntimeEvent(runtime.Event{
		Kind: runtime.EventMessageUpdate,
		Data: runtime.MessageUpdateData{StreamEvent: llm.StreamEvent{
			Kind:  llm.StreamEventTextDelta,
			Delta: "answer",
		}},
	})

	if cmd != nil {
		t.Fatalf("runtime text delta returned print command")
	}
	if m.currentRound != nil || m.displayedText != "" || len(m.typewriterQueue) != 0 || m.flushedText != "" {
		t.Fatalf("old live state was mutated: currentRound=%+v displayed=%q queue=%d flushed=%q",
			m.currentRound, m.displayedText, len(m.typewriterQueue), m.flushedText)
	}
	asst := m.transcript.activeAssistant()
	if asst == nil || len(asst.segments) != 1 || asst.segments[0].text.text != "answer" {
		t.Fatalf("text delta not captured in transcript: %+v", asst)
	}
}

func TestResponseMsgFinalizesTranscriptWithoutPrintAbove(t *testing.T) {
	calledPrintAbove := false
	m := &interactiveModel{
		printAboveFn: func(string) { calledPrintAbove = true },
		renderer:     transcriptRenderer{},
		focus:        focusInput,
	}
	m.initTranscriptViewport(80, 10)
	m.beginPromptForTranscript("hello")

	_, _ = m.Update(responseMsg{content: "final", reasoning: "why", agentEventFinal: true, fallback: true})

	if calledPrintAbove {
		t.Fatalf("response finalization printed above the TUI")
	}
	asst := m.transcript.activeAssistant()
	if asst == nil || asst.status != assistantStatusDone || asst.finalSource != finalSourceFallback {
		t.Fatalf("assistant not finalized from outbound fallback: %+v", asst)
	}
}
```

Add imports if the file does not already include them:

```go
import (
	"ori/internal/llm"
	"ori/internal/runtime"
)
```

- [ ] **Step 2: Run migration tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestSubmitPromptAppendsTranscriptBlocksWithoutPrintAbove|TestHandleRuntimeEventUsesTranscriptInsteadOfCurrentRound|TestResponseMsgFinalizesTranscriptWithoutPrintAbove'
```

Expected: FAIL with `m.beginPromptForTranscript undefined` or old state mutation assertions.

- [ ] **Step 3: Add prompt helper and migrate submitPrompt**

In `cmd/ori/tui_command.go`, add:

```go
func (m *interactiveModel) beginPromptForTranscript(displayContent string) {
	m.beginTranscriptPrompt(displayContent, time.Now())
	m.spinnerIdx = 0
	m.currentRound = nil
	m.streamText = ""
	m.displayedText = ""
	m.typewriterQueue = nil
	m.flushedText = ""
	m.refreshTranscriptViewport()
	m.viewVersion++
}
```

Add `time` to imports.

Replace the state setup and `printAbove` part of `submitPrompt` with:

```go
	m.mu.Lock()
	m.beginPromptForTranscript(displayContent)
	m.mu.Unlock()

	m.dispatcher.Bus().PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     m.chatID,
		Content:    dispatchContent,
		SessionKey: m.sessionKey,
	})

	return m.tickSpinner()
```

Keep the dispatcher publish unchanged. Remove the `padded` and `printCmd` logic from `submitPrompt`.

- [ ] **Step 4: Migrate responseMsg handling**

In `cmd/ori/tui_update.go`, replace the `responseMsg` case body with:

```go
		m.mu.Lock()
		if msg.agentEventFinal && !msg.fallback && m.active && !m.responseReceived {
			m.mu.Unlock()
			return m, m.deferResponse(msg)
		}
		m.finalizeTranscriptFromOutbound(msg.content, msg.reasoning, msg.agentEventFinal)
		m.refreshTranscriptViewport()
		m.viewVersion++
		m.mu.Unlock()
		return m, nil
```

- [ ] **Step 5: Migrate runtimeEventMsg handling**

In `cmd/ori/tui_update.go`, replace the `runtimeEventMsg` case body with:

```go
		m.mu.Lock()
		mutated := m.reduceRuntimeEvent(msg.ev)
		if mutated {
			m.refreshTranscriptViewport()
			m.viewVersion++
		}
		m.mu.Unlock()
		return m, nil
```

Replace the old `handleRuntimeEvent` body with:

```go
func (m *interactiveModel) handleRuntimeEvent(ev runtime.Event) tea.Cmd {
	if !m.active && ev.Kind != runtime.EventAgentEnd {
		return nil
	}
	m.reduceRuntimeEvent(ev)
	m.refreshTranscriptViewport()
	return nil
}
```

This keeps tests that call `handleRuntimeEvent` working while normal `Update` uses the reducer directly.

- [ ] **Step 6: Migrate Ctrl-C active cancellation**

In `cmd/ori/tui_update.go`, replace the active Ctrl-C branch:

```go
			if m.active {
				m.cancelActiveAssistant()
				m.quitting = true
				m.shutdown()
				m.refreshTranscriptViewport()
				m.viewVersion++
				return m, tea.Quit
			}
```

Do not call `formatCurrentState()` or `printAbove` in this branch.

- [ ] **Step 7: Run migration tests**

Run:

```sh
go test ./cmd/ori -run 'TestSubmitPromptAppendsTranscriptBlocksWithoutPrintAbove|TestHandleRuntimeEventUsesTranscriptInsteadOfCurrentRound|TestResponseMsgFinalizesTranscriptWithoutPrintAbove'
```

Expected: PASS.

- [ ] **Step 8: Run targeted legacy regression tests and update expectations**

Run:

```sh
go test ./cmd/ori -run 'TestAgentEnd_PrintsFinalOutputWithToolCallsFromSameTurn|TestAgentEnd_PrintsFinalOutputWithToolCallsFromPreviousTurn|TestHandleRuntimeEvent_ToolEndFallsBackToThinking|TestHandleRuntimeEvent_ToolUpdateRendersRunningPreview'
```

Expected before updates: tests whose names say `PrintsFinalOutput` fail because final content no longer prints above. Update those tests to assert:

```go
asst := m.transcript.activeAssistant()
if asst == nil || asst.status != assistantStatusDone {
	t.Fatalf("assistant not finalized: %+v", asst)
}
out := plainView(m.renderer.renderTranscript(m.transcript, renderContext{width: 80}))
if !strings.Contains(out, "expected final text") {
	t.Fatalf("final text missing from transcript viewport:\n%s", out)
}
```

Expected after updates: PASS.

- [ ] **Step 9: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestSubmitPrompt|TestHandleRuntimeEvent|TestResponseMsg|TestAgentEnd'
git status --short
```

Commit:

```sh
git add cmd/ori/tui_command.go cmd/ori/tui_update.go cmd/ori/tui_render_test.go
git commit -m "feat: drive TUI responses through transcript"
```

---

### Task 6: Migrate Slash Commands And Management Panels To Transcript Plus Overlay

**Files:**
- Modify: `cmd/ori/tui_command.go`
- Modify: `cmd/ori/tui_management.go`
- Modify: `cmd/ori/tui_view.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Write failing command and overlay tests**

Append to `cmd/ori/tui_render_test.go`:

```go
func TestApplySlashCommandResultAppendsCommandBlock(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)

	cmd := m.applySlashCommandResult("/status", &appcore.CommandResult{Text: "ready", Status: "ready"})
	if cmd != nil {
		t.Fatalf("plain command result returned print command")
	}
	if len(m.transcript.blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(m.transcript.blocks))
	}
	got := m.transcript.blocks[0].command
	if got == nil || got.command != "/status" || got.text != "ready" {
		t.Fatalf("command block not appended: %+v", got)
	}
}

func TestApplySlashCommandResultOpensOverlayAndRecordsCommand(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)

	cmd := m.applySlashCommandResult("/mcp", &appcore.CommandResult{
		Text:      "MCP status",
		UIRequest: appcore.UIRequestMCP,
	})
	if cmd != nil {
		t.Fatalf("UI command returned print command")
	}
	if m.panel == nil || m.panel.kind != appcore.UIRequestMCP {
		t.Fatalf("panel not opened: %+v", m.panel)
	}
	if m.focus != focusOverlay {
		t.Fatalf("focus = %q, want overlay", m.focus)
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].command == nil {
		t.Fatalf("command transcript block missing: %+v", m.transcript.blocks)
	}
}

func TestClearCommandClearsTranscriptAndAddsSystemBlock(t *testing.T) {
	m := &interactiveModel{renderer: transcriptRenderer{}, focus: focusInput}
	m.initTranscriptViewport(80, 10)
	m.transcript.appendUserBlock("u1", "old", time.Unix(1, 0))

	cmd := m.applySlashCommandResult("/clear", &appcore.CommandResult{
		Text:          "New session started.",
		Status:        "ready",
		ResetSession:  true,
		ClearViewport: true,
	})
	if cmd != nil {
		t.Fatalf("clear returned terminal clear command")
	}
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].system == nil {
		t.Fatalf("clear did not replace transcript with system block: %+v", m.transcript.blocks)
	}
}
```

- [ ] **Step 2: Run command tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestApplySlashCommandResultAppendsCommandBlock|TestApplySlashCommandResultOpensOverlayAndRecordsCommand|TestClearCommandClearsTranscriptAndAddsSystemBlock'
```

Expected: FAIL because `applySlashCommandResult` still returns print/clear commands and does not append command blocks.

- [ ] **Step 3: Add command result reducer helper**

Add to `cmd/ori/tui_reducer.go`:

```go
func (m *interactiveModel) appendCommandResult(input string, result *appcore.CommandResult) {
	if result == nil {
		return
	}
	if result.ResetSession || result.ClearViewport {
		m.transcript.clear()
		message := result.Text
		if message == "" {
			message = "Session reset."
		}
		m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, message, time.Now())
		return
	}
	if result.Text == "" && result.Markdown == "" && result.Status == "" {
		return
	}
	m.transcript.appendCommandBlock(
		m.nextBlockID("command"),
		input,
		result.Text,
		result.Markdown,
		result.Status,
		time.Now(),
	)
}
```

- [ ] **Step 4: Migrate slash command result application**

Replace `applySlashCommandResult` in `cmd/ori/tui_command.go` with:

```go
func (m *interactiveModel) applySlashCommandResult(input string, result *appcore.CommandResult) tea.Cmd {
	if result == nil {
		return nil
	}
	if result.PromptReplacement != "" {
		return m.submitPrompt(input, result.PromptReplacement)
	}
	m.appendCommandResult(input, result)
	if result.UIRequest != "" {
		m.openManagementPanel(result.UIRequest)
		m.focus = focusOverlay
	}
	if result.Status != "" {
		m.status = result.Status
	}
	m.refreshTranscriptViewport()
	m.viewVersion++
	return nil
}
```

Leave `renderedCommandOutput`, `renderCommandResultBlock`, and `renderResetCommandOutput` in place until Task 8, because existing tests and non-migrated paths may still reference them.

- [ ] **Step 5: Update management panel focus behavior**

In `cmd/ori/tui_management.go`, modify `openManagementPanel` so it sets focus:

```go
func (m *interactiveModel) openManagementPanel(kind string) {
	panel := &managementPanel{kind: kind, configDraft: map[string]string{}}
	// keep existing config draft initialization here
	m.panel = panel
	m.focus = focusOverlay
	m.viewVersion++
}
```

In the Esc handling branch for management panel close, set:

```go
	m.focus = focusInput
```

- [ ] **Step 6: Render overlay separately from transcript**

In `cmd/ori/tui_view.go`, keep this order:

```go
if view := m.viewport.View(); strings.TrimSpace(view) != "" {
	s.WriteString(view)
	s.WriteString("\n")
}
if m.panel != nil {
	s.WriteString("\n")
	s.WriteString(m.renderManagementPanel())
}
```

Do not append management panel content to `m.transcript`.

- [ ] **Step 7: Run command and overlay tests**

Run:

```sh
go test ./cmd/ori -run 'TestApplySlashCommandResult|TestClearCommandClearsTranscriptAndAddsSystemBlock|TestManagementPanelOpensFromUIRequest'
```

Expected: PASS after updating `TestManagementPanelOpensFromUIRequest` to assert transcript command block plus `m.panel`, rather than expecting fallback text in the main view.

- [ ] **Step 8: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestApplySlashCommandResult|TestClearCommand|TestManagementPanel'
git status --short
```

Commit:

```sh
git add cmd/ori/tui_command.go cmd/ori/tui_management.go cmd/ori/tui_view.go cmd/ori/tui_render_test.go
git commit -m "feat: record commands in TUI transcript"
```

---

### Task 7: Migrate Session Resume To Transcript Blocks

**Files:**
- Modify: `cmd/ori/tui_management.go`
- Modify: `cmd/ori/tui_reducer.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Write failing session transcript tests**

Append to `cmd/ori/tui_render_test.go`:

```go
func TestTranscriptFromSessionMessagesBuildsBlocks(t *testing.T) {
	messages := []appcore.SessionMessageView{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "answer", Reasoning: "thinking", ToolCalls: []appcore.SessionToolCallView{
			{ID: "tool-1", Name: "shell", ArgumentsMap: map[string]any{"cmd": "date"}},
		}},
		{Role: "tool", ToolCallID: "tool-1", Name: "shell", Content: "tool result"},
	}

	tr := transcriptFromSessionMessages(messages, time.Unix(1, 0))

	if len(tr.blocks) != 2 {
		t.Fatalf("blocks = %d, want user + assistant", len(tr.blocks))
	}
	asst := tr.blocks[1].assistant
	if asst == nil {
		t.Fatalf("assistant block missing: %+v", tr.blocks[1])
	}
	out := plainView(transcriptRenderer{}.renderTranscript(tr, renderContext{width: 80}))
	for _, want := range []string{"hello", "thinking", "answer", "shell", "tool result"} {
		if !strings.Contains(out, want) {
			t.Fatalf("session transcript missing %q:\n%s", want, out)
		}
	}
}

func TestResumeSelectedSessionLoadsTranscript(t *testing.T) {
	m := newSessionPanelTestModel(t)
	m.initTranscriptViewport(80, 10)
	m.openManagementPanel(appcore.UIRequestSessions)
	for i, item := range m.managementSessions() {
		if item.Key == "cli:target" {
			m.panel.selected = i
			break
		}
	}

	cmd := m.resumeSelectedSession()
	if cmd != nil {
		t.Fatalf("resume returned print command")
	}
	if len(m.transcript.blocks) == 0 {
		t.Fatalf("resume did not load transcript blocks")
	}
	if m.focus != focusInput {
		t.Fatalf("focus = %q, want input", m.focus)
	}
}
```

- [ ] **Step 2: Run session tests and verify they fail**

Run:

```sh
go test ./cmd/ori -run 'TestTranscriptFromSessionMessagesBuildsBlocks|TestResumeSelectedSessionLoadsTranscript'
```

Expected: FAIL with `undefined: transcriptFromSessionMessages` or old resume returning `printAbove` command.

- [ ] **Step 3: Implement session transcript conversion**

Add to `cmd/ori/tui_reducer.go`:

```go
func transcriptFromSessionMessages(messages []appcore.SessionMessageView, ts time.Time) transcript {
	var tr transcript
	nextID := 0
	newID := func(prefix string) string {
		nextID++
		return fmt.Sprintf("%s-%d", prefix, nextID)
	}
	var currentAssistant *assistantBlock

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			tr.appendUserBlock(newID("user"), msg.Content, ts)
			currentAssistant = nil
		case "assistant":
			currentAssistant = tr.appendAssistantBlock(newID("assistant"), ts)
			currentAssistant.status = assistantStatusDone
			currentAssistant.renderCursor = false
			if msg.Reasoning != "" {
				currentAssistant.appendReasoningDelta(msg.Reasoning)
			}
			for _, call := range msg.ToolCalls {
				currentAssistant.upsertToolStart(call.ID, call.Name, call.ArgumentsMap, ts)
			}
			if msg.Content != "" {
				currentAssistant.appendTextDelta(msg.Content)
				currentAssistant.finalText = msg.Content
			}
			currentAssistant.status = assistantStatusDone
		case "tool", "tool_result", "toolResult":
			if currentAssistant == nil {
				currentAssistant = tr.appendAssistantBlock(newID("assistant"), ts)
				currentAssistant.status = assistantStatusDone
			}
			currentAssistant.finishTool(msg.ToolCallID, msg.Name, msg.Content, false, ts)
			currentAssistant.status = assistantStatusDone
		default:
			tr.appendSystemBlock(newID("system"), systemLevelInfo, msg.Role+": "+msg.Content, ts)
			currentAssistant = nil
		}
	}
	tr.activeAssistantID = ""
	return tr
}
```

- [ ] **Step 4: Migrate resumeSelectedSession**

In `cmd/ori/tui_management.go`, replace the body of `resumeSelectedSession` after it selects `item` and `messages` with:

```go
	m.transcript = transcriptFromSessionMessages(messages, time.Now())
	m.transcript.appendSystemBlock(m.nextBlockID("system"), systemLevelInfo, "resumed "+item.Key, time.Now())
	m.sessionKey = item.Key
	m.chatID = item.Key
	m.subscribeRuntimeEvents(item.Key)
	m.panel = nil
	m.focus = focusInput
	m.active = false
	m.waiting = false
	m.responseReceived = false
	m.status = "ready"
	m.refreshTranscriptViewport()
	m.viewVersion++
	return nil
```

Do not call `applyClearCommandResult`, `clearTerminalHistory`, `printAbove`, or `renderSessionResumeOutput` from this path.

- [ ] **Step 5: Run session tests**

Run:

```sh
go test ./cmd/ori -run 'TestTranscriptFromSessionMessagesBuildsBlocks|TestResumeSelectedSessionLoadsTranscript|TestRenderSessionResumeOutputIncludesSummary'
```

Expected: the first two tests PASS. `TestRenderSessionResumeOutputIncludesSummary` may remain as a legacy formatter test until Task 8, or be deleted in Task 8 when the formatter is removed.

- [ ] **Step 6: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori -run 'TestTranscriptFromSessionMessages|TestResumeSelectedSession'
git status --short
```

Commit:

```sh
git add cmd/ori/tui_management.go cmd/ori/tui_reducer.go cmd/ori/tui_render_test.go
git commit -m "feat: load sessions into TUI transcript"
```

---

### Task 8: Remove Legacy Double-Plane State And Formatters

**Files:**
- Modify: `cmd/ori/tui_model.go`
- Modify: `cmd/ori/tui_update.go`
- Modify: `cmd/ori/tui_view.go`
- Modify: `cmd/ori/tui_command.go`
- Modify: `cmd/ori/tui_management.go`
- Modify: `cmd/ori/output.go`
- Modify: `cmd/ori/tui_render_test.go`

- [ ] **Step 1: Search for old state references**

Run:

```sh
rg -n 'currentRound|displayedText|typewriterQueue|flushedText|formatCurrentState|formatFinalMessage|maybeFlushStreamWindow|renderCompletedRound|renderSessionResumeOutput|renderSessionHistory' cmd/ori
```

Expected before cleanup: matches in model, update, view, command, management, output, and tests.

- [ ] **Step 2: Remove old fields from interactiveModel**

In `cmd/ori/tui_model.go`, remove:

```go
	currentRound    *thinkingRound
	streamText      string
	displayedText   string
	typewriterQueue []rune
	flushedText     string
	lastRenderedText   string
	lastRenderedOutput string
	lastRenderedWidth  int
```

In `viewCacheKey`, remove:

```go
	displayedText      string
	typewriterQueueLen int
```

- [ ] **Step 3: Remove typewriter and stream flush code**

In `cmd/ori/tui_update.go`, remove:

```go
typewriterTickMsg handling
tickTypewriter
maybeFlushStreamWindowThrottled
flushLineThreshold
maybeFlushStreamWindow
rememberFlushedText
formatCurrentState
formatFinalMessage
unflushedFinalContent
clearActiveState fields related to stream text
renderCompletedRound
```

Keep `contentsToString`, `outboundFromAgentEventFinal`, `cloneToolArgs`, and `hasRunningToolCall` only if still referenced. If `hasRunningToolCall` becomes unused, remove it.

- [ ] **Step 4: Remove live content rendering from View**

In `cmd/ori/tui_view.go`, remove:

```go
renderRound
renderLiveContent
```

Keep `closeOpenMarkdown` and `lastUnclosedLink` only if `renderMarkdown` or renderer tests still use them. If no production path uses `closeOpenMarkdown`, move its tests to renderer scope or remove both function and test together.

- [ ] **Step 5: Remove old command and session formatters**

In `cmd/ori/tui_command.go`, remove:

```go
commandResultOutput
renderCommandResultBlock
renderedCommandOutput
renderResetCommandOutput
clearTerminalHistory
```

In `cmd/ori/tui_management.go`, remove:

```go
renderSessionResumeOutput
renderSessionHistory
renderSessionTurn
renderSessionUserMessage
renderSessionAssistantMessages
sessionThinkingRound
renderSessionToolMessage
```

Delete tests that only verify these removed formatters. Replace them with transcript conversion or renderer tests from Tasks 6 and 7.

- [ ] **Step 6: Update clear/reset tests**

Replace old assertions like:

```go
if m.active || m.waiting || m.currentRound != nil || m.streamText != "" || m.displayedText != "" || len(m.typewriterQueue) != 0 || m.flushedText != "" {
	t.Fatalf("...")
}
```

with:

```go
if m.active || m.waiting {
	t.Fatalf("expected inactive state, active=%v waiting=%v", m.active, m.waiting)
}
if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].system == nil {
	t.Fatalf("expected fresh system block after reset: %+v", m.transcript.blocks)
}
```

- [ ] **Step 7: Run cleanup search and verify old references are gone**

Run:

```sh
rg -n 'currentRound|displayedText|typewriterQueue|flushedText|formatCurrentState|formatFinalMessage|maybeFlushStreamWindow|renderCompletedRound|renderSessionResumeOutput|renderSessionHistory' cmd/ori
```

Expected: no matches, except historical wording in comments if a comment explicitly explains removal. Remove those comments if they create confusion.

- [ ] **Step 8: Run focused TUI tests**

Run:

```sh
go test ./cmd/ori
```

Expected: PASS.

- [ ] **Step 9: Format and commit**

Run:

```sh
make fmt
go test ./cmd/ori
git status --short
```

Expected: no top-level `ori` binary staged.

Commit:

```sh
git add cmd/ori
git commit -m "refactor: remove legacy TUI rendering plane"
```

---

### Task 9: Full Verification And Terminal Smoke Test

**Files:**
- Modify only if verification exposes a regression in `cmd/ori`.

- [ ] **Step 1: Run focused package verification**

Run:

```sh
go test ./cmd/ori
```

Expected: PASS.

- [ ] **Step 2: Run project formatting**

Run:

```sh
make fmt
```

Expected: command completes successfully. If it modifies files, inspect them with `git diff --stat`.

- [ ] **Step 3: Run full project check**

Run:

```sh
make check
```

Expected: PASS. If it fails outside `cmd/ori`, record the exact failing package and test in the final implementation note and verify `go test ./cmd/ori` still passes.

- [ ] **Step 4: Inspect generated binary churn**

Run:

```sh
git status --short
```

Expected: no top-level `ori` or `gateway` binary changes. If `ori` changed because of local build output, restore it with:

```sh
git show HEAD:ori > ori
git show HEAD:ori | cmp -s - ori
```

Expected: `cmp` exits 0.

- [ ] **Step 5: Manual interactive smoke**

Run:

```sh
go run ./cmd/ori agent
```

In the TUI, exercise:

- Send a short prompt and confirm user + assistant appear in the viewport.
- Send a long prompt and confirm output stays in the viewport without duplicated final text.
- Run a tool-using prompt and confirm running/done tool segments stay in order.
- Run `/status` and confirm a command block appears in the transcript.
- Run `/mcp`, close with Esc, and confirm the command block remains while the overlay disappears.
- Run `/sessions`, resume a session, and confirm transcript blocks load without terminal clear flicker.
- Press PageUp/PageDown and confirm scroll position is not stolen by new output.

Exit with Ctrl-C.

Expected: no duplicated assistant final, no reasoning/tool blocks appearing between final answer lines, no terminal scrollback-only content required for the visible history.

- [ ] **Step 6: Commit verification fixes if any were required**

If Step 5 required code or test changes, run:

```sh
make fmt
go test ./cmd/ori
git add cmd/ori
git commit -m "fix: stabilize transcript viewport smoke issues"
```

If no changes were required, do not create an empty commit.

---

## Self-Review Checklist

- Spec coverage:
  - Transcript viewport is implemented in Tasks 4 and 5.
  - `printAbove` is removed from normal response and command paths in Tasks 5 and 6.
  - Reducer, transcript model, renderer, viewport shell, and overlay boundaries are implemented in Tasks 1 through 6.
  - Slash command, management overlay, and session replay are implemented in Tasks 6 and 7.
  - Legacy `flushedText/currentRound/displayedText` paths are removed in Task 8.
  - Focused and full verification are covered in Task 9.
- Placeholder scan:
  - The plan contains no open-ended placeholder sections.
  - Each code-changing step includes concrete code or an exact replacement pattern.
- Type consistency:
  - Transcript types use lower-case names consistently: `transcript`, `block`, `assistantBlock`, `assistantSegment`.
  - Renderer types use `transcriptRenderer` and `renderContext`.
  - Reducer helpers use the same `assistantStatus`, `toolStatus`, and `finalSource` constants defined in Task 1.
