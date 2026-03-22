package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"nanobot-go/internal/bus"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"
)

const version = "0.1.0-go"

type AgentLoop struct {
	bus                bus.MessageBus
	sessionStore       session.SessionStore
	toolRegistry       tools.ToolRegistry
	provider           providers.LLMProvider
	maxIterations      int
	commands           map[string]CommandHandler
	mu                 sync.Mutex
	running            bool
	startTime          time.Time
	sessionCancelFuncs map[string]context.CancelFunc
}

type CommandHandler func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error)

func NewAgentLoop(bus bus.MessageBus, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) *AgentLoop {
	al := &AgentLoop{
		bus:                bus,
		sessionStore:       sessionStore,
		toolRegistry:       toolRegistry,
		provider:           provider,
		maxIterations:      maxIterations,
		commands:           make(map[string]CommandHandler),
		sessionCancelFuncs: make(map[string]context.CancelFunc),
	}
	al.registerDefaultCommands()
	return al
}

func (al *AgentLoop) registerDefaultCommands() {
	al.commands["help"] = al.handleHelp
	al.commands["stop"] = al.handleStop
	al.commands["restart"] = al.handleRestart
	al.commands["status"] = al.handleStatus
	al.commands["new"] = al.handleNew
}

func (al *AgentLoop) handleHelp(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	lines := []string{
		"nanobot commands:",
		"/new — Start a new conversation",
		"/stop — Stop the current task",
		"/restart — Restart the bot",
		"/status — Show bot status",
		"/help — Show available commands",
	}
	return strings.Join(lines, "\n"), nil
}

func (al *AgentLoop) handleStop(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	// Cancel any active processing for this session
	if cancel, ok := al.sessionCancelFuncs[inbound.SessionKey]; ok {
		cancel()
		delete(al.sessionCancelFuncs, inbound.SessionKey)
		return "Stopped active task.", nil
	}
	return "No active task to stop.", nil
}

func (al *AgentLoop) handleRestart(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	// Send restart message first
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

func (al *AgentLoop) handleStatus(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	al.mu.Lock()
	running := al.running
	elapsed := time.Since(al.startTime)
	al.mu.Unlock()

	model := al.provider.GetDefaultModel()
	status := "running"
	if !running {
		status = "stopped"
	}

	// Get session info
	sess := al.sessionStore.GetOrCreate(inbound.SessionKey)
	msgCount := len(sess.Messages)

	lines := []string{
		fmt.Sprintf("nanobot v%s", version),
		fmt.Sprintf("Model: %s", model),
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Uptime: %s", elapsed.Round(time.Second)),
		fmt.Sprintf("Messages in session: %d", msgCount),
	}
	return strings.Join(lines, "\n"), nil
}

func (al *AgentLoop) handleNew(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	sess := al.sessionStore.GetOrCreate(inbound.SessionKey)
	sess.Messages = nil
	sess.LastConsolidated = 0
	al.sessionStore.Save(sess)
	return "New session started.", nil
}

func (al *AgentLoop) Start(ctx context.Context) error {
	al.mu.Lock()
	al.running = true
	al.startTime = time.Now()
	al.mu.Unlock()

	inboundCh := al.bus.ConsumeInbound()

	// Process inbound messages
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-inboundCh:
			if !ok {
				return nil
			}
			al.handleMessage(ctx, msg)
		}
	}
}

func (al *AgentLoop) Stop() {
	al.mu.Lock()
	defer al.mu.Unlock()
	al.running = false
}

func (al *AgentLoop) handleMessage(ctx context.Context, inbound bus.InboundMessage) {
	// Handle commands
	if len(inbound.Content) > 0 && inbound.Content[0] == '/' {
		parts := splitCommand(inbound.Content)
		cmd := parts[0]
		args := ""
		if len(parts) > 1 {
			args = parts[1]
		}
		if handler, ok := al.commands[cmd]; ok {
			resp, err := handler(ctx, args, inbound)
			if err != nil {
				slog.Error("command error", "cmd", cmd, "error", err)
				return
			}
			al.bus.PublishOutbound(bus.OutboundMessage{
				Channel: inbound.Channel,
				ChatID:  inbound.ChatID,
				Content: resp,
				ReplyTo: inbound.SenderID,
			})
			return
		}
	}

	// Cancel any existing processing for this session
	if cancel, ok := al.sessionCancelFuncs[inbound.SessionKey]; ok {
		cancel()
	}

	// Create cancellable context for this session's processing
	msgCtx, cancel := context.WithCancel(ctx)
	al.sessionCancelFuncs[inbound.SessionKey] = cancel

	// Process message in goroutine
	go func() {
		defer func() {
			al.mu.Lock()
			delete(al.sessionCancelFuncs, inbound.SessionKey)
			al.mu.Unlock()
		}()

		outbound, err := al.processMessage(msgCtx, inbound, inbound.SessionKey)
		if err != nil {
			slog.Error("process message error", "error", err)
			return
		}
		if outbound != nil {
			al.bus.PublishOutbound(*outbound)
		}
	}()
}

func (al *AgentLoop) processMessage(ctx context.Context, inbound bus.InboundMessage, sessionKey string) (*bus.OutboundMessage, error) {
	// Get or create session
	sess := al.sessionStore.GetOrCreate(sessionKey)
	sess.Messages = append(sess.Messages, session.Message{
		Role:    "user",
		Content: inbound.Content,
	})

	// Build context and run agent loop
	messages := buildMessages(sess)
	toolDefs := convertToolDefs(al.toolRegistry.GetDefinitions())

	for i := 0; i < al.maxIterations; i++ {
		// Check if context was cancelled (e.g., by /stop)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		resp, err := al.provider.Chat(ctx, messages, toolDefs, providers.ChatOptions{
			MaxTokens:   4096,
			Temperature: 0.7,
		})
		if err != nil {
			// Check if context was cancelled
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.Error("provider error", "error", err)
			break
		}

		messages = append(messages, providers.Message{
			Role:       "assistant",
			Content:    resp.Content,
			ToolCalls:  resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			// Final response
			sess.Messages = append(sess.Messages, session.Message{
				Role:    "assistant",
				Content: resp.Content,
			})
			al.sessionStore.Save(sess)
			return &bus.OutboundMessage{
				Channel: inbound.Channel,
				ChatID:  inbound.ChatID,
				Content: resp.Content,
				ReplyTo: inbound.SenderID,
			}, nil
		}

		// Execute tools
		for _, tc := range resp.ToolCalls {
			slog.Info("executing tool", "name", tc.Name, "id", tc.ID, "args", tc.Arguments)
			result, err := al.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			slog.Info("tool result", "id", tc.ID, "result", fmt.Sprintf("%v", result))
			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("%v", result),
				ToolCallID: tc.ID,
			})
		}
	}

	al.sessionStore.Save(sess)
	return nil, nil
}

// ProcessDirect processes a message directly and returns the outbound payload.
// Used for external calls like cron jobs, heartbeats, etc.
func (al *AgentLoop) ProcessDirect(ctx context.Context, content string, sessionKey string, channel string, chatID string) (*bus.OutboundMessage, error) {
	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "user",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
	}
	return al.processMessage(ctx, msg, sessionKey)
}

func splitCommand(content string) []string {
	var parts []string
	current := ""
	inSpace := false
	for _, c := range content {
		if c == ' ' {
			if !inSpace && current != "" {
				parts = append(parts, current)
				current = ""
			}
			inSpace = true
		} else {
			current += string(c)
			inSpace = false
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func buildMessages(sess *session.Session) []providers.Message {
	var msgs []providers.Message
	for _, msg := range sess.Messages {
		msgs = append(msgs, providers.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	return msgs
}

func convertToolDefs(defs []map[string]any) []providers.ToolDef {
	result := make([]providers.ToolDef, 0, len(defs))
	for _, d := range defs {
		result = append(result, providers.ToolDef{
			Name:        getString(d, "name"),
			Description: getString(d, "description"),
			Parameters:  getMap(d, "parameters"),
		})
	}
	return result
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}
