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
func messagesFromSession(sess *session.Session) []runtime.AgentMessage {
	out := make([]runtime.AgentMessage, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		text := extractSessionContent(m.Content)
		switch m.Role {
		case "user":
			out = append(out, runtime.WrapLLM(llm.UserMessage{
				Content:   []llm.Content{llm.TextContent{Text: text}},
				Timestamp: time.Now(),
			}))
		case "assistant":
			out = append(out, runtime.WrapLLM(llm.AssistantMessage{
				Content:    []llm.Content{llm.TextContent{Text: text}},
				StopReason: llm.StopReasonStop,
				Timestamp:  time.Now(),
			}))
		case "tool":
			out = append(out, runtime.WrapLLM(llm.ToolResultMessage{
				Content:   []llm.Content{llm.TextContent{Text: text}},
				Timestamp: time.Now(),
			}))
		default:
			slog.Debug("messagesFromSession: skipping unknown role", "role", m.Role)
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
	}
	return ""
}
