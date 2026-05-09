package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"ori/internal/bus"
)

// version is the legacy ori version string surfaced by /status.
const version = "0.2.0-go"

// RegisterDefaultCommands installs the built-in /help /stop /restart
// /status /new /reasoning handlers on the given dispatcher.
func RegisterDefaultCommands(d *Dispatcher) {
	d.RegisterCommand("help", handleHelp)
	d.RegisterCommand("stop", handleStop)
	d.RegisterCommand("restart", handleRestart)
	d.RegisterCommand("status", handleStatus)
	d.RegisterCommand("new", handleNew)
	d.RegisterCommand("reasoning", handleReasoning)
}

func handleHelp(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
	lines := []string{
		"ori commands:",
		"/new — Start a new conversation",
		"/stop — Stop the current task",
		"/restart — Restart the bot",
		"/status — Show bot status",
		"/reasoning on|off — Toggle thinking mode",
		"/help — Show available commands",
	}
	return strings.Join(lines, "\n"), nil
}

func handleStop(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
	if d.AbortSession(inbound.SessionKey) {
		return "Stopped active task.", nil
	}
	return "No active task to stop.", nil
}

func handleRestart(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
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
	return "Restarting...", nil
}

func handleStatus(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
	sess := d.Session(inbound.SessionKey)
	lines := []string{
		fmt.Sprintf("ori v%s", version),
		fmt.Sprintf("Model: %s", d.Model().ID),
		"Status: running",
		fmt.Sprintf("Uptime: %s", time.Since(d.StartTime()).Round(time.Second)),
		fmt.Sprintf("Active turns: %d", d.ActiveCount()),
		fmt.Sprintf("Messages in session: %d", len(sess.Messages)),
	}
	return strings.Join(lines, "\n"), nil
}

func handleNew(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
	d.AbortSession(inbound.SessionKey)
	d.ResetSession(inbound.SessionKey)
	return "New session started.", nil
}

func handleReasoning(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
	args = strings.TrimSpace(strings.ToLower(args))
	switch args {
	case "on", "true", "1", "yes":
		d.SetReasoning(inbound.SessionKey, true)
		return "Thinking mode: enabled", nil
	case "off", "false", "0", "no":
		d.SetReasoning(inbound.SessionKey, false)
		return "Thinking mode: disabled", nil
	case "":
		next := !d.ReasoningEnabled(inbound.SessionKey)
		d.SetReasoning(inbound.SessionKey, next)
		if next {
			return "Thinking mode: enabled", nil
		}
		return "Thinking mode: disabled", nil
	}
	return "Usage: /reasoning on|off (or /reasoning to toggle)", nil
}
