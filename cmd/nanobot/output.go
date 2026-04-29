package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// formatAssistantMessage renders a complete assistant response for persistent terminal output.
func formatAssistantMessage(tcs []toolCallEntry, content, reasoning string) string {
	var b strings.Builder
	sep := borderStyle.Render(strings.Repeat("─", min(60, getTerminalWidth())))

	b.WriteString(sep)
	b.WriteString("\n")
	b.WriteString(assistantLabelStyle.Render("nanobot"))
	b.WriteString("\n")

	// Tool calls
	if len(tcs) > 0 {
		b.WriteString("\n")
		for _, tc := range tcs {
			var icon, statusText string
			var iconStyle lipgloss.Style
			switch tc.status {
			case "done":
				icon = "✓"
				statusText = fmt.Sprintf(" • %s", formatDuration(tc.durationMs))
				iconStyle = toolDoneStyle
			case "error":
				icon = "✗"
				if tc.durationMs > 0 {
					statusText = fmt.Sprintf(" • %s", formatDuration(tc.durationMs))
				}
				iconStyle = toolErrorStyle
			default:
				icon = "○"
				iconStyle = toolEntryStyle
			}
			b.WriteString("  ")
			b.WriteString(iconStyle.Render(icon) + " ")
			b.WriteString(toolEntryStyle.Render(tc.name))
			b.WriteString(toolDurationStyle.Render(statusText))
			b.WriteString("\n")
			b.WriteString(renderToolCallBlock(tc))
		}
		b.WriteString("\n")
	}

	// Reasoning
	if reasoning != "" {
		b.WriteString(reasoningStyle.Render(reasoning))
		b.WriteString("\n")
	}

	// Content with reasoning-detection
	b.WriteString(formatContentWithReasoning(content, reasoning))
	b.WriteString("\n")
	return b.String()
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
		b.WriteString(reasoningStyle.Render(reasoningBlock))
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
		b.WriteString(reasoningStyle.Render(strings.Join(reasoningLines, "\n")))
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
			b.WriteString(reasoningStyle.Render(strings.Join(reasoningLines[:answerStart], "\n")))
			b.WriteString("\n")
			b.WriteString(renderMarkdown(strings.Join(reasoningLines[answerStart:], "\n")))
		}
	}
	return b.String()
}

// formatArgs formats tool arguments as a compact single-line string
func formatArgs(args map[string]any) string {
	if args == nil || len(args) == 0 {
		return ""
	}
	var parts []string
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		s = strings.ReplaceAll(s, "\n", " ")
		parts = append(parts, fmt.Sprintf("%s: %s", k, s))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts, ", ")
}

// truncateStr collapses newlines and truncates a string to maxLen characters
func truncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// renderToolCallBlock renders a tool call's args/result/error as compact lines
func renderToolCallBlock(tc toolCallEntry) string {
	var b strings.Builder
	maxWidth := getTerminalWidth() - 12 // padding for prefix
	if maxWidth < 40 {
		maxWidth = 40
	}
	hasResult := (tc.status == "done" && tc.result != "") || (tc.status == "error" && tc.result != "")
	if tc.args != "" {
		prefix := "    ┌ "
		if !hasResult {
			prefix = "    └ "
		}
		b.WriteString(toolArgsStyle.Render(prefix+"Args: "+truncateStr(tc.args, maxWidth)) + "\n")
	}
	if tc.status == "error" && tc.result != "" {
		b.WriteString("    └ " + toolErrorStyle.Render("Error: "+truncateStr(tc.result, maxWidth)) + "\n")
	} else if tc.status == "done" && tc.result != "" {
		b.WriteString(toolArgsStyle.Render("    └ Result: "+truncateStr(tc.result, maxWidth)) + "\n")
	}
	return b.String()
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

// getTerminalWidth returns a reasonable terminal width (fallback to 60)
func getTerminalWidth() int {
	// Try to get from environment or use default
	if w := os.Getenv("COLUMNS"); w != "" {
		var cw int
		if _, err := fmt.Sscanf(w, "%d", &cw); err == nil && cw > 0 {
			return cw
		}
	}
	return 60
}

// renderMarkdown renders markdown content using glamour
func renderMarkdown(content string) string {
	if content == "" {
		return ""
	}
	rendered, err := mdRenderer.Render(content)
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
