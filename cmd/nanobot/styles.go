package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
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

	// Removed static renderers - now created dynamically based on terminal width

	reasoningStyleJSON = []byte(`{
  "document": {
    "block_prefix": "\n",
    "block_suffix": "\n",
    "color": "245",
    "margin": 2
  },
  "block_quote": {
    "indent": 1,
    "indent_token": "│ "
  },
  "paragraph": {},
  "list": {
    "level_indent": 2
  },
  "heading": {
    "block_suffix": "\n",
    "color": "245",
    "bold": true,
    "italic": true
  },
  "h1": {
    "prefix": " ",
    "suffix": " ",
    "color": "245",
    "bold": true
  },
  "h2": {
    "prefix": "## "
  },
  "h3": {
    "prefix": "### "
  },
  "h4": {
    "prefix": "#### "
  },
  "h5": {
    "prefix": "##### "
  },
  "h6": {
    "prefix": "###### ",
    "bold": false
  },
  "text": {},
  "strikethrough": {
    "crossed_out": true
  },
  "emph": {
    "italic": true
  },
  "strong": {
    "bold": true
  },
  "hr": {
    "color": "240",
    "format": "\n--------\n"
  },
  "item": {
    "block_prefix": "• "
  },
  "enumeration": {
    "block_prefix": ". "
  },
  "task": {
    "ticked": "[✓] ",
    "unticked": "[ ] "
  },
  "link": {
    "color": "245",
    "underline": true
  },
  "link_text": {
    "color": "245",
    "bold": true
  },
  "image": {
    "color": "245",
    "underline": true
  },
  "image_text": {
    "color": "243",
    "format": "Image: {{.text}} →"
  },
  "code": {
    "prefix": " ",
    "suffix": " ",
    "color": "245",
    "background_color": "236"
  },
  "code_block": {
    "color": "244",
    "margin": 2,
    "chroma": {
      "text": {
        "color": "#999999"
      },
      "error": {
        "color": "#999999",
        "background_color": "#F05B5B"
      },
      "comment": {
        "color": "#676767"
      },
      "comment_preproc": {
        "color": "#999999"
      },
      "keyword": {
        "color": "#999999"
      },
      "keyword_reserved": {
        "color": "#999999"
      },
      "keyword_namespace": {
        "color": "#999999"
      },
      "keyword_type": {
        "color": "#999999"
      },
      "operator": {
        "color": "#999999"
      },
      "punctuation": {
        "color": "#999999"
      },
      "name": {
        "color": "#999999"
      },
      "name_builtin": {
        "color": "#999999"
      },
      "name_tag": {
        "color": "#999999"
      },
      "name_attribute": {
        "color": "#999999"
      },
      "name_class": {
        "color": "#999999",
        "underline": true,
        "bold": true
      },
      "name_constant": {},
      "name_decorator": {
        "color": "#999999"
      },
      "name_exception": {},
      "name_function": {
        "color": "#999999"
      },
      "name_other": {},
      "literal": {},
      "literal_number": {
        "color": "#999999"
      },
      "literal_date": {},
      "literal_string": {
        "color": "#999999"
      },
      "literal_string_escape": {
        "color": "#999999"
      },
      "generic_deleted": {
        "color": "#999999"
      },
      "generic_emph": {
        "italic": true
      },
      "generic_inserted": {
        "color": "#999999"
      },
      "generic_strong": {
        "bold": true
      },
      "generic_subheading": {
        "color": "#777777"
      },
      "background": {
        "background_color": "#333333"
      }
    }
  },
  "table": {},
  "definition_list": {},
  "definition_term": {},
  "definition_description": {
    "block_prefix": "\n🠶 "
  },
  "html_block": {},
  "html_span": {}
}`)
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

// getTerminalWidth returns the actual terminal width, or a reasonable default.
func getTerminalWidth() int {
	// Try to get actual terminal size from stdout
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return width
	}
	// Fallback to COLUMNS env var
	if w := os.Getenv("COLUMNS"); w != "" {
		var cw int
		if _, err := fmt.Sscanf(w, "%d", &cw); err == nil && cw > 0 {
			return cw
		}
	}
	// Final fallback
	return 80
}
