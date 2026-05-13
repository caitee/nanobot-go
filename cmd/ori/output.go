package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// renderAssistantHeader returns the persistent "✦ ori" banner (preceded
// by a separator). Flushed once at the top of each assistant response so it
// sits above the first round of reasoning/tool calls.
func renderAssistantHeader() string {
	var b strings.Builder
	b.WriteString(borderStyle.Render(strings.Repeat("─", getTerminalWidth())))
	b.WriteString("\n")
	b.WriteString(spinnerStyle.Render("✦"))
	b.WriteString(" ")
	b.WriteString(assistantLabelStyle.Render("ori"))
	b.WriteString("\n")
	return b.String()
}

// formatAssistantMessage renders rounds + content for persistent terminal
// output. Header is rendered separately (see renderAssistantHeader) so it can
// be flushed once at the start of the response, not re-emitted on every
// finalize/cancel call.
func formatAssistantMessage(rounds []thinkingRound, content, reasoning string) string {
	var b strings.Builder

	// Display all rounds in order (reasoning + tool calls)
	for i, round := range rounds {
		// Add spacing between rounds (but not before the first round)
		if i > 0 && (round.reasoning != "" || len(round.toolCalls) > 0) {
			b.WriteString("\n")
		}

		b.WriteString(renderRoundContent(round, false))
	}

	// Content with reasoning-detection
	b.WriteString(formatContentWithReasoning(content, reasoning))
	b.WriteString("\n")
	return b.String()
}

type reasoningRenderMode int

const (
	reasoningModeCompleted reasoningRenderMode = iota
	reasoningModeLive
)

func renderRoundContent(round thinkingRound, isLive bool) string {
	var b strings.Builder
	mode := reasoningModeCompleted
	if isLive {
		mode = reasoningModeLive
	}
	if round.reasoning != "" {
		b.WriteString(renderReasoningBlock(round.reasoning, mode))
		b.WriteString("\n")
	}
	for i := range round.toolCalls {
		b.WriteString(renderToolCallBlock(round.toolCalls[i], isLive))
	}
	return b.String()
}

func renderReasoningBlock(reasoning string, mode reasoningRenderMode) string {
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
	b.WriteString(reasoningHeaderStyle.Render(fmt.Sprintf("  thinking · %d lines summarized", len(lines))))
	b.WriteString("\n")
	b.WriteString(renderReasoningMarkdown(preview))
	return b.String()
}

func nonEmptyLines(s string) []string {
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// formatContentWithReasoning splits content into reasoning (gray) and answer (white).
// The reasoning parameter is the extracted reasoning content (passed separately).
// The content is rendered as markdown for nice formatting.
func formatContentWithReasoning(content string, reasoning string) string {
	if content == "" {
		return ""
	}

	// If reasoning was provided separately (extended thinking), content is just the answer
	if reasoning != "" {
		return renderMarkdown(content)
	}

	// Check if LLM used --- Reasoning --- markers
	reasoningStart := strings.Index(content, "--- Reasoning ---")
	reasoningEnd := strings.LastIndex(content, "---")
	if reasoningStart >= 0 && reasoningEnd > reasoningStart {
		reasoningBlock := content[reasoningStart+len("--- Reasoning ---") : reasoningEnd]
		after := content[reasoningEnd+len("---"):]
		var b strings.Builder
		b.WriteString(renderReasoningMarkdown(reasoningBlock))
		if strings.TrimSpace(after) != "" {
			b.WriteString("\n")
			b.WriteString(renderMarkdown(strings.TrimSpace(after)))
		}
		return b.String()
	}

	// No markers — try heuristic detection
	lines := strings.Split(content, "\n")
	if len(lines) <= 1 {
		return renderMarkdown(content)
	}

	var reasoningLines, answerLines []string
	inReasoning := true
	for i, line := range lines {
		lower := strings.ToLower(line)
		if strings.HasPrefix(line, "##") || strings.HasPrefix(line, "**") ||
			strings.Contains(lower, "final answer") || strings.Contains(lower, "最终答案") ||
			strings.Contains(lower, "回答:") || strings.Contains(lower, "reply:") ||
			(strings.Contains(lower, "答案") && i > len(lines)/2) {
			inReasoning = false
		}
		if inReasoning {
			reasoningLines = append(reasoningLines, line)
		} else {
			answerLines = append(answerLines, line)
		}
	}

	var b strings.Builder
	if len(reasoningLines) > 0 {
		b.WriteString(renderReasoningMarkdown(strings.Join(reasoningLines, "\n")))
		b.WriteString("\n")
	}
	if len(answerLines) > 0 {
		b.WriteString(renderMarkdown(strings.Join(answerLines, "\n")))
	} else if len(reasoningLines) > 0 {
		// No clear answer marker — treat last 25% as answer
		total := len(reasoningLines)
		answerStart := int(float64(total) * 0.75)
		if answerStart < total-1 {
			b.WriteString("\n")
			b.WriteString(renderReasoningMarkdown(strings.Join(reasoningLines[:answerStart], "\n")))
			b.WriteString("\n")
			b.WriteString(renderMarkdown(strings.Join(reasoningLines[answerStart:], "\n")))
		}
	}
	return b.String()
}

// formatArgs formats tool arguments as a stable compact string for fallback
// paths and tests. Structured rendering uses the original args map when
// available.
func formatArgs(args map[string]any) string {
	if args == nil || len(args) == 0 {
		return ""
	}
	keys := sortedArgKeys(args)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s := formatArgValue(args[k])
		s = strings.ReplaceAll(s, "\n", " ")
		parts = append(parts, fmt.Sprintf("%s: %s", k, s))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, ", ")
}

func sortedArgKeys(args map[string]any) []string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatArgValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprintf("%v", v)
	}
}

// truncateStr collapses newlines and truncates a string to maxLen display width.
// Uses lipgloss.Width for correct CJK/unicode width calculation.
func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if lipgloss.Width(s) <= maxLen {
		return s
	}
	// Truncate by display width
	result := []rune(s)
	for i := len(result); i > 0; i-- {
		candidate := string(result[:i])
		if lipgloss.Width(candidate)+3 <= maxLen { // +3 for "..."
			return candidate + "..."
		}
	}
	return "..."
}

// toolDisplayWidth returns the max column width we budget for a truncated
// tool args/result line. Centralized so the cache below matches what the
// renderers in tui_view.go / tui_update.go use.
func toolDisplayWidth() int {
	const maxToolDetailPrefixWidth = 14 // "    └ Result: "
	w := getTerminalWidth() - maxToolDetailPrefixWidth
	if w < 1 {
		return 1
	}
	return w
}

// truncatedField caches the truncated form of a string for a given display
// width. truncateStr is O(len(source)) in the worst case; since View() runs
// on every tea.Msg this would dominate CPU on long tool outputs. The cache
// invalidates implicitly: when source or width changes, the next get() call
// recomputes.
type truncatedField struct {
	source string
	width  int
	cached string
}

// set replaces the source string and invalidates the cache.
func (t *truncatedField) set(s string) {
	if s == t.source {
		return
	}
	t.source = s
	t.cached = ""
	t.width = 0
}

// get returns the truncated form of source at the given width, reusing the
// cache when possible.
func (t *truncatedField) get(width int) string {
	if t.source == "" {
		return ""
	}
	if t.cached != "" && t.width == width {
		return t.cached
	}
	t.cached = truncateStr(t.source, width)
	t.width = width
	return t.cached
}

// renderToolCallBlock renders a tool call's args/result/error as a structured
// activity block.
func renderToolCallBlock(tc toolCallEntry, isLive bool) string {
	var b strings.Builder
	icon, statusText, iconStyle := toolStatusParts(tc, isLive)
	b.WriteString("  ")
	b.WriteString(iconStyle.Render(icon) + " ")
	b.WriteString(toolEntryStyle.Render(tc.name))
	b.WriteString(toolDurationStyle.Render(statusText))
	b.WriteString("\n")

	b.WriteString(renderToolArgsBlock(tc))

	if tc.status == "running" && tc.partial != "" {
		b.WriteString(renderPreviewBlock("Preview", tc.partial, toolPreviewStyle, 4))
		return b.String()
	}
	if tc.status == "error" && tc.result != "" {
		b.WriteString(renderPreviewBlock("Error", tc.result, toolErrorStyle, 4))
	} else if tc.status == "done" && tc.result != "" {
		b.WriteString(renderPreviewBlock("Result", tc.result, toolPreviewStyle, 4))
	}
	return b.String()
}

func toolStatusParts(tc toolCallEntry, isLive bool) (string, string, lipgloss.Style) {
	switch tc.status {
	case "done":
		return "✓", fmt.Sprintf(" %s", toolMetaText(tc)), toolDoneStyle
	case "error":
		return "✗", fmt.Sprintf(" %s", toolMetaText(tc)), toolErrorStyle
	case "running":
		if isLive {
			if tc.startTime.IsZero() {
				return "●", " running", toolPulseStyle
			}
			return "●", " running " + formatDuration(timeSince(tc.startTime)), toolPulseStyle
		}
		return "○", " running", toolRunningStyle
	default:
		return "○", " pending", toolEntryStyle
	}
}

func toolMetaText(tc toolCallEntry) string {
	parts := make([]string, 0, 2)
	if tc.durationMs >= 0 {
		parts = append(parts, formatDuration(tc.durationMs))
	}
	if size := toolResultSize(tc); size != "" {
		parts = append(parts, size)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func toolResultSize(tc toolCallEntry) string {
	source := tc.result
	if source == "" {
		source = tc.partial
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	return fmt.Sprintf("%d chars", len([]rune(source)))
}

func timeSince(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	ms := time.Now().Sub(t).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func renderToolArgsBlock(tc toolCallEntry) string {
	if len(tc.argsMap) == 0 && tc.args == "" {
		return ""
	}
	if len(tc.argsMap) == 0 {
		return renderKeyValueBlock([]toolKeyValue{{key: "arg", value: tc.args}}, toolArgsStyle)
	}
	rows := make([]toolKeyValue, 0, len(tc.argsMap))
	for _, key := range sortedArgKeys(tc.argsMap) {
		rows = append(rows, toolKeyValue{key: key, value: formatArgValue(tc.argsMap[key])})
	}
	return renderKeyValueBlock(rows, toolArgsStyle)
}

type toolKeyValue struct {
	key   string
	value string
}

func renderKeyValueBlock(rows []toolKeyValue, style lipgloss.Style) string {
	var b strings.Builder
	width := toolDisplayWidth()
	for _, row := range rows {
		key := truncateStr(row.key, 10)
		value := strings.ReplaceAll(row.value, "\n", " ")
		line := fmt.Sprintf("    │ %-10s %s", key, truncateStr(value, width-15))
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}
	return b.String()
}

func renderPreviewBlock(label, content string, style lipgloss.Style, maxLines int) string {
	lines := previewLines(content)
	if len(lines) == 0 {
		return ""
	}
	visible := maxLines
	if len(lines) < visible {
		visible = len(lines)
	}
	width := toolDisplayWidth()
	var b strings.Builder
	b.WriteString(style.Render("    │ " + label))
	b.WriteString("\n")
	for _, line := range lines[:visible] {
		b.WriteString(style.Render("    │ " + truncateStr(line, width)))
		b.WriteString("\n")
	}
	if hidden := len(lines) - visible; hidden > 0 {
		b.WriteString(style.Render(fmt.Sprintf("    │ ... %d more lines", hidden)))
		b.WriteString("\n")
	}
	return b.String()
}

func previewLines(content string) []string {
	raw := strings.Split(strings.TrimSpace(content), "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

// formatDuration formats duration in milliseconds to a human-readable string
func formatDuration(ms int64) string {
	if ms < 0 {
		return ""
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	return fmt.Sprintf("%.1fm", float64(ms)/60000)
}

// getMarkdownRenderer creates or returns a cached glamour renderer for the current terminal width.
func getMarkdownRenderer() *glamour.TermRenderer {
	width := getTerminalWidth()
	// Leave some margin for padding
	wrapWidth := width - 4
	if wrapWidth < 40 {
		wrapWidth = 40
	}
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(wrapWidth),
	)
	return renderer
}

// getReasoningRenderer creates or returns a cached dimmed glamour renderer for reasoning content.
func getReasoningRenderer() *glamour.TermRenderer {
	width := getTerminalWidth()
	wrapWidth := width - 4
	if wrapWidth < 40 {
		wrapWidth = 40
	}
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes(reasoningStyleJSON),
		glamour.WithWordWrap(wrapWidth),
	)
	return renderer
}

// renderReasoningMarkdown renders reasoning content as markdown with a dimmed glamour style.
// Uses a separate glamour renderer (mdReasoningRenderer) with gray colors so that
// reasoning is visually distinct from the final answer, while still getting proper
// markdown formatting (lists, code blocks, headings, etc.).
func renderReasoningMarkdown(content string) string {
	if content == "" {
		return ""
	}
	content = preprocessMath(content)
	renderer := getReasoningRenderer()
	if renderer == nil {
		// Fallback: plain reasoningStyle if renderer failed to init
		return reasoningStyle.Render(content)
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return reasoningStyle.Render(content)
	}
	return strings.TrimSuffix(rendered, "\n")
}

// preprocessMath converts LaTeX math delimiters into code spans/blocks so that
// glamour (which has no math support) preserves formulas instead of mangling them.
// Block math ($$...$$) becomes fenced code blocks; inline math ($...$) becomes
// backtick code spans.
var (
	// Match block math: $$ ... $$ (possibly multiline)
	reBlockMath = regexp.MustCompile(`(?s)\$\$(.+?)\$\$`)
	// Match inline math: $ ... $ (single line, non-greedy)
	reInlineMath = regexp.MustCompile(`\$([^$\n]+?)\$`)
)

func preprocessMath(content string) string {
	// First, replace block math with fenced code blocks
	content = reBlockMath.ReplaceAllStringFunc(content, func(m string) string {
		inner := strings.TrimSpace(m[2 : len(m)-2])
		// If it's a single line, use inline code style for compactness
		if !strings.Contains(inner, "\n") {
			return "`" + inner + "`"
		}
		return "```\n" + inner + "\n```"
	})
	// Then, replace remaining inline math with backtick code spans
	content = reInlineMath.ReplaceAllStringFunc(content, func(m string) string {
		inner := m[1 : len(m)-1]
		// Skip if already inside backticks (contains backtick)
		if strings.Contains(inner, "`") {
			return m
		}
		return "`" + inner + "`"
	})
	return content
}

// renderMarkdown renders markdown content using glamour
func renderMarkdown(content string) string {
	if content == "" {
		return ""
	}
	content = preprocessMath(content)
	renderer := getMarkdownRenderer()
	if renderer == nil {
		return content
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimSuffix(rendered, "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
