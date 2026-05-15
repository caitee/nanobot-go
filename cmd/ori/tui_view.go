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
		hasNewOutput:    m.hasNewTranscriptOutput,
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
		s.WriteString(waitingStyle.Render("new transcript output below"))
		s.WriteString("\n")
	}
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
	if suggestions := m.renderSlashCommandSuggestions(); suggestions != "" {
		s.WriteString(suggestions)
	}
	s.WriteString(textInputOut)
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
	if height < 5 {
		height = 5
	}
	m.viewport = viewport.New(width, height)
	m.renderer = transcriptRenderer{}
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
	if height < 5 {
		height = 5
	}
	if m.viewport.Width <= 0 || m.viewport.Height <= 0 {
		m.initTranscriptViewport(width, height)
		m.transcriptViewportText = m.renderTranscriptViewportContent()
		m.viewport.SetContent(m.transcriptViewportText)
		m.viewport.GotoBottom()
		m.clearNewTranscriptOutput()
		m.viewVersion++
		return true
	}
	wasAtBottom := m.viewport.AtBottom()
	widthChanged := m.viewport.Width != width
	if m.viewport.Width == width && m.viewport.Height == height {
		return false
	}
	m.viewport.Width = width
	m.viewport.Height = height
	if widthChanged {
		m.transcriptViewportText = m.renderTranscriptViewportContent()
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
	wasAtBottom := m.viewport.AtBottom()
	wasEmpty := strings.TrimSpace(m.transcriptViewportText) == ""
	content := m.renderTranscriptViewportContent()
	contentChanged := content != m.transcriptViewportText
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
	return m.renderer.renderTranscript(m.transcript, renderContext{
		width:  m.viewport.Width,
		focus:  m.focus,
		active: m.active,
		now:    time.Now(),
	})
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
