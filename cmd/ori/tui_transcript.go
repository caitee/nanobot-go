package main

import (
	"strings"
	"time"
)

type blockKind int

const (
	blockKindUser blockKind = iota
	blockKindAssistant
	blockKindCommand
	blockKindSystem
)

type assistantStatus int

const (
	assistantStatusWaiting assistantStatus = iota
	assistantStatusThinking
	assistantStatusResponding
	assistantStatusRunningTools
	assistantStatusDone
	assistantStatusError
	assistantStatusCancelled
)

type segmentKind int

const (
	segmentKindReasoning segmentKind = iota
	segmentKindText
	segmentKindTool
)

type toolStatus int

const (
	toolStatusPending toolStatus = iota
	toolStatusRunning
	toolStatusDone
	toolStatusError
)

type finalSource int

const (
	finalSourceNone finalSource = iota
	finalSourceRuntime
	finalSourceFallback
)

type systemLevel int

const (
	systemLevelInfo systemLevel = iota
	systemLevelWarning
	systemLevelError
)

type focusArea int

const (
	focusInput focusArea = iota
	focusTranscript
	focusOverlay
)

type transcript struct {
	blocks            []block
	activeAssistantID string
	focus             focusArea
}

type block struct {
	kind      blockKind
	id        string
	createdAt time.Time

	user      *userBlock
	assistant *assistantBlock
	command   *commandBlock
	system    *systemBlock
}

type userBlock struct {
	id        string
	content   string
	createdAt time.Time
}

type assistantBlock struct {
	id            string
	status        assistantStatus
	segments      []assistantSegment
	createdAt     time.Time
	completedAt   time.Time
	finalText     string
	finalConflict bool
	finalSource   finalSource
}

type assistantSegment struct {
	kind      segmentKind
	createdAt time.Time
	updatedAt time.Time

	reasoning *reasoningSegment
	text      *textSegment
	tool      *toolCallSegment
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
	durationMs int64
	startedAt  time.Time
	endedAt    time.Time
	lastUpdate time.Time
	expanded   bool
	orphan     bool
}

type commandBlock struct {
	id        string
	command   string
	text      string
	markdown  string
	status    string
	createdAt time.Time
}

type systemBlock struct {
	id        string
	level     systemLevel
	message   string
	createdAt time.Time
}

func (tr *transcript) clear() {
	tr.blocks = nil
	tr.activeAssistantID = ""
	tr.focus = focusInput
}

func (tr *transcript) appendUserBlock(id, text string, createdAt time.Time) *userBlock {
	user := &userBlock{id: id, content: text, createdAt: createdAt}
	tr.blocks = append(tr.blocks, block{
		kind:      blockKindUser,
		id:        id,
		createdAt: createdAt,
		user:      user,
	})
	return user
}

func (tr *transcript) appendAssistantBlock(id string, createdAt time.Time) *assistantBlock {
	assistant := &assistantBlock{
		id:        id,
		status:    assistantStatusWaiting,
		createdAt: createdAt,
	}
	tr.blocks = append(tr.blocks, block{
		kind:      blockKindAssistant,
		id:        id,
		createdAt: createdAt,
		assistant: assistant,
	})
	tr.activeAssistantID = id
	return assistant
}

func (tr *transcript) appendCommandBlock(id, command string, text, markdown, status string, createdAt time.Time) *commandBlock {
	commandBlock := &commandBlock{
		id:        id,
		command:   command,
		text:      text,
		markdown:  markdown,
		status:    status,
		createdAt: createdAt,
	}
	tr.blocks = append(tr.blocks, block{
		kind:      blockKindCommand,
		id:        id,
		createdAt: createdAt,
		command:   commandBlock,
	})
	return commandBlock
}

func (tr *transcript) appendSystemBlock(id string, level systemLevel, message string, createdAt time.Time) *systemBlock {
	system := &systemBlock{
		id:        id,
		level:     level,
		message:   message,
		createdAt: createdAt,
	}
	tr.blocks = append(tr.blocks, block{
		kind:      blockKindSystem,
		id:        id,
		createdAt: createdAt,
		system:    system,
	})
	return system
}

func (tr *transcript) activeAssistant() *assistantBlock {
	if tr.activeAssistantID == "" {
		return nil
	}
	for i := len(tr.blocks) - 1; i >= 0; i-- {
		if tr.blocks[i].kind == blockKindAssistant && tr.blocks[i].id == tr.activeAssistantID {
			return tr.blocks[i].assistant
		}
	}
	return nil
}

func (a *assistantBlock) appendReasoningDelta(delta string, ts time.Time) {
	if delta == "" {
		return
	}
	a.setStatusIfNonTerminal(assistantStatusThinking)
	if len(a.segments) > 0 {
		last := &a.segments[len(a.segments)-1]
		if last.kind == segmentKindReasoning && last.reasoning != nil {
			last.reasoning.text += delta
			last.updatedAt = ts
			return
		}
	}
	a.segments = append(a.segments, assistantSegment{
		kind:      segmentKindReasoning,
		createdAt: ts,
		updatedAt: ts,
		reasoning: &reasoningSegment{text: delta},
	})
}

func (a *assistantBlock) appendTextDelta(delta string, ts time.Time) {
	if delta == "" {
		return
	}
	a.setStatusIfNonTerminal(assistantStatusResponding)
	if len(a.segments) > 0 {
		last := &a.segments[len(a.segments)-1]
		if last.kind == segmentKindText && last.text != nil {
			last.text.text += delta
			last.updatedAt = ts
			return
		}
	}
	a.segments = append(a.segments, assistantSegment{
		kind:      segmentKindText,
		createdAt: ts,
		updatedAt: ts,
		text:      &textSegment{text: delta},
	})
}

func (a *assistantBlock) streamedText() string {
	var builder strings.Builder
	for i := range a.segments {
		if a.segments[i].kind == segmentKindText && a.segments[i].text != nil {
			builder.WriteString(a.segments[i].text.text)
		}
	}
	return builder.String()
}

func (a *assistantBlock) setFinalText(source finalSource, final string, ts time.Time) bool {
	merged, conflict := mergeFinalText(a.streamedText(), final)
	a.finalText = merged
	a.finalConflict = conflict
	a.finalSource = source
	a.setStatusIfNonTerminal(assistantStatusDone)
	a.completedAt = ts
	if !a.applyFinalTextSegment(merged, ts) {
		a.finalConflict = true
		conflict = true
	}
	return conflict
}

func (a *assistantBlock) applyFinalTextSegment(text string, ts time.Time) bool {
	lastText := -1
	for i := range a.segments {
		if a.segments[i].kind == segmentKindText && a.segments[i].text != nil {
			lastText = i
		}
	}
	if lastText >= 0 {
		prefixBeforeLast := a.textBeforeSegment(lastText)
		suffix, ok := strings.CutPrefix(text, prefixBeforeLast)
		if !ok {
			suffix = text
			a.clearTextBeforeSegment(lastText, ts)
		}
		a.segments[lastText].text.text = suffix
		a.segments[lastText].updatedAt = ts
		return ok
	}
	if text != "" {
		a.segments = append(a.segments, assistantSegment{
			kind:      segmentKindText,
			createdAt: ts,
			updatedAt: ts,
			text:      &textSegment{text: text},
		})
	}
	return true
}

func (a *assistantBlock) textBeforeSegment(segmentIndex int) string {
	var builder strings.Builder
	for i := 0; i < segmentIndex; i++ {
		if a.segments[i].kind == segmentKindText && a.segments[i].text != nil {
			builder.WriteString(a.segments[i].text.text)
		}
	}
	return builder.String()
}

func (a *assistantBlock) clearTextBeforeSegment(segmentIndex int, ts time.Time) {
	for i := 0; i < segmentIndex; i++ {
		if a.segments[i].kind == segmentKindText && a.segments[i].text != nil {
			a.segments[i].text.text = ""
			a.segments[i].updatedAt = ts
		}
	}
}

func (a *assistantBlock) upsertToolStart(id, name string, args map[string]any, startedAt time.Time) *toolCallSegment {
	if id != "" {
		if tool, segmentIndex := a.findToolSegment(id, name); tool != nil {
			wasSettled := isSettledToolStatus(tool.status)
			tool.name = firstNonEmpty(name, tool.name)
			tool.args = cloneToolArgs(args)
			tool.startedAt = startedAt
			tool.orphan = false
			if !tool.endedAt.IsZero() {
				tool.durationMs = tool.endedAt.Sub(startedAt).Milliseconds()
			}
			if !wasSettled {
				tool.lastUpdate = startedAt
				a.touchSegment(segmentIndex, startedAt)
				tool.status = toolStatusRunning
				a.setStatusIfNonTerminal(assistantStatusRunningTools)
			}
			return tool
		}
	}
	return a.appendToolStart(id, name, args, startedAt, false)
}

func (a *assistantBlock) appendToolStart(id, name string, args map[string]any, startedAt time.Time, orphan bool) *toolCallSegment {
	tool := &toolCallSegment{
		id:         id,
		name:       name,
		args:       cloneToolArgs(args),
		status:     toolStatusRunning,
		startedAt:  startedAt,
		lastUpdate: startedAt,
		orphan:     orphan,
	}
	a.segments = append(a.segments, assistantSegment{
		kind:      segmentKindTool,
		createdAt: startedAt,
		updatedAt: startedAt,
		tool:      tool,
	})
	a.setStatusIfNonTerminal(assistantStatusRunningTools)
	return tool
}

func (a *assistantBlock) updateTool(id, name, partial string, updatedAt time.Time) *toolCallSegment {
	tool, segmentIndex := a.findToolSegment(id, name)
	if tool == nil {
		tool = a.appendToolStart(id, name, nil, updatedAt, true)
		segmentIndex = len(a.segments) - 1
	}
	wasSettled := isSettledToolStatus(tool.status)
	tool.name = firstNonEmpty(name, tool.name)
	tool.partial = partial
	tool.lastUpdate = updatedAt
	if tool.status == toolStatusPending {
		tool.status = toolStatusRunning
	}
	a.touchSegment(segmentIndex, updatedAt)
	if !wasSettled {
		a.setStatusIfNonTerminal(assistantStatusRunningTools)
	}
	return tool
}

func (a *assistantBlock) finishTool(id, name, result string, isError bool, endedAt time.Time) *toolCallSegment {
	tool, segmentIndex := a.findToolSegment(id, name)
	if tool == nil {
		tool = a.appendToolStart(id, name, nil, endedAt, true)
		segmentIndex = len(a.segments) - 1
	}
	tool.name = firstNonEmpty(name, tool.name)
	tool.result = result
	tool.endedAt = endedAt
	tool.lastUpdate = endedAt
	if isError {
		tool.status = toolStatusError
	} else {
		tool.status = toolStatusDone
	}
	if !tool.startedAt.IsZero() {
		tool.durationMs = endedAt.Sub(tool.startedAt).Milliseconds()
	}
	a.touchSegment(segmentIndex, endedAt)
	if !a.hasRunningTool() {
		a.setStatusIfNonTerminal(assistantStatusThinking)
	}
	return tool
}

func (a *assistantBlock) touchSegment(index int, ts time.Time) {
	if index >= 0 && index < len(a.segments) {
		a.segments[index].updatedAt = ts
	}
}

func (a *assistantBlock) setStatusIfNonTerminal(status assistantStatus) {
	if isTerminalAssistantStatus(a.status) {
		return
	}
	a.status = status
}

func isTerminalAssistantStatus(status assistantStatus) bool {
	switch status {
	case assistantStatusDone, assistantStatusError, assistantStatusCancelled:
		return true
	default:
		return false
	}
}

func isSettledToolStatus(status toolStatus) bool {
	return status == toolStatusDone || status == toolStatusError
}

func (a *assistantBlock) findTool(id, name string) *toolCallSegment {
	tool, _ := a.findToolSegment(id, name)
	return tool
}

func (a *assistantBlock) findToolSegment(id, name string) (*toolCallSegment, int) {
	if id != "" {
		for i := range a.segments {
			tool := a.segments[i].tool
			if a.segments[i].kind == segmentKindTool && tool != nil && tool.id == id {
				return tool, i
			}
		}
		return nil, -1
	}
	if name != "" {
		for i := len(a.segments) - 1; i >= 0; i-- {
			tool := a.segments[i].tool
			if a.segments[i].kind == segmentKindTool && tool != nil && tool.name == name && tool.status == toolStatusRunning {
				return tool, i
			}
		}
		for i := len(a.segments) - 1; i >= 0; i-- {
			tool := a.segments[i].tool
			if a.segments[i].kind == segmentKindTool && tool != nil && tool.name == name {
				return tool, i
			}
		}
	}
	return nil, -1
}

func (a *assistantBlock) hasRunningTool() bool {
	for i := range a.segments {
		tool := a.segments[i].tool
		if a.segments[i].kind == segmentKindTool && tool != nil && tool.status == toolStatusRunning {
			return true
		}
	}
	return false
}

func mergeFinalText(streamed, final string) (string, bool) {
	if final == "" {
		return streamed, false
	}
	if streamed == "" {
		return final, false
	}
	if strings.HasPrefix(final, streamed) {
		return final, false
	}
	if strings.HasPrefix(streamed, final) {
		return streamed, false
	}
	return final, true
}
