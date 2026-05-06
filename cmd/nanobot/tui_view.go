package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full bubbletea view: completed rounds above, current round
// in progress, live streaming text with typewriter cursor, plus the footer
// status bar and input line.
func (m *interactiveModel) View() string {
	sep := borderStyle.Render(strings.Repeat("─", getTerminalWidth()))
	var s strings.Builder

	if m.quitting {
		s.WriteString(sep)
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render("Goodbye!\n"))
		s.WriteString("\n")
		return s.String()
	}

	if m.active {
		s.WriteString(sep)
		s.WriteString("\n")
		s.WriteString(spinnerStyle.Render("✦"))
		s.WriteString(" ")
		s.WriteString(assistantLabelStyle.Render("nanobot"))
		s.WriteString("\n")

		for i, round := range m.rounds {
			if i > 0 {
				s.WriteString("\n")
			}
			s.WriteString(m.renderRound(round, false))
		}

		if m.currentRound != nil {
			if len(m.rounds) > 0 {
				s.WriteString("\n")
			}
			s.WriteString(m.renderRound(*m.currentRound, true))
		}

		if m.displayedText != "" {
			renderedText := m.renderLiveContent(m.displayedText)
			s.WriteString(renderedText)
			if len(m.typewriterQueue) > 0 {
				s.WriteString("▍")
			}
			s.WriteString("\n")
		}
	}

	s.WriteString("\n")
	if m.active && m.status != "" && m.status != "done" {
		s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
		s.WriteString(" ")
		s.WriteString(toolRunningStyle.Render(m.status))
		s.WriteString("\n")
	} else if m.waiting && !m.active {
		s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
		s.WriteString(" ")
		s.WriteString(waitingStyle.Render("waiting"))
		s.WriteString("\n")
	}
	s.WriteString(sep)
	s.WriteString("\n")
	s.WriteString(m.textInput.View())
	s.WriteString("\n")

	return s.String()
}

// renderRound renders one round (reasoning + tool calls).
func (m *interactiveModel) renderRound(round thinkingRound, isLive bool) string {
	var s strings.Builder

	if round.reasoning != "" {
		renderedReasoning := renderReasoningMarkdown(round.reasoning)
		const maxReasoningLines = 5
		lines := strings.Split(renderedReasoning, "\n")
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(lines) > maxReasoningLines {
			hidden := len(lines) - maxReasoningLines
			s.WriteString(reasoningStyle.Render(fmt.Sprintf("  ⋮ (%d more lines)", hidden)))
			s.WriteString("\n")
			s.WriteString(strings.Join(lines[len(lines)-maxReasoningLines:], "\n"))
		} else {
			s.WriteString(strings.Join(lines, "\n"))
		}
		s.WriteString("\n")
	}

	if len(round.toolCalls) > 0 {
		for _, tc := range round.toolCalls {
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
			case "running":
				if isLive {
					icon = spinnerFrames[m.spinnerIdx]
				} else {
					icon = "○"
				}
				statusText = " running..."
				iconStyle = toolRunningStyle
			default:
				icon = "○"
				statusText = " pending"
				iconStyle = toolEntryStyle
			}
			s.WriteString("  ")
			s.WriteString(iconStyle.Render(icon) + " ")
			s.WriteString(toolEntryStyle.Render(tc.name))
			s.WriteString(toolDurationStyle.Render(statusText))
			s.WriteString("\n")
			maxWidth := getTerminalWidth() - 12
			if maxWidth < 40 {
				maxWidth = 40
			}
			hasResult := (tc.status == "done" && tc.result != "") || (tc.status == "error" && tc.result != "")
			if tc.args != "" {
				prefix := "    ┌ "
				if !hasResult {
					prefix = "    └ "
				}
				s.WriteString(toolArgsStyle.Render(prefix + "Args: " + truncateStr(tc.args, maxWidth)))
				s.WriteString("\n")
			}
			if tc.status == "error" && tc.result != "" {
				s.WriteString("    └ " + toolErrorStyle.Render("Error: "+truncateStr(tc.result, maxWidth)))
				s.WriteString("\n")
			} else if tc.status == "done" && tc.result != "" {
				s.WriteString(toolArgsStyle.Render("    └ Result: " + truncateStr(tc.result, maxWidth)))
				s.WriteString("\n")
			}
		}
	}

	return s.String()
}

// renderLiveContent renders live streaming content, falling back to raw text
// if the markdown syntax is mid-construction.
func (m *interactiveModel) renderLiveContent(text string) string {
	if text == "" {
		return ""
	}
	if isIncompleteMarkdown(text) {
		return text
	}
	processed := preprocessMath(text)
	renderer := getMarkdownRenderer()
	if renderer == nil {
		return text
	}
	rendered, err := renderer.Render(processed)
	if err != nil {
		return text
	}
	return strings.TrimSuffix(rendered, "\n")
}

// isIncompleteMarkdown reports whether the text ends with an unclosed fence,
// $$ pair, inline $, or trailing backtick. Prevents glamour from rendering
// partial markdown in weird ways while the stream is in flight.
func isIncompleteMarkdown(text string) bool {
	if strings.Count(text, "```")%2 != 0 {
		return true
	}
	temp := text
	for {
		idx := strings.Index(temp, "$$")
		if idx < 0 {
			break
		}
		end := strings.Index(temp[idx+2:], "$$")
		if end < 0 {
			return true
		}
		temp = temp[:idx] + temp[idx+2+end+2:]
	}
	count := 0
	for i := 0; i < len(temp); i++ {
		if temp[i] == '$' {
			count++
		}
	}
	if count%2 != 0 {
		return true
	}
	if idx := strings.LastIndex(text, "\n"); idx >= 0 {
		lastLine := text[idx+1:]
		if strings.Count(lastLine, "`")%2 != 0 {
			return true
		}
	} else {
		if strings.Count(text, "`")%2 != 0 {
			return true
		}
	}
	return false
}
