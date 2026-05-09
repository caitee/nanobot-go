package app

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"nanobot-go/internal/llm"
	"nanobot-go/internal/runtime"
	"nanobot-go/internal/session"
)

// finalCollector listens on an Agent's runtime event stream and captures the
// final assistant text + reasoning at EventAgentEnd. The Dispatcher uses it
// to build the OutboundMessage that channels publish after a turn completes.
type finalCollector struct {
	mu        sync.Mutex
	text      string
	reasoning string
	set       bool
}

func newFinalCollector() *finalCollector { return &finalCollector{} }

func (f *finalCollector) handle(e runtime.Event) {
	if e.Kind != runtime.EventAgentEnd {
		return
	}
	data, ok := e.AgentEnd()
	if !ok {
		return
	}
	text, reasoning := ExtractFinalAssistant(data.Messages)
	f.mu.Lock()
	f.text = text
	f.reasoning = reasoning
	f.set = true
	f.mu.Unlock()
}

func (f *finalCollector) Result() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, f.reasoning
}

// transcriptPersister writes each completed assistant / tool-result message
// back into the session as it arrives, so a turn that gets cancelled midway
// still leaves the read-so-far tool output available to the next turn.
//
// It subscribes to EventMessageEnd. User messages are ignored (runTurn writes
// the user prompt once, synchronously, before the agent starts). Each message
// is appended to the session file incrementally via SessionStore.AppendMessage
// so we don't pay an O(N) rewrite per tool result.
type transcriptPersister struct {
	mu    sync.Mutex
	sess  *session.Session
	store session.SessionStore
}

func newTranscriptPersister(sess *session.Session, store session.SessionStore) *transcriptPersister {
	return &transcriptPersister{sess: sess, store: store}
}

func (p *transcriptPersister) handle(e runtime.Event) {
	if e.Kind != runtime.EventMessageEnd {
		return
	}
	data, ok := e.MessageEnd()
	if !ok {
		return
	}
	underlying, ok := runtime.Unwrap(data.Message)
	if !ok {
		return
	}

	switch m := underlying.(type) {
	case llm.UserMessage:
		// The dispatcher already persisted the user prompt before Prompt()
		// began; the runtime re-emits it for UI consistency.
		return
	case llm.AssistantMessage:
		p.appendAssistant(m)
	case llm.ToolResultMessage:
		p.appendToolResult(m)
	default:
		slog.Debug("transcriptPersister: skipping unknown message type", "kind", e.Kind)
	}
}

func (p *transcriptPersister) appendAssistant(m llm.AssistantMessage) {
	var textBlocks []llm.Content
	var toolCalls []session.ToolCall
	for _, block := range m.Content {
		switch b := block.(type) {
		case llm.ToolCallContent:
			toolCalls = append(toolCalls, session.ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: b.Arguments,
			})
		default:
			textBlocks = append(textBlocks, block)
		}
	}

	msg := session.Message{
		Role:       "assistant",
		Content:    contentBlocksToSession(textBlocks),
		ToolCalls:  toolCalls,
		StopReason: string(m.StopReason),
	}
	p.append(msg)
}

func (p *transcriptPersister) appendToolResult(m llm.ToolResultMessage) {
	msg := session.Message{
		Role:       "tool",
		Content:    contentBlocksToSession(m.Content),
		ToolCallID: m.ToolCallID,
		Name:       m.ToolName,
	}
	p.append(msg)
}

func (p *transcriptPersister) append(msg session.Message) {
	p.mu.Lock()
	p.sess.Messages = append(p.sess.Messages, msg)
	sess := p.sess
	p.mu.Unlock()

	if err := p.store.AppendMessage(sess, msg); err != nil {
		slog.Warn("transcriptPersister: append failed", "error", err)
	}
}

// flush is a no-op kept for API compatibility with callers that treat the
// persister as a write-behind buffer. With AppendMessage on the hot path,
// writes are already synchronous.
func (p *transcriptPersister) flush() {}

// ExtractFinalAssistant scans the run's new messages backwards for the last
// assistant answer and returns its concatenated text + thinking content.
func ExtractFinalAssistant(msgs []runtime.AgentMessage) (text, reasoning string) {
	for i := len(msgs) - 1; i >= 0; i-- {
		underlying, ok := runtime.Unwrap(msgs[i])
		if !ok {
			continue
		}
		am, ok := underlying.(llm.AssistantMessage)
		if !ok {
			continue
		}
		// Skip tool-calling intermediate assistants; we want the final
		// "answer" assistant whose stop reason is not toolUse.
		if am.StopReason == llm.StopReasonToolUse && i < len(msgs)-1 {
			continue
		}
		var tb, rb strings.Builder
		for _, block := range am.Content {
			switch b := block.(type) {
			case llm.TextContent:
				tb.WriteString(b.Text)
			case llm.ThinkingContent:
				rb.WriteString(b.Thinking)
			}
		}
		if tb.Len() == 0 && am.ErrorMessage != "" {
			return "Error: " + am.ErrorMessage, rb.String()
		}
		return tb.String(), rb.String()
	}
	return "", ""
}

// messagesFromSession converts a persisted session transcript into
// runtime.AgentMessage values the runtime can consume as InitialHistory.
// Assistant rows may carry a mix of TextContent and ToolCallContent blocks;
// tool rows are rehydrated as ToolResultMessage with their ToolCallID so the
// provider can correlate them with the matching assistant call.
func messagesFromSession(sess *session.Session) []runtime.AgentMessage {
	out := make([]runtime.AgentMessage, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		switch m.Role {
		case "user":
			out = append(out, runtime.WrapLLM(llm.UserMessage{
				Content:   contentFromSession(m.Content),
				Timestamp: time.Now(),
			}))
		case "assistant":
			content := contentFromSession(m.Content)
			for _, tc := range m.ToolCalls {
				content = append(content, llm.ToolCallContent{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				})
			}
			stop := llm.StopReasonStop
			if m.StopReason != "" {
				stop = llm.StopReason(m.StopReason)
			} else if len(m.ToolCalls) > 0 {
				stop = llm.StopReasonToolUse
			}
			out = append(out, runtime.WrapLLM(llm.AssistantMessage{
				Content:    content,
				StopReason: stop,
				Timestamp:  time.Now(),
			}))
		case "tool":
			out = append(out, runtime.WrapLLM(llm.ToolResultMessage{
				ToolCallID: m.ToolCallID,
				ToolName:   m.Name,
				Content:    contentFromSession(m.Content),
				Timestamp:  time.Now(),
			}))
		default:
			slog.Debug("messagesFromSession: skipping unknown role", "role", m.Role)
		}
	}
	return out
}

// contentFromSession rehydrates the persisted Content field back into a
// []llm.Content. A string is wrapped as TextContent; a []any of maps is
// decoded block-by-block (text / tool_call / image / thinking).
func contentFromSession(v any) []llm.Content {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if x == "" {
			return nil
		}
		return []llm.Content{llm.TextContent{Text: x}}
	case []byte:
		if len(x) == 0 {
			return nil
		}
		return []llm.Content{llm.TextContent{Text: string(x)}}
	case []llm.Content:
		return x
	case []any:
		out := make([]llm.Content, 0, len(x))
		for _, raw := range x {
			if c, ok := contentBlockFromAny(raw); ok {
				out = append(out, c)
			}
		}
		return out
	}
	return nil
}

// contentBlockFromAny decodes a single persisted content block. Blocks we
// don't recognise are dropped rather than passed through as unknown types
// because the provider adapters only know about the concrete llm.Content
// implementations.
func contentBlockFromAny(raw any) (llm.Content, bool) {
	switch c := raw.(type) {
	case llm.Content:
		return c, true
	case map[string]any:
		kind, _ := c["type"].(string)
		switch kind {
		case "", "text":
			text, _ := c["text"].(string)
			sig, _ := c["signature"].(string)
			return llm.TextContent{Text: text, Signature: sig}, true
		case "tool_call":
			id, _ := c["id"].(string)
			name, _ := c["name"].(string)
			args, _ := c["arguments"].(map[string]any)
			return llm.ToolCallContent{ID: id, Name: name, Arguments: args}, true
		case "thinking":
			thinking, _ := c["thinking"].(string)
			sig, _ := c["signature"].(string)
			redacted, _ := c["redacted"].(bool)
			return llm.ThinkingContent{Thinking: thinking, Signature: sig, Redacted: redacted}, true
		case "image":
			data, _ := c["data"].(string)
			mime, _ := c["mime_type"].(string)
			return llm.ImageContent{Data: data, MimeType: mime}, true
		}
	case string:
		return llm.TextContent{Text: c}, true
	}
	return nil, false
}

// contentBlocksToSession converts []llm.Content into the persisted []any shape
// so they round-trip through JSON cleanly.
func contentBlocksToSession(blocks []llm.Content) any {
	if len(blocks) == 0 {
		return ""
	}
	// Fast path: pure text → store as a plain string for backwards compat and
	// smaller files. Only fall through to the structured form when there's a
	// non-text block.
	allText := true
	var sb strings.Builder
	for _, b := range blocks {
		t, ok := b.(llm.TextContent)
		if !ok {
			allText = false
			break
		}
		sb.WriteString(t.Text)
	}
	if allText {
		return sb.String()
	}

	out := make([]any, 0, len(blocks))
	for _, b := range blocks {
		switch x := b.(type) {
		case llm.TextContent:
			out = append(out, map[string]any{"type": "text", "text": x.Text, "signature": x.Signature})
		case llm.ToolCallContent:
			out = append(out, map[string]any{"type": "tool_call", "id": x.ID, "name": x.Name, "arguments": x.Arguments})
		case llm.ThinkingContent:
			out = append(out, map[string]any{"type": "thinking", "thinking": x.Thinking, "signature": x.Signature, "redacted": x.Redacted})
		case llm.ImageContent:
			out = append(out, map[string]any{"type": "image", "data": x.Data, "mime_type": x.MimeType})
		}
	}
	return out
}

func extractSessionContent(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case nil:
		return ""
	case []any:
		var sb strings.Builder
		for _, raw := range x {
			if m, ok := raw.(map[string]any); ok {
				if kind, _ := m["type"].(string); kind == "" || kind == "text" {
					if t, ok := m["text"].(string); ok {
						sb.WriteString(t)
					}
				}
			}
		}
		return sb.String()
	}
	return ""
}
