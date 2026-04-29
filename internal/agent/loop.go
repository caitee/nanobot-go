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
		cmd := strings.TrimPrefix(parts[0], "/")
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
	al.publishAgentEvent(sessionKey, bus.EventSessionStart, nil)
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
			al.publishAgentEvent(sessionKey, bus.EventSessionEnd, bus.SessionEndData{Cancelled: true})
			return nil, ctx.Err()
		default:
		}

		// Publish thinking event
		al.publishAgentEvent(sessionKey, bus.EventLLMThinking, nil)

		// Use streaming for response
		al.publishAgentEvent(sessionKey, bus.EventLLMResponding, nil)
		var fullText string
		var reasoningText string
		resp := &providers.LLMResponse{}
		streamCh := al.provider.StreamGenerate(ctx, messages, toolDefs, providers.ChatOptions{
			MaxTokens:   4096,
			Temperature: 0.7,
		})
		for chunk := range streamCh {
			if chunk.Error != nil {
				slog.Error("stream error", "error", chunk.Error)
				al.publishAgentEvent(sessionKey, bus.EventLLMFinal, bus.LLMFinalData{
					Error: chunk.Error.Error(),
				})
				al.publishAgentEvent(sessionKey, bus.EventSessionEnd, bus.SessionEndData{})
				return nil, nil
			}
			if chunk.Chunk != "" {
				totalText := fullText
				if chunk.IsReasoning {
					reasoningText += chunk.Chunk
					totalText = reasoningText
				} else {
					fullText += chunk.Chunk
					totalText = fullText
				}
				slog.Info("stream chunk", "isReasoning", chunk.IsReasoning, "chunk", chunk.Chunk, "fullTextLen", len(fullText), "reasoningLen", len(reasoningText))
				al.publishAgentEvent(sessionKey, bus.EventLLMStreamChunk, bus.StreamChunkData{
					Delta:       chunk.Chunk,
					FullText:    totalText,
					IsReasoning: chunk.IsReasoning,
				})
			}
			if chunk.Done {
				resp.Content = chunk.Content
				resp.ToolCalls = chunk.ToolCalls
				resp.FinishReason = chunk.FinishReason
				resp.Usage = chunk.Usage
				resp.ReasoningContent = chunk.ReasoningContent
				break
			}
		}

		// Override content with streamed text (only if we actually got streamed content)
		if resp.Content == "" && fullText != "" {
			resp.Content = fullText
		}
		if resp.ReasoningContent == "" && reasoningText != "" {
			resp.ReasoningContent = reasoningText
		}

		messages = append(messages, providers.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			// Final response - no tools. Now mark as done.
			// If reasoningText is empty (thinking wasn't streamed), use resp.ReasoningContent
			finalReasoning := reasoningText
			if finalReasoning == "" && resp.ReasoningContent != "" {
				finalReasoning = resp.ReasoningContent
			}
			slog.Info("llm_final event", "contentLen", len(resp.Content), "reasoningLen", len(finalReasoning), "reasoningTextEmpty", reasoningText == "", "respReasoningEmpty", resp.ReasoningContent == "")
			al.publishAgentEvent(sessionKey, bus.EventLLMFinal, bus.LLMFinalData{
				Content:          resp.Content,
				ReasoningContent: finalReasoning,
			})
			sess.Messages = append(sess.Messages, session.Message{
				Role:    "assistant",
				Content: resp.Content,
			})
			al.sessionStore.Save(sess)
			al.publishAgentEvent(sessionKey, bus.EventSessionEnd, bus.SessionEndData{})
			slog.Info("processMessage: returning final response", "contentLen", len(resp.Content))
			return &bus.OutboundMessage{
				Channel:   inbound.Channel,
				ChatID:    inbound.ChatID,
				Content:   resp.Content,
				ReplyTo:   inbound.SenderID,
				Reasoning: finalReasoning,
				Metadata: map[string]any{
					bus.OutboundMetadataAgentEventFinal: true,
				},
			}, nil
		}

		// LLM wants to call tools
		al.publishAgentEvent(sessionKey, bus.EventLLMToolCalls, convertToolCallInfo(resp.ToolCalls))

		// Execute tools
		for _, tc := range resp.ToolCalls {
			startTime := time.Now()
			slog.Info("executing tool", "name", tc.Name, "id", tc.ID, "args", tc.Arguments)

			// Publish tool start event
			al.publishAgentEvent(sessionKey, bus.EventToolStart, bus.ToolCallInfo{
				ID:   tc.ID,
				Name: tc.Name,
				Args: tc.Arguments,
			})

			result, err := al.toolRegistry.Execute(ctx, tc.Name, tc.Arguments)
			duration := time.Since(startTime)

			if err != nil {
				al.publishAgentEvent(sessionKey, bus.EventToolError, bus.ToolResultEventData{
					ToolName:   tc.Name,
					ToolID:     tc.ID,
					Success:    false,
					Error:      err.Error(),
					DurationMs: duration.Milliseconds(),
				})
				result = fmt.Sprintf("error: %v", err)
			} else {
				al.publishAgentEvent(sessionKey, bus.EventToolEnd, bus.ToolResultEventData{
					ToolName:   tc.Name,
					ToolID:     tc.ID,
					Success:    true,
					Result:     fmt.Sprintf("%v", result),
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
	al.publishAgentEvent(sessionKey, bus.EventSessionEnd, bus.SessionEndData{})
	return nil, nil
}

// publishAgentEvent is a helper to publish agent events
func (al *AgentLoop) publishAgentEvent(sessionKey string, eventType bus.AgentEventType, data any) {
	al.bus.PublishAgentEvent(bus.AgentEvent{
		SessionKey: sessionKey,
		Type:       eventType,
		Timestamp:  time.Now(),
		Data:       data,
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
