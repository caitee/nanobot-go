package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type renderContext struct {
	width  int
	focus  focusArea
	active bool
	now    time.Time
}

type transcriptRenderer struct{}

func (r transcriptRenderer) renderTranscript(tr transcript, ctx renderContext) string {
	ctx = normalizeRenderContext(ctx)
	parts := make([]string, 0, len(tr.blocks))
	for i := range tr.blocks {
		rendered := strings.TrimRight(r.renderBlock(tr.blocks[i], ctx), "\n")
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n\n")
}

func (r transcriptRenderer) renderBlock(b block, ctx renderContext) string {
	ctx = normalizeRenderContext(ctx)
	switch b.kind {
	case blockKindUser:
		return r.renderUserBlock(b.user, ctx)
	case blockKindAssistant:
		return r.renderAssistantBlock(b.assistant, ctx)
	case blockKindCommand:
		return r.renderCommandBlock(b.command, ctx)
	case blockKindSystem:
		return r.renderSystemBlock(b.system, ctx)
	default:
		return ""
	}
}

func (r transcriptRenderer) renderUserBlock(user *userBlock, _ renderContext) string {
	if user == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(userPromptStyle.Render("you"))
	if user.content != "" {
		b.WriteString("\n")
		b.WriteString(userMessageStyle.Render(user.content))
	}
	return b.String()
}

func (r transcriptRenderer) renderAssistantBlock(asst *assistantBlock, ctx renderContext) string {
	if asst == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(spinnerStyle.Render("✦"))
	b.WriteString(" ")
	b.WriteString(assistantLabelStyle.Render("ori"))
	if status := transcriptStatusString(asst.status); status != "" && asst.status != assistantStatusDone {
		b.WriteString(waitingStyle.Render(" · " + status))
	}
	if asst.finalConflict {
		b.WriteString("\n")
		b.WriteString(toolErrorStyle.Render("  merge conflict resolved"))
	}

	renderedText := false
	for i := range asst.segments {
		rendered := strings.TrimRight(r.renderSegment(asst.segments[i], ctx), "\n")
		if strings.TrimSpace(rendered) == "" {
			continue
		}
		if asst.segments[i].kind == segmentKindText {
			renderedText = true
		}
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	if !renderedText && asst.finalText != "" {
		b.WriteString("\n")
		b.WriteString(renderMarkdown(asst.finalText))
	}
	return b.String()
}

func (r transcriptRenderer) renderSegment(seg assistantSegment, ctx renderContext) string {
	switch seg.kind {
	case segmentKindReasoning:
		if seg.reasoning == nil {
			return ""
		}
		return renderReasoningBlock(seg.reasoning.text, reasoningModeLive)
	case segmentKindText:
		if seg.text == nil || seg.text.text == "" {
			return ""
		}
		return renderMarkdown(seg.text.text)
	case segmentKindTool:
		return r.renderToolSegment(seg.tool, ctx)
	default:
		return ""
	}
}

func (r transcriptRenderer) renderToolSegment(tool *toolCallSegment, ctx renderContext) string {
	if tool == nil {
		return ""
	}
	ctx = normalizeRenderContext(ctx)
	var b strings.Builder
	icon, status, iconStyle := toolSegmentStatusParts(tool, ctx)
	name := firstNonEmpty(tool.name, "tool")
	if tool.orphan {
		name += " (orphan)"
	}
	header := fmt.Sprintf("  %s %s%s", icon, name, status)
	b.WriteString(iconStyle.Render(fitLine(header, ctx.width)))
	b.WriteString("\n")

	for _, key := range sortedArgKeys(tool.args) {
		prefix := fmt.Sprintf("    │ %-10s ", truncateStr(key, 10))
		b.WriteString(toolArgsStyle.Render(fitPrefixedLine(prefix, formatArgValue(tool.args[key]), ctx.width)))
		b.WriteString("\n")
	}

	if tool.status == toolStatusRunning && tool.partial != "" {
		b.WriteString(renderToolPreviewForWidth("Preview", tool.partial, toolPreviewStyle, ctx.width, 4))
		return b.String()
	}
	if tool.status == toolStatusError && tool.result != "" {
		b.WriteString(renderToolPreviewForWidth("Error", tool.result, toolErrorStyle, ctx.width, 4))
		return b.String()
	}
	if tool.status == toolStatusDone && tool.result != "" {
		b.WriteString(renderToolPreviewForWidth("Result", tool.result, toolPreviewStyle, ctx.width, 4))
	}
	return b.String()
}

func (r transcriptRenderer) renderCommandBlock(cmd *commandBlock, _ renderContext) string {
	if cmd == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(slashCommandSelectedStyle.Render(cmd.command))
	if cmd.status != "" {
		b.WriteString(toolDurationStyle.Render(" · " + cmd.status))
	}
	if cmd.text != "" {
		b.WriteString("\n")
		b.WriteString(cmd.text)
	}
	if cmd.markdown != "" {
		b.WriteString("\n")
		b.WriteString(renderMarkdown(cmd.markdown))
	}
	return b.String()
}

func (r transcriptRenderer) renderSystemBlock(system *systemBlock, _ renderContext) string {
	if system == nil {
		return ""
	}
	label := systemLevelLabel(system.level)
	if system.message == "" {
		return waitingStyle.Render(label)
	}
	return waitingStyle.Render(label + " · " + system.message)
}

func normalizeRenderContext(ctx renderContext) renderContext {
	if ctx.width <= 0 {
		ctx.width = getTerminalWidth()
	}
	if ctx.now.IsZero() {
		ctx.now = time.Now()
	}
	return ctx
}

func toolSegmentStatusParts(tool *toolCallSegment, ctx renderContext) (string, string, lipgloss.Style) {
	switch tool.status {
	case toolStatusDone:
		return "✓", toolSegmentMetaText(tool), toolDoneStyle
	case toolStatusError:
		return "✗", toolSegmentMetaText(tool), toolErrorStyle
	case toolStatusRunning:
		if !tool.startedAt.IsZero() {
			elapsed := ctx.now.Sub(tool.startedAt).Milliseconds()
			if elapsed < 0 {
				elapsed = 0
			}
			return "●", " running " + formatDuration(elapsed), toolPulseStyle
		}
		return "●", " running", toolPulseStyle
	default:
		return "○", " pending", toolEntryStyle
	}
}

func toolSegmentMetaText(tool *toolCallSegment) string {
	parts := make([]string, 0, 2)
	if tool.durationMs >= 0 {
		parts = append(parts, formatDuration(tool.durationMs))
	}
	if size := toolSegmentResultSize(tool); size != "" {
		parts = append(parts, size)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " · ")
}

func toolSegmentResultSize(tool *toolCallSegment) string {
	source := tool.result
	if source == "" {
		source = tool.partial
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	return fmt.Sprintf("%d chars", len([]rune(source)))
}

func renderToolPreviewForWidth(label, content string, style lipgloss.Style, width, maxLines int) string {
	lines := previewLines(content)
	if len(lines) == 0 {
		return ""
	}
	visible := min(len(lines), maxLines)
	var b strings.Builder
	b.WriteString(style.Render(fitLine("    │ "+label, width)))
	b.WriteString("\n")
	for _, line := range lines[:visible] {
		b.WriteString(style.Render(fitPrefixedLine("    │ ", line, width)))
		b.WriteString("\n")
	}
	if hidden := len(lines) - visible; hidden > 0 {
		b.WriteString(style.Render(fitLine(fmt.Sprintf("    │ ... %d more lines", hidden), width)))
		b.WriteString("\n")
	}
	return b.String()
}

func fitPrefixedLine(prefix, value string, width int) string {
	if width <= 0 {
		width = getTerminalWidth()
	}
	value = strings.ReplaceAll(value, "\n", " ")
	available := width - lipgloss.Width(prefix)
	if available < 1 {
		return fitLine(prefix, width)
	}
	return prefix + truncateStr(value, available)
}

func fitLine(line string, width int) string {
	if width <= 0 {
		width = getTerminalWidth()
	}
	if lipgloss.Width(line) <= width {
		return line
	}
	return truncateStr(line, width)
}

func systemLevelLabel(level systemLevel) string {
	switch level {
	case systemLevelWarning:
		return "warning"
	case systemLevelError:
		return "error"
	default:
		return "info"
	}
}
