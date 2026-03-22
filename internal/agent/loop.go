package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"nanobot-go/internal/bus"
	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tools"
)

type AgentLoop struct {
	bus           bus.MessageBus
	sessionStore  session.SessionStore
	toolRegistry  tools.ToolRegistry
	provider      providers.LLMProvider
	maxIterations int
	commands      map[string]CommandHandler
	mu            sync.Mutex
	running       bool
}

type CommandHandler func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error)

func NewAgentLoop(bus bus.MessageBus, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int) *AgentLoop {
	al := &AgentLoop{
		bus:           bus,
		sessionStore:  sessionStore,
		toolRegistry:  toolRegistry,
		provider:      provider,
		maxIterations: maxIterations,
		commands:      make(map[string]CommandHandler),
	}
	al.registerDefaultCommands()
	return al
}

func (al *AgentLoop) registerDefaultCommands() {
	al.commands["help"] = func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
		return "Available commands: /help, /stop, /restart, /status, /new", nil
	}
	al.commands["stop"] = func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
		al.Stop()
		return "Agent stopped", nil
	}
	al.commands["restart"] = func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
		return "Agent restarted", nil
	}
	al.commands["status"] = func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
		al.mu.Lock()
		running := al.running
		al.mu.Unlock()
		status := "running"
		if !running {
			status = "stopped"
		}
		return fmt.Sprintf("Agent status: %s", status), nil
	}
}

func (al *AgentLoop) Start(ctx context.Context) error {
	al.mu.Lock()
	al.running = true
	al.mu.Unlock()

	inboundCh := al.bus.ConsumeInbound()
	outboundCh := al.bus.ConsumeOutbound()

	// Start outbound forwarder goroutine
	go func() {
		for msg := range outboundCh {
			// Forward to appropriate channel
			slog.Info("outbound message", "channel", msg.Channel, "chat_id", msg.ChatID)
		}
	}()

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
				Channel:  inbound.Channel,
				ChatID:   inbound.ChatID,
				Content:  resp,
				ReplyTo:  inbound.SenderID,
			})
			return
		}
	}

	// Get or create session
	sess := al.sessionStore.GetOrCreate(inbound.SessionKey)
	sess.Messages = append(sess.Messages, session.Message{
		Role:    "user",
		Content: inbound.Content,
	})

	// Build context and run agent loop
	messages := buildMessages(sess)
	toolDefs := convertToolDefs(al.toolRegistry.GetDefinitions())

	for i := 0; i < al.maxIterations; i++ {
		resp, err := al.provider.Chat(ctx, messages, toolDefs, providers.ChatOptions{
			MaxTokens:   4096,
			Temperature: 0.7,
		})
		if err != nil {
			slog.Error("provider error", "error", err)
			break
		}

		messages = append(messages, providers.Message{
			Role:    "assistant",
			Content: resp.Content,
		})

		if len(resp.ToolCalls) == 0 {
			// Final response
			al.bus.PublishOutbound(bus.OutboundMessage{
				Channel:  inbound.Channel,
				ChatID:   inbound.ChatID,
				Content:  resp.Content,
				ReplyTo:  inbound.SenderID,
			})
			sess.Messages = append(sess.Messages, session.Message{
				Role:    "assistant",
				Content: resp.Content,
			})
			break
		}

		// Execute tools
		for _, tc := range resp.ToolCalls {
			result, err := al.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			messages = append(messages, providers.Message{
				Role:    "tool",
				Content: fmt.Sprintf("%v", result),
			})
		}
	}

	al.sessionStore.Save(sess)
}

func splitCommand(content string) []string {
	var parts []string
	current := ""
	for _, c := range content {
		if c == ' ' && len(parts) == 0 {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
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
