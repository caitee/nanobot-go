package main

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// View renders the full bubbletea view: transcript viewport, overlay panel,
// status bar, suggestions, and input line.
func (m *interactiveModel) View() string {
	textInputOut := m.textInput.View()
	width := getTerminalWidth()
	padding := transcriptHorizontalPaddingFor(width)
	viewportOut := m.viewport.View()
	key := viewCacheKey{
		version:         m.viewVersion,
		spinnerIdx:      m.spinnerIdx,
		width:           width,
		textInput:       textInputOut,
		active:          m.active,
		waiting:         m.waiting,
		quitting:        m.quitting,
		status:          m.status,
		viewportContent: viewportOut,
		viewportWidth:   m.viewport.Width,
		viewportHeight:  m.viewport.Height,
		viewportYOffset: m.viewport.YOffset,
		focus:           m.focus,
		viewMode:        normalizeTranscriptViewMode(m.viewMode),
		hasNewOutput:    m.hasNewTranscriptOutput,
	}
	// Cache hit: nothing relevant has changed since the last successful
	// render. Returning the stored string lets bubbletea's own diff detect a
	// no-op and skip writing to the terminal.
	if m.cachedViewOutput != "" && m.cachedViewKey == key {
		return m.cachedViewOutput
	}

	sep := paddedViewLine(borderStyle.Render(strings.Repeat("─", max(1, width-padding*2))), padding, width)
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

	if view := strings.TrimRight(viewportOut, "\n"); strings.TrimSpace(view) != "" {
		s.WriteString(view)
		s.WriteString("\n")
	}

	if m.panel != nil {
		if strings.TrimSpace(viewportOut) != "" {
			s.WriteString("\n")
		}
		s.WriteString(m.renderManagementPanel())
	}

	s.WriteString("\n")
	if m.hasNewTranscriptOutput {
		s.WriteString(paddedViewLine(waitingStyle.Render("new transcript output below"), padding, width))
		s.WriteString("\n")
	}
	if m.active && m.status != "" && m.status != "done" {
		s.WriteString(strings.Repeat(" ", padding))
		s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
		s.WriteString(" ")
		s.WriteString(toolRunningStyle.Render(m.status))
		s.WriteString("\n")
	} else if m.waiting && !m.active {
		s.WriteString(strings.Repeat(" ", padding))
		s.WriteString(spinnerStyle.Render(spinnerFrames[m.spinnerIdx]))
		s.WriteString(" ")
		s.WriteString(waitingStyle.Render("waiting"))
		s.WriteString("\n")
	}
	s.WriteString(sep)
	s.WriteString("\n")
	if suggestions := m.renderSlashCommandSuggestions(); suggestions != "" {
		s.WriteString(padRenderedLines(strings.TrimRight(suggestions, "\n"), padding, width))
		s.WriteString("\n")
	}
	s.WriteString(paddedViewLine(textInputOut, padding, width))
	s.WriteString("\n")

	out := s.String()
	m.cachedViewKey = key
	m.cachedViewOutput = out
	return out
}

func transcriptViewportHeight() int {
	return transcriptViewportHeightFor(getTerminalHeight())
}

func transcriptViewportHeightFor(terminalHeight int) int {
	h := terminalHeight - 4
	if h < 1 {
		return 1
	}
	return h
}

func paddedViewLine(line string, padding, width int) string {
	if padding <= 0 {
		return fitLine(line, width)
	}
	return fitLine(strings.Repeat(" ", padding)+line, width)
}

func (m *interactiveModel) initTranscriptViewport(width, height int) {
	if width <= 0 {
		width = getTerminalWidth()
	}
	if height <= 0 {
		height = transcriptViewportHeight()
	}
	height = normalizeTranscriptViewportMaxHeight(height)
	m.viewportMaxHeight = height
	m.viewport = viewport.New(width, height)
	m.renderer = transcriptRenderer{}
	m.viewMode = normalizeTranscriptViewMode(m.viewMode)
	if m.focus != focusTranscript && m.focus != focusOverlay {
		m.focus = focusInput
	}
}

func (m *interactiveModel) resizeTranscriptViewport(width, height int) bool {
	if width <= 0 {
		width = getTerminalWidth()
	}
	if height <= 0 {
		height = transcriptViewportHeight()
	}
	height = normalizeTranscriptViewportMaxHeight(height)
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		m.viewportMaxHeight = height
		m.renderer = transcriptRenderer{}
		m.transcriptViewportText = m.renderTranscriptViewportContentForWidth(width)
		m.viewport = viewport.New(width, transcriptViewportHeightForContent(m.transcriptViewportText, height))
		m.viewport.SetContent(m.transcriptViewportText)
		m.viewport.GotoBottom()
		m.clearNewTranscriptOutput()
		m.viewVersion++
		return true
	}
	wasAtBottom := m.viewport.AtBottom()
	widthChanged := m.viewport.Width != width
	maxHeightChanged := m.viewportMaxHeight != height
	content := m.transcriptViewportText
	if widthChanged {
		content = m.renderTranscriptViewportContentForWidth(width)
	}
	nextHeight := transcriptViewportHeightForContent(content, height)
	if m.viewport.Width == width && m.viewport.Height == nextHeight && !maxHeightChanged {
		return false
	}
	m.viewportMaxHeight = height
	m.viewport.Width = width
	m.viewport.Height = nextHeight
	if widthChanged {
		m.transcriptViewportText = content
	}
	m.viewport.SetContent(m.transcriptViewportText)
	m.viewVersion++
	if wasAtBottom {
		m.viewport.GotoBottom()
		m.clearNewTranscriptOutput()
		return true
	}
	if m.viewport.AtBottom() {
		m.clearNewTranscriptOutput()
	}
	return true
}

func (m *interactiveModel) refreshTranscriptViewport() {
	m.refreshTranscriptViewportWithNewOutput(true)
}

func (m *interactiveModel) refreshTranscriptViewportForRepaint() {
	m.refreshTranscriptViewportWithNewOutput(false)
}

func (m *interactiveModel) refreshTranscriptViewportWithNewOutput(markNewOutput bool) {
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		m.initTranscriptViewport(getTerminalWidth(), transcriptViewportHeight())
	}
	if m.viewportMaxHeight <= 0 {
		m.viewportMaxHeight = normalizeTranscriptViewportMaxHeight(m.viewport.Height)
	}
	wasAtBottom := m.viewport.AtBottom()
	wasEmpty := strings.TrimSpace(m.transcriptViewportText) == ""
	content := m.renderTranscriptViewportContent()
	contentChanged := content != m.transcriptViewportText
	nextHeight := transcriptViewportHeightForContent(content, m.viewportMaxHeight)
	heightChanged := m.viewport.Height != nextHeight
	if heightChanged {
		m.viewport.Height = nextHeight
		m.viewVersion++
	}
	if contentChanged {
		m.transcriptViewportText = content
		m.viewVersion++
	}
	m.viewport.SetContent(content)
	if wasAtBottom || wasEmpty {
		m.viewport.GotoBottom()
		m.clearNewTranscriptOutput()
		return
	}
	if m.viewport.AtBottom() {
		m.clearNewTranscriptOutput()
		return
	}
	if contentChanged && markNewOutput {
		m.markNewTranscriptOutput()
	}
}

func (m *interactiveModel) renderTranscriptViewportContent() string {
	return m.renderTranscriptViewportContentForWidth(m.viewport.Width)
}

func (m *interactiveModel) renderTranscriptViewportContentForWidth(width int) string {
	return m.renderer.renderTranscript(m.transcript, renderContext{
		width:    width,
		focus:    m.focus,
		active:   m.active,
		now:      time.Now(),
		viewMode: normalizeTranscriptViewMode(m.viewMode),
	})
}

func normalizeTranscriptViewportMaxHeight(height int) int {
	if height < 1 {
		return 1
	}
	return height
}

func transcriptViewportHeightForContent(content string, maxHeight int) int {
	maxHeight = normalizeTranscriptViewportMaxHeight(maxHeight)
	height := transcriptContentHeight(content)
	if height < 1 {
		height = 1
	}
	return min(height, maxHeight)
}

func transcriptContentHeight(content string) int {
	content = strings.TrimRight(content, "\n")
	if strings.TrimSpace(content) == "" {
		return 1
	}
	return strings.Count(content, "\n") + 1
}

func (m *interactiveModel) markNewTranscriptOutput() {
	if m.hasNewTranscriptOutput {
		return
	}
	m.hasNewTranscriptOutput = true
	m.viewVersion++
}

func (m *interactiveModel) clearNewTranscriptOutput() {
	if !m.hasNewTranscriptOutput {
		return
	}
	m.hasNewTranscriptOutput = false
	m.viewVersion++
}
