package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type reasoningRenderMode int

const (
	reasoningModeCompleted reasoningRenderMode = iota
	reasoningModeLive
)

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
