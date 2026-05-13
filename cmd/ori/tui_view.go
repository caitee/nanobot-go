package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the full bubbletea view: completed rounds above, current round
// in progress, live streaming text with typewriter cursor, plus the footer
// status bar and input line.
func (m *interactiveModel) View() string {
	textInputOut := m.textInput.View()
	width := getTerminalWidth()
	key := viewCacheKey{
		version:            m.viewVersion,
		spinnerIdx:         m.spinnerIdx,
		width:              width,
		textInput:          textInputOut,
		active:             m.active,
		waiting:            m.waiting,
		quitting:           m.quitting,
		status:             m.status,
		displayedText:      m.displayedText,
		typewriterQueueLen: len(m.typewriterQueue),
	}
	// Cache hit: nothing relevant has changed since the last successful
	// render. Returning the stored string lets bubbletea's own diff detect a
	// no-op and skip writing to the terminal.
	if m.cachedViewOutput != "" && m.cachedViewKey == key {
		return m.cachedViewOutput
	}

	sep := borderStyle.Render(strings.Repeat("─", width))
	var s strings.Builder

	if m.quitting {
		s.WriteString(sep)
		s.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render("Goodbye!\n"))
		s.WriteString("\n")
		out := s.String()
		m.cachedViewKey = key
		m.cachedViewOutput = out
		return out
	}

	if m.active {
		// Header ("── / ✦ ori") was flushed above the TUI on agent_start,
		// so we don't redraw it here — only the live current round + streaming
		// text. This keeps the banner pinned above the first round instead of
		// re-emitting it between rounds and the final message.

		// Only render the current round in progress (completed rounds are flushed to View above)
		if m.currentRound != nil {
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
	s.WriteString(textInputOut)
	s.WriteString("\n")

	out := s.String()
	m.cachedViewKey = key
	m.cachedViewOutput = out
	return out
}

// renderRound renders one round (reasoning + tool calls).
func (m *interactiveModel) renderRound(round thinkingRound, isLive bool) string {
	return renderRoundContent(round, isLive)
}

// renderLiveContent renders live streaming content. Instead of falling back
// to raw text while markdown is mid-construction (which causes visible flicker
// between plain and formatted rendering), we temporarily close any open
// markdown constructs at the tail so glamour always sees syntactically
// complete input.
//
// The render result is memoised on the model: View() runs on every tea.Msg
// (spinner / typewriter ticks included), but the rendered output only needs to
// change when displayedText or terminal width changes.
func (m *interactiveModel) renderLiveContent(text string) string {
	if text == "" {
		return ""
	}
	width := getTerminalWidth()
	if m.lastRenderedText == text && m.lastRenderedWidth == width && m.lastRenderedOutput != "" {
		return m.lastRenderedOutput
	}
	renderer := getMarkdownRenderer()
	if renderer == nil {
		m.lastRenderedText = text
		m.lastRenderedWidth = width
		m.lastRenderedOutput = text
		return text
	}
	processed := preprocessMath(closeOpenMarkdown(text))
	rendered, err := renderer.Render(processed)
	if err != nil {
		m.lastRenderedText = text
		m.lastRenderedWidth = width
		m.lastRenderedOutput = text
		return text
	}
	out := strings.TrimSuffix(rendered, "\n")
	m.lastRenderedText = text
	m.lastRenderedWidth = width
	m.lastRenderedOutput = out
	return out
}

// closeOpenMarkdown appends temporary closers for unclosed markdown constructs
// at the tail of text so the parser sees well-formed input during streaming.
// The appended bytes are discarded on the next frame when the real closers
// arrive, so the user only ever sees complete formatting transition smoothly
// instead of toggling between raw text and styled output.
func closeOpenMarkdown(text string) string {
	if text == "" {
		return text
	}
	out := text

	// Fenced code block: ``` count must be even.
	if strings.Count(out, "```")%2 != 0 {
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "```"
	}

	// Block math $$ ... $$: pair them off, close if dangling.
	temp := out
	dangling := false
	for {
		idx := strings.Index(temp, "$$")
		if idx < 0 {
			break
		}
		end := strings.Index(temp[idx+2:], "$$")
		if end < 0 {
			dangling = true
			break
		}
		temp = temp[idx+2+end+2:]
	}
	if dangling {
		out += "$$"
	}

	// Inline $ ... $: count $ outside of $$ pairs.
	stripped := out
	for {
		idx := strings.Index(stripped, "$$")
		if idx < 0 {
			break
		}
		end := strings.Index(stripped[idx+2:], "$$")
		if end < 0 {
			break
		}
		stripped = stripped[:idx] + stripped[idx+2+end+2:]
	}
	if strings.Count(stripped, "$")%2 != 0 {
		out += "$"
	}

	// Inline code: only consider the last line to avoid touching fenced blocks.
	lastLineStart := strings.LastIndex(out, "\n") + 1
	lastLine := out[lastLineStart:]
	if !strings.HasPrefix(strings.TrimSpace(lastLine), "```") {
		if strings.Count(lastLine, "`")%2 != 0 {
			out += "`"
		}
	}

	// Link/image syntax on the last line: [text, [text](, ![text, ![text](.
	// Only close the most recent unclosed construct; nested cases are rare in
	// a single streamed chunk.
	lastLine = out[strings.LastIndex(out, "\n")+1:]
	if open := lastUnclosedLink(lastLine); open != "" {
		out += open
	}

	return out
}

// lastUnclosedLink returns the closer needed if the last line has a dangling
// link/image opener, or "" otherwise.
func lastUnclosedLink(line string) string {
	if closeIdx := strings.LastIndex(line, "]("); closeIdx >= 0 {
		rest := line[closeIdx+2:]
		if !strings.Contains(rest, ")") && strings.LastIndex(line[:closeIdx], "[") >= 0 {
			return ")"
		}
	}

	// Scan for the last '[' that hasn't been matched by ']'.
	depth := 0
	lastOpen := -1
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '[':
			if depth == 0 {
				lastOpen = i
			}
			depth++
		case ']':
			if depth > 0 {
				depth--
				if depth == 0 {
					lastOpen = -1
				}
			}
		}
	}
	if lastOpen < 0 {
		if depth == 0 {
			return ""
		}
		// Unmatched ']' shouldn't happen, but be safe.
		return ""
	}
	// Just "[text..." with no closer yet.
	return "]()"
}
