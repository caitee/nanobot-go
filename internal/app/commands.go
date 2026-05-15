package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"ori/internal/bus"
	"ori/internal/skills"
)

// version is the legacy ori version string surfaced by /status.
const version = "0.2.0-go"

// CommandScope describes where a slash command is meant to execute.
type CommandScope string

const (
	CommandScopeApp    CommandScope = "app"
	CommandScopeTUI    CommandScope = "tui"
	CommandScopePrompt CommandScope = "prompt"
)

// Command describes one slash command and its metadata.
type Command struct {
	Name         string
	Aliases      []string
	Description  string
	ArgumentHint string
	Scope        CommandScope
	Handler      SlashCommandHandler
}

// CommandResult is the structured outcome of running a slash command.
type CommandResult struct {
	Text              string
	Markdown          string
	Status            string
	ResetSession      bool
	ClearViewport     bool
	PromptReplacement string
	UIRequest         string
}

// SlashCommandHandler is the native metadata-aware command handler.
type SlashCommandHandler func(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error)

// RegisterDefaultCommands installs the built-in slash commands.
func RegisterDefaultCommands(d *Dispatcher) {
	d.RegisterSlashCommand(Command{Name: "help", Description: "Show available commands", Scope: CommandScopeApp, Handler: handleHelp})
	d.RegisterSlashCommand(Command{Name: "stop", Description: "Stop the current task", Scope: CommandScopeApp, Handler: handleStop})
	d.RegisterSlashCommand(Command{Name: "restart", Description: "Restart the bot", Scope: CommandScopeApp, Handler: handleRestart})
	d.RegisterSlashCommand(Command{Name: "status", Description: "Show bot status", Scope: CommandScopeApp, Handler: handleStatus})
	d.RegisterSlashCommand(Command{Name: "clear", Description: "Clear the current conversation", Scope: CommandScopeApp, Handler: handleNew})
	d.RegisterSlashCommand(Command{Name: "new", Description: "Start a new conversation", Scope: CommandScopeApp, Handler: handleNew})
	d.RegisterSlashCommand(Command{Name: "mcp", Description: "Manage MCP servers", Scope: CommandScopeApp, Handler: handleMCP})
	d.RegisterSlashCommand(Command{Name: "skills", Description: "Manage skills", Scope: CommandScopeApp, Handler: handleSkills})
	d.RegisterSlashCommand(Command{Name: "config", Description: "Manage common settings", Scope: CommandScopeApp, Handler: handleConfig})
	d.RegisterSlashCommand(Command{Name: "sessions", Description: "Resume a previous session", Scope: CommandScopeApp, Handler: handleSessions})
	d.RegisterSlashCommand(Command{Name: "reasoning", Description: "Toggle thinking mode", ArgumentHint: "on|off", Scope: CommandScopeApp, Handler: handleReasoning})
	d.RegisterSlashCommand(Command{Name: "quit", Aliases: []string{"exit"}, Description: "Quit interactive mode", Scope: CommandScopeTUI, Handler: handleTUIOnly})
}

func handleHelp(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	commands := d.ListCommands()
	lines := []string{"ori commands:"}
	for _, cmd := range commands {
		if cmd.Scope == CommandScopePrompt {
			continue
		}
		name := "/" + cmd.Name
		if cmd.ArgumentHint != "" {
			name += " " + cmd.ArgumentHint
		}
		lines = append(lines, fmt.Sprintf("%s — %s", name, cmd.Description))
	}
	return &CommandResult{Text: strings.Join(lines, "\n")}, nil
}

func handleStop(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	if d.AbortSession(inbound.SessionKey) {
		return &CommandResult{Text: "Stopped active task.", Status: "stopped"}, nil
	}
	return &CommandResult{Text: "No active task to stop.", Status: "idle"}, nil
}

func handleRestart(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		cmd := exec.Command(os.Args[0], os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			slog.Error("restart failed", "error", err)
		}
		os.Exit(0)
	}()
	return &CommandResult{Text: "Restarting...", Status: "restarting"}, nil
}

func handleStatus(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	sess := d.Session(inbound.SessionKey)
	lines := []string{
		fmt.Sprintf("ori v%s", version),
		fmt.Sprintf("Model: %s", d.Model().ID),
		"Status: running",
		fmt.Sprintf("Uptime: %s", time.Since(d.StartTime()).Round(time.Second)),
		fmt.Sprintf("Active turns: %d", d.ActiveCount()),
		fmt.Sprintf("Messages in session: %d", len(sess.Messages)),
	}
	return &CommandResult{Text: strings.Join(lines, "\n"), Status: "running"}, nil
}

func handleNew(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	d.AbortSession(inbound.SessionKey)
	d.ResetSession(inbound.SessionKey)
	return &CommandResult{Text: "New session started.", Status: "ready", ResetSession: true, ClearViewport: true}, nil
}

func handleSkills(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	if d.management != nil {
		return &CommandResult{Text: d.management.FormatSkillStatus(), UIRequest: UIRequestSkills}, nil
	}
	if d.skillLoader == nil {
		return &CommandResult{Text: "No skills found.", UIRequest: UIRequestSkills}, nil
	}
	return &CommandResult{Text: skills.FormatSkillList(d.skillLoader.ListSkills(false)), UIRequest: UIRequestSkills}, nil
}

func handleMCP(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	text := "No MCP servers configured."
	if d.management != nil {
		text = d.management.FormatMCPStatus()
	}
	return &CommandResult{Text: text, UIRequest: UIRequestMCP}, nil
}

func handleConfig(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	text := "No configurable fields available."
	if d.management != nil {
		text = d.management.FormatConfigStatus()
	}
	return &CommandResult{Text: text, UIRequest: UIRequestConfig}, nil
}

func handleSessions(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	text := "No sessions found."
	if d.management != nil {
		text = d.management.FormatSessionStatus(inbound.SessionKey)
	} else if d.sessionStore != nil {
		text = formatSessionStatus(sessionViews(d.sessionStore.List(), inbound.SessionKey))
	}
	return &CommandResult{Text: text, UIRequest: UIRequestSessions}, nil
}

func handleTUIOnly(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	return &CommandResult{Text: "This command is only available in interactive mode."}, nil
}

func handleReasoning(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (*CommandResult, error) {
	args = strings.TrimSpace(strings.ToLower(args))
	switch args {
	case "on", "true", "1", "yes":
		d.SetReasoning(inbound.SessionKey, true)
		return &CommandResult{Text: "Thinking mode: enabled", Status: "reasoning enabled"}, nil
	case "off", "false", "0", "no":
		d.SetReasoning(inbound.SessionKey, false)
		return &CommandResult{Text: "Thinking mode: disabled", Status: "reasoning disabled"}, nil
	case "":
		next := !d.ReasoningEnabled(inbound.SessionKey)
		d.SetReasoning(inbound.SessionKey, next)
		if next {
			return &CommandResult{Text: "Thinking mode: enabled", Status: "reasoning enabled"}, nil
		}
		return &CommandResult{Text: "Thinking mode: disabled", Status: "reasoning disabled"}, nil
	}
	return &CommandResult{Text: "Usage: /reasoning on|off (or /reasoning to toggle)"}, nil
}

func skillCommands(loader *skills.SkillLoader) []Command {
	if loader == nil {
		return nil
	}
	items := loader.ListSkills(false)
	out := make([]Command, 0, len(items))
	for _, item := range items {
		if item == nil || item.Name == "" {
			continue
		}
		out = append(out, Command{
			Name:        "skill:" + item.Name,
			Description: item.Description,
			Scope:       CommandScopePrompt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
