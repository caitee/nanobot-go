package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func (r transcriptRenderer) renderUserBlock(user *userBlock, ctx renderContext) string {
	if user == nil {
		return ""
	}
	ctx = normalizeRenderContext(ctx)
	var b strings.Builder
	b.WriteString(userPromptStyle.Render(fitLine("you", ctx.width)))
	if user.content != "" {
		b.WriteString("\n")
		b.WriteString(userMessageStyle.Render(fitPlainText(user.content, ctx.width)))
	}
	return b.String()
}

func (r transcriptRenderer) renderAssistantBlock(asst *assistantBlock, ctx renderContext) string {
	if asst == nil {
		return ""
	}
	var b strings.Builder
	header := "✦ ori"
	if status := transcriptStatusString(asst.status); status != "" && asst.status != assistantStatusDone {
		header += " · " + status
	}
	b.WriteString(spinnerStyle.Render(fitLine(header, ctx.width)))
	if asst.finalConflict {
		b.WriteString("\n")
		b.WriteString(toolErrorStyle.Render(fitLine("  merge conflict resolved", ctx.width)))
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
		b.WriteString(renderMarkdownForWidth(asst.finalText, ctx.width))
	}
	return b.String()
}

func (r transcriptRenderer) renderSegment(seg assistantSegment, ctx renderContext) string {
	switch seg.kind {
	case segmentKindReasoning:
		if seg.reasoning == nil {
			return ""
		}
		return renderReasoningBlockForWidth(seg.reasoning.text, reasoningModeLive, ctx.width)
	case segmentKindText:
		if seg.text == nil || seg.text.text == "" {
			return ""
		}
		return renderMarkdownForWidth(seg.text.text, ctx.width)
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

func (r transcriptRenderer) renderCommandBlock(cmd *commandBlock, ctx renderContext) string {
	if cmd == nil {
		return ""
	}
	ctx = normalizeRenderContext(ctx)
	var b strings.Builder
	header := cmd.command
	if cmd.status != "" {
		header += " · " + cmd.status
	}
	b.WriteString(slashCommandSelectedStyle.Render(fitLine(header, ctx.width)))
	if cmd.text != "" {
		b.WriteString("\n")
		b.WriteString(fitPlainText(cmd.text, ctx.width))
	}
	if cmd.markdown != "" {
		b.WriteString("\n")
		b.WriteString(renderMarkdownForWidth(cmd.markdown, ctx.width))
	}
	return b.String()
}

func (r transcriptRenderer) renderSystemBlock(system *systemBlock, ctx renderContext) string {
	if system == nil {
		return ""
	}
	ctx = normalizeRenderContext(ctx)
	label := systemLevelLabel(system.level)
	if system.message == "" {
		return waitingStyle.Render(fitLine(label, ctx.width))
	}
	return waitingStyle.Render(fitLine(label+" · "+system.message, ctx.width))
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

func renderReasoningBlockForWidth(reasoning string, mode reasoningRenderMode, width int) string {
	lines := nonEmptyLines(reasoning)
	if len(lines) == 0 {
		return ""
	}
	visible := 3
	if mode == reasoningModeLive {
		visible = 5
	}
	if len(lines) < visible {
		visible = len(lines)
	}
	preview := strings.Join(lines[len(lines)-visible:], "\n")
	var b strings.Builder
	b.WriteString(reasoningHeaderStyle.Render(fitLine(fmt.Sprintf("  thinking · %d lines summarized", len(lines)), width)))
	if rendered := renderReasoningMarkdownForWidth(preview, width); rendered != "" {
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	return b.String()
}

func renderMarkdownForWidth(content string, width int) string {
	if content == "" {
		return ""
	}
	content = preprocessMath(content)
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(markdownWrapWidth(width)),
	)
	if renderer == nil {
		return fitPlainText(content, width)
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return fitPlainText(content, width)
	}
	return fitRenderedLines(strings.TrimSuffix(rendered, "\n"), width)
}

func renderReasoningMarkdownForWidth(content string, width int) string {
	if content == "" {
		return ""
	}
	content = preprocessMath(content)
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes(reasoningStyleJSON),
		glamour.WithWordWrap(markdownWrapWidth(width)),
	)
	if renderer == nil {
		return reasoningStyle.Render(fitPlainText(content, width))
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return reasoningStyle.Render(fitPlainText(content, width))
	}
	return fitRenderedLines(strings.TrimSuffix(rendered, "\n"), width)
}

func markdownWrapWidth(width int) int {
	if width <= 4 {
		return 1
	}
	return width - 4
}

func fitPlainText(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = fitLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func fitRenderedLines(rendered string, width int) string {
	lines := strings.Split(rendered, "\n")
	for i := range lines {
		lines[i] = fitLine(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

func fitPrefixedLine(prefix, value string, width int) string {
	if width <= 0 {
		width = getTerminalWidth()
	}
	value = strings.ReplaceAll(value, "\n", " ")
	available := width - ansi.StringWidth(prefix)
	if available < 1 {
		return fitLine(prefix, width)
	}
	return prefix + truncateStr(value, available)
}

func fitLine(line string, width int) string {
	if width <= 0 {
		width = getTerminalWidth()
	}
	if ansi.StringWidth(line) <= width {
		return line
	}
	if width <= 3 {
		return ansi.Truncate(line, width, "")
	}
	return ansi.Truncate(line, width, "...")
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
