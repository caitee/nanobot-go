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
	enableReasoning    bool
	commands           map[string]CommandHandler
	mu                 sync.Mutex
	running            bool
	startTime          time.Time
	sessionCancelFuncs map[string]context.CancelFunc
	reasoningStates    sync.Map // sessionKey -> bool
}

type CommandHandler func(ctx context.Context, args string, inbound bus.InboundMessage) (string, error)

func NewAgentLoop(bus bus.MessageBus, sessionStore session.SessionStore, toolRegistry tools.ToolRegistry, provider providers.LLMProvider, maxIterations int, enableReasoning bool) *AgentLoop {
	al := &AgentLoop{
		bus:                bus,
		sessionStore:       sessionStore,
		toolRegistry:       toolRegistry,
		provider:           provider,
		maxIterations:      maxIterations,
		enableReasoning:    enableReasoning,
		commands:           make(map[string]CommandHandler),
		sessionCancelFuncs: make(map[string]context.CancelFunc),
		reasoningStates:    sync.Map{},
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
	al.commands["reasoning"] = al.handleReasoning
}

func (al *AgentLoop) handleHelp(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	lines := []string{
		"nanobot commands:",
		"/new — Start a new conversation",
		"/stop — Stop the current task",
		"/restart — Restart the bot",
		"/status — Show bot status",
		"/reasoning on|off — Toggle thinking mode",
		"/help — Show available commands",
	}
	return strings.Join(lines, "\n"), nil
}

func (al *AgentLoop) handleReasoning(ctx context.Context, args string, inbound bus.InboundMessage) (string, error) {
	args = strings.TrimSpace(strings.ToLower(args))
	var newState bool

	switch args {
	case "on", "true", "1", "yes":
		newState = true
	case "off", "false", "0", "no":
		newState = false
	case "":
		// Toggle current state
		if v, ok := al.reasoningStates.Load(inbound.SessionKey); ok {
			newState = !v.(bool)
		} else {
			newState = !al.enableReasoning
		}
	default:
		return "Usage: /reasoning on|off (or /reasoning to toggle)", nil
	}

	al.reasoningStates.Store(inbound.SessionKey, newState)
	stateStr := "enabled"
	if !newState {
		stateStr = "disabled"
	}
	return fmt.Sprintf("Thinking mode: %s", stateStr), nil
}

// isReasoningEnabled returns whether reasoning is enabled for a session.
func (al *AgentLoop) isReasoningEnabled(sessionKey string) bool {
	if v, ok := al.reasoningStates.Load(sessionKey); ok {
		return v.(bool)
	}
	return al.enableReasoning
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
	slog.Info("AgentLoop started, waiting for inbound messages")

	// Process inbound messages
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-inboundCh:
			if !ok {
				slog.Info("AgentLoop: inbound channel closed")
				return nil
			}
			slog.Info("AgentLoop received inbound message", "content", msg.Content, "session", msg.SessionKey)
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

		slog.Info("AgentLoop processing message", "session", inbound.SessionKey)
		outbound, err := al.processMessage(msgCtx, inbound, inbound.SessionKey)
		slog.Info("AgentLoop processMessage returned", "session", inbound.SessionKey, "hasOutbound", outbound != nil, "err", err)
		if err != nil {
			slog.Error("process message error", "error", err)
			return
		}
		if outbound != nil {
			slog.Info("AgentLoop publishing outbound response", "contentLen", len(outbound.Content))
			al.bus.PublishOutbound(*outbound)
		} else {
			slog.Warn("AgentLoop: no outbound to publish, returning without response")
		}
	}()
}

func (al *AgentLoop) processMessage(ctx context.Context, inbound bus.InboundMessage, sessionKey string) (*bus.OutboundMessage, error) {
	// Publish session start event
	al.publishAgentEvent(sessionKey, EventSessionStart, nil)
	slog.Info("processMessage: session started", "session", sessionKey)

	// Get or create session
	sess := al.sessionStore.GetOrCreate(sessionKey)
	sess.Messages = append(sess.Messages, session.Message{
		Role:    "user",
		Content: inbound.Content,
	})

	// Build context and run agent loop
	messages := al.buildMessagesWithReasoning(sess, sessionKey)
	toolDefs := convertToolDefs(al.toolRegistry.GetDefinitions())
	slog.Info("processMessage: built messages", "count", len(messages))

	for i := 0; i < al.maxIterations; i++ {
		// Check if context was cancelled (e.g., by /stop)
		select {
		case <-ctx.Done():
			al.publishAgentEvent(sessionKey, EventSessionEnd, map[string]any{"cancelled": true})
			return nil, ctx.Err()
		default:
		}

		// Publish thinking event
		al.publishAgentEvent(sessionKey, EventLLMThinking, nil)

		// Use streaming for response
		al.publishAgentEvent(sessionKey, EventLLMResponding, nil)
		var fullText string
		streamCh := al.provider.StreamGenerate(ctx, messages, toolDefs, providers.ChatOptions{
			MaxTokens:   4096,
			Temperature: 0.7,
		})
		for chunk := range streamCh {
			if chunk.Error != nil {
				slog.Error("stream error", "error", chunk.Error)
				al.publishAgentEvent(sessionKey, EventLLMFinal, map[string]any{
					"error": chunk.Error.Error(),
				})
				al.publishAgentEvent(sessionKey, EventSessionEnd, nil)
				return nil, nil
			}
			if chunk.Chunk != "" {
				fullText += chunk.Chunk
				al.publishAgentEvent(sessionKey, EventLLMStreamChunk, bus.StreamChunkData{
					Delta:    chunk.Chunk,
					FullText: fullText,
				})
			}
			if chunk.Done {
				break
			}
		}

		// Build the final response from accumulated content
		// We need tool calls, so do a non-streaming call to get them properly
		// This is necessary because streaming doesn't return tool calls in the same way
		resp, chatErr := al.provider.Chat(ctx, messages, toolDefs, providers.ChatOptions{
			MaxTokens:   4096,
			Temperature: 0.7,
		})
		if chatErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.Error("provider error", "error", chatErr)
			al.publishAgentEvent(sessionKey, EventLLMFinal, map[string]any{
				"error": chatErr.Error(),
			})
			al.publishAgentEvent(sessionKey, EventSessionEnd, nil)
			return nil, nil
		}

		// Override content with streamed text
		resp.Content = fullText

		messages = append(messages, providers.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			// Final response - no tools. Now mark as done.
			al.publishAgentEvent(sessionKey, EventLLMFinal, bus.LLMFinalData{
				Content: resp.Content,
			})
			sess.Messages = append(sess.Messages, session.Message{
				Role:    "assistant",
				Content: resp.Content,
			})
			al.sessionStore.Save(sess)
			al.publishAgentEvent(sessionKey, EventSessionEnd, nil)
			slog.Info("processMessage: returning final response", "contentLen", len(resp.Content))
			return &bus.OutboundMessage{
				Channel: inbound.Channel,
				ChatID:  inbound.ChatID,
				Content: resp.Content,
				ReplyTo: inbound.SenderID,
			}, nil
		}

		// LLM wants to call tools
		al.publishAgentEvent(sessionKey, EventLLMToolCalls, bus.ToolCallEventData{
			ToolCalls: convertToolCallInfo(resp.ToolCalls),
		})

		// Execute tools
		for _, tc := range resp.ToolCalls {
			startTime := time.Now()
			slog.Info("executing tool", "name", tc.Name, "id", tc.ID, "args", tc.Arguments)

			// Publish tool start event
			al.publishAgentEvent(sessionKey, EventToolStart, bus.ToolCallEventData{
				ToolCalls: []bus.ToolCallInfo{{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Arguments,
				}},
			})

			result, err := al.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)
			duration := time.Since(startTime)

			if err != nil {
				al.publishAgentEvent(sessionKey, EventToolError, bus.ToolResultEventData{
					ToolName:   tc.Name,
					ToolID:     tc.ID,
					Success:    false,
					Error:      err.Error(),
					DurationMs: duration.Milliseconds(),
				})
				result = fmt.Sprintf("error: %v", err)
			} else {
				al.publishAgentEvent(sessionKey, EventToolEnd, bus.ToolResultEventData{
					ToolName:   tc.Name,
					ToolID:     tc.ID,
					Success:    true,
					Result:     "", // Don't expose result by default
					DurationMs: duration.Milliseconds(),
				})
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
	al.publishAgentEvent(sessionKey, EventSessionEnd, nil)
	return nil, nil
}

// publishAgentEvent is a helper to publish agent events
func (al *AgentLoop) publishAgentEvent(sessionKey, eventType string, data any) {
	eventData := make(map[string]any)
	if data != nil {
		eventData["data"] = data
	}
	al.bus.PublishAgentEvent(bus.AgentEvent{
		SessionKey: sessionKey,
		Type:       eventType,
		Timestamp:  time.Now(),
		Data:      eventData,
	})
}

// convertToolCallInfo converts provider ToolCalls to bus.ToolCallInfo
func convertToolCallInfo(tcs []providers.ToolCall) []bus.ToolCallInfo {
	result := make([]bus.ToolCallInfo, len(tcs))
	for i, tc := range tcs {
		result[i] = bus.ToolCallInfo{
			ID:   tc.ID,
			Name: tc.Name,
			Args: tc.Arguments,
		}
	}
	return result
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

// buildMessagesWithReasoning builds messages with reasoning instruction when enabled.
func (al *AgentLoop) buildMessagesWithReasoning(sess *session.Session, sessionKey string) []providers.Message {
	msgs := buildMessages(sess)
	if al.isReasoningEnabled(sessionKey) {
		// Prepend reasoning instruction as a system message
		reasoningMsg := providers.Message{
			Role:    "system",
			Content: "请展示你的思考过程。在回答时，先写出你的推理和思考步骤，再用中文给出最终答案。",
		}
		msgs = append([]providers.Message{reasoningMsg}, msgs...)
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
