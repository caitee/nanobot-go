package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appcore "ori/internal/app"
	"ori/internal/bus"
	"ori/internal/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Interact with the agent",
	Run:   runAgent,
}

var (
	agentMessageFlag   string
	agentSessionFlag   string
	agentWorkspaceFlag string
	agentConfigFlag    string
	agentMarkdownFlag  bool
	agentLogsFlag      bool
)

func init() {
	agentCmd.Flags().StringVarP(&agentMessageFlag, "message", "m", "", "Message to send to the agent")
	agentCmd.Flags().StringVarP(&agentSessionFlag, "session", "s", "", "Session ID (default: generate unique session per run)")
	agentCmd.Flags().StringVarP(&agentWorkspaceFlag, "workspace", "w", "", "Workspace directory")
	agentCmd.Flags().StringVarP(&agentConfigFlag, "config", "c", "", "Config file path")
	agentCmd.Flags().BoolVarP(&agentMarkdownFlag, "markdown", "", true, "Render assistant output as Markdown")
	agentCmd.Flags().BoolVarP(&agentLogsFlag, "logs", "", false, "Show ori runtime logs during chat")
}

// generateSessionKey creates a unique session key for this invocation.
func generateSessionKey() string {
	return fmt.Sprintf("cli:session-%d", time.Now().UnixNano())
}

// resolveSessionKey returns the session key to use: if the user provided
// an explicit -s value, use it; otherwise generate a fresh session.
func resolveSessionKey(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return generateSessionKey()
}

func runAgent(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load(agentConfigFlag)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Override workspace if specified
	workspace := agentWorkspaceFlag
	if workspace == "" {
		workspace = cfg.Agents.Workspace
	}
	if workspace == "" {
		homeDir, _ := os.UserHomeDir()
		workspace = filepath.Join(homeDir, ".ori", "workspace")
	}
	cfg.Agents.Workspace = workspace

	app, err := appcore.New(cfg)
	if err != nil {
		slog.Error("failed to create app", "error", err)
		os.Exit(1)
	}
	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start app", "error", err)
		os.Exit(1)
	}
	defer app.Stop()

	sessionKey := resolveSessionKey(agentSessionFlag)
	chatID := sessionKey
	if idx := strings.Index(sessionKey, ":"); idx != -1 {
		chatID = sessionKey[idx+1:]
	}

	if agentMessageFlag != "" {
		runAgentSingle(ctx, app, sessionKey, chatID)
		return
	}
	runAgentInteractive(ctx, app, cfg, sessionKey, chatID)
}

// runAgentSingle sends one prompt through the dispatcher, waits for the
// outbound response, and prints it to stdout.
func runAgentSingle(ctx context.Context, app *appcore.App, sessionKey, chatID string) {
	outbound := app.Bus.ConsumeOutbound()

	app.Bus.PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     chatID,
		Content:    agentMessageFlag,
		SessionKey: sessionKey,
	})

	select {
	case msg, ok := <-outbound:
		if !ok {
			return
		}
		fmt.Println()
		fmt.Println(assistantLabelStyle.Render("ori:"))
		fmt.Println()
		if msg.Reasoning != "" {
			fmt.Println(renderReasoningMarkdown(msg.Reasoning))
		}
		fmt.Print(renderMarkdown(msg.Content))
	case <-ctx.Done():
	}
}

func runAgentInteractive(ctx context.Context, app *appcore.App, cfg *config.Config, sessionKey, chatID string) {
	fmt.Print(renderBanner(cfg, sessionKey))

	model := newInteractiveModel(app.Dispatcher, app.Bus, sessionKey, chatID)
	p := tea.NewProgram(model, tea.WithoutSignals())
	model.SetProgram(p)
	if _, err := p.Run(); err != nil {
		slog.Error("interactive mode error", "error", err)
	}
}

// renderBanner composes the startup banner: the ASCII star straddles the
// panel's top-right corner (two rows above the top border, the middle row
// sitting in a gap cut out of the border, two rows landing in the panel's
// first content rows). The panel has a fixed width so it doesn't sprawl on
// wide terminals, with a small margin between the star and the border so
// neither crowds the other.
func renderBanner(cfg *config.Config, sessionKey string) string {
	_ = sessionKey // session is intentionally omitted from the banner

	const (
		desiredW  = 68
		starW     = 7
		starGap   = 2 // blank cells between star and border
		starShift = 6 // shift star from right edge by this many cells
	)

	panelW := desiredW
	if tw := getTerminalWidth(); tw > 0 && tw < panelW {
		panelW = tw
	}
	if panelW < 40 {
		panelW = 40
	}

	fg := func(code, s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(code)).Render(s)
	}
	fgBold := func(code, s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(code)).Bold(true).Render(s)
	}
	fgItalic := func(code, s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(code)).Italic(true).Render(s)
	}

	const (
		borderC  = "240"
		starC    = "86"
		titleC   = "86"
		labelC   = "245"
		valueC   = "252"
		taglineC = "245"
		sepC     = "240"
		modeC    = "245"
	)

	provider := firstNonEmpty(cfg.Agents.Provider, "-")
	model := firstNonEmpty(cfg.Agents.Model, "-")
	workspace := firstNonEmpty(shortenHome(cfg.Agents.Workspace), "-")

	title := fgBold(titleC, "ori") + "  " + fgItalic(taglineC, "a small craft in your orbit.")
	info := fg(labelC, "provider ") + fgBold(valueC, provider) +
		fg(sepC, "  ·  ") +
		fg(labelC, "model ") + fgBold(valueC, model)
	ws := fg(labelC, "workspace ") + fgBold(valueC, workspace)
	contents := []string{title, "", info, ws}

	starRows := []string{
		"   .   ",
		" ./|\\, ",
		"<-=O=->",
		" '\\|/" + "`" + " ",
		"   '   ",
	}

	// starCol is the 1-based column of the star's first cell within the panel.
	// Reserve gapRight blanks plus 1 cell for the right border to its right.
	starCol := panelW - starShift - starW - 1
	if starCol < 1 {
		starCol = 1
	}

	var lines []string

	for _, row := range starRows[:2] {
		lines = append(lines, strings.Repeat(" ", starCol)+fg(starC, row))
	}

	leftDashes := starCol - starGap - 1
	if leftDashes < 0 {
		leftDashes = 0
	}
	rightDashes := panelW - starCol - starW - starGap - 1
	if rightDashes < 0 {
		rightDashes = 0
	}
	topBorder := fg(borderC, "╭"+strings.Repeat("─", leftDashes)) +
		strings.Repeat(" ", starGap) +
		fg(starC, starRows[2]) +
		strings.Repeat(" ", starGap) +
		fg(borderC, strings.Repeat("─", rightDashes)) +
		fg(borderC, "╮")
	lines = append(lines, topBorder)

	innerW := panelW - 2
	leftPad := "  "
	for i, c := range contents {
		body := leftPad + c
		bodyW := lipgloss.Width(body)
		var innerLine string
		if i < 2 {
			// Star's lower half sits in the right portion of these rows.
			targetCol := starCol - 1 // inner region starts at col 1, so subtract the left border
			padBefore := targetCol - bodyW
			if padBefore < 0 {
				padBefore = 0
			}
			afterEnd := targetCol + starW
			afterPad := innerW - afterEnd
			if afterPad < 0 {
				afterPad = 0
			}
			innerLine = body + strings.Repeat(" ", padBefore) +
				fg(starC, starRows[3+i]) +
				strings.Repeat(" ", afterPad)
		} else {
			pad := innerW - bodyW - len(leftPad)
			if pad < 0 {
				pad = 0
			}
			innerLine = body + strings.Repeat(" ", pad) + leftPad
		}
		lines = append(lines, fg(borderC, "│")+innerLine+fg(borderC, "│"))
	}

	lines = append(lines, fg(borderC, "╰"+strings.Repeat("─", panelW-2)+"╯"))

	return strings.Join(lines, "\n") + "\n\n" + fg(modeC, " Interactive mode") + "\n\n"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func shortenHome(p string) string {
	if p == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}
