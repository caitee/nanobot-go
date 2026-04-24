package main

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	logo = `
    _   _
   | | | |
  / __/ __|
  \__ \__ \
  (   (   )
   |_| |_|
  AI Assistant
`

	mdRenderer, _ = glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(60),
	)
)

var (
	spinnerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).Bold(true)

	userPromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).Bold(true)

	assistantLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("130")).Bold(true)

	toolEntryStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	toolRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("75"))

	toolDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("76"))

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	toolIconStyle = lipgloss.NewStyle().
			Bold(true)

	toolArgsStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	toolDurationStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	contentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("white"))

	reasoningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")).
			Italic(true)

	streamingCursorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Bold(true)

	inputStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	borderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	waitingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
