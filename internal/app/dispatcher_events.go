package app

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"nanobot-go/internal/bus"
	"nanobot-go/internal/llm"
	"nanobot-go/internal/runtime"
	"nanobot-go/internal/session"
)

// eventTranslator turns new runtime.Event streams into the legacy
// bus.AgentEvent sequence the TUI is still written against. It also
// accumulates the final assistant text / reasoning so the Dispatcher can
// publish an OutboundMessage at the end.
//
// Translation strategy (a per-event mapping is the simplest possible glue):
//
//	runtime.EventTurnStart        -> bus.EventLLMThinking / EventLLMResponding
//	runtime.EventMessageUpdate    -> bus.EventLLMStreamChunk (text/thinking)
//	runtime.EventToolExecution*   -> bus.EventToolStart / EventToolEnd / EventToolError
//	runtime.EventTurnEnd          -> bus.EventLLMToolCalls when the assistant
//	                                 actually called tools
//	runtime.EventAgentEnd         -> bus.EventLLMFinal + bus.EventSessionEnd
//
// The translator is stateless between turns; the only per-run state is the
// accumulated text buffers, kept so we can fire a single EventLLMFinal.
type eventTranslator struct {
	bus        bus.MessageBus
	sessionKey string
	final      *finalCollector

	mu            sync.Mutex
	text          string
	reasoning     string
	hadTurnStart  bool
	lastStopIsErr bool
	errorMessage  string
}

func newEventTranslator(b bus.MessageBus, sessionKey string, final *finalCollector) *eventTranslator {
	return &eventTranslator{bus: b, sessionKey: sessionKey, final: final}
}

func (t *eventTranslator) handle(e runtime.Event) {
	switch e.Kind {
	case runtime.EventAgentStart:
		// Nothing to translate: dispatcher publishes EventSessionStart itself
		// before invoking Prompt so the TUI gets an immediate signal.

	case runtime.EventTurnStart:
		t.mu.Lock()
		t.hadTurnStart = true
		// Clear per-turn buffers. Final message accumulation uses the
		// message_end path instead, which survives multiple turns.
		t.text = ""
		t.reasoning = ""
		t.mu.Unlock()
		t.publish(bus.AgentEvent{
			Type: bus.EventLLMThinking,
		})
		t.publish(bus.AgentEvent{
			Type: bus.EventLLMResponding,
		})

	case runtime.EventMessageUpdate:
		data, ok := e.MessageUpdate()
		if !ok {
			return
		}
		ev := data.StreamEvent
		switch ev.Kind {
		case llm.StreamEventTextDelta:
			t.mu.Lock()
			t.text += ev.Delta
			full := t.text
			t.mu.Unlock()
			t.publish(bus.AgentEvent{
				Type: bus.EventLLMStreamChunk,
				Data: bus.StreamChunkData{Delta: ev.Delta, FullText: full, IsReasoning: false},
			})
		case llm.StreamEventThinkingDelta:
			t.mu.Lock()
			t.reasoning += ev.Delta
			full := t.reasoning
			t.mu.Unlock()
			t.publish(bus.AgentEvent{
				Type: bus.EventLLMStreamChunk,
				Data: bus.StreamChunkData{Delta: ev.Delta, FullText: full, IsReasoning: true},
			})
		case llm.StreamEventError:
			t.mu.Lock()
			t.lastStopIsErr = true
			t.errorMessage = ev.ErrorMessage
			t.mu.Unlock()
		}

	case runtime.EventMessageEnd:
		data, ok := e.MessageEnd()
		if !ok {
			return
		}
		// Only process assistant messages here; user / tool messages are
		// not mapped to legacy events.
		underlying, ok := runtime.Unwrap(data.Message)
		if !ok {
			return
		}
		if am, ok := underlying.(llm.AssistantMessage); ok {
			t.recordAssistant(am)
		}

	case runtime.EventTurnEnd:
		data, ok := e.TurnEnd()
		if !ok {
			return
		}
		// Surface the legacy EventLLMToolCalls whenever the assistant
		// issued tool calls, after the tools have already executed. The
		// TUI uses this for the "summary bubble" of tool names.
		var calls []bus.ToolCallInfo
		for _, block := range data.Assistant.Content {
			if tc, ok := block.(llm.ToolCallContent); ok {
				calls = append(calls, bus.ToolCallInfo{
					ID:   tc.ID,
					Name: tc.Name,
					Args: tc.Arguments,
				})
			}
		}
		if len(calls) > 0 {
			t.publish(bus.AgentEvent{
				Type: bus.EventLLMToolCalls,
				Data: calls,
			})
		}

	case runtime.EventToolExecutionStart:
		data, ok := e.ToolStart()
		if !ok {
			return
		}
		t.publish(bus.AgentEvent{
			Type: bus.EventToolStart,
			Data: bus.ToolCallInfo{ID: data.ToolCallID, Name: data.ToolName, Args: data.Args},
		})

	case runtime.EventToolExecutionEnd:
		data, ok := e.ToolEnd()
		if !ok {
			return
		}
		kind := bus.EventToolEnd
		if data.IsError {
			kind = bus.EventToolError
		}
		t.publish(bus.AgentEvent{
			Type: kind,
			Data: bus.ToolResultEventData{
				ToolID:   data.ToolCallID,
				ToolName: data.ToolName,
				Success:  !data.IsError,
				Result:   toolResultString(data.Result),
				Error:    toolErrorString(data),
			},
		})

	case runtime.EventAgentEnd:
		data, _ := e.AgentEnd()
		// Pull the final assistant message from the run's new messages.
		finalText, finalReasoning := extractFinalAssistant(data.Messages)
		t.final.Set(finalText, finalReasoning)

		t.mu.Lock()
		errMsg := t.errorMessage
		isErr := t.lastStopIsErr
		t.mu.Unlock()

		t.publish(bus.AgentEvent{
			Type: bus.EventLLMFinal,
			Data: bus.LLMFinalData{
				Content:          finalText,
				ReasoningContent: finalReasoning,
				Error:            errMsg,
			},
		})
		t.publish(bus.AgentEvent{
			Type: bus.EventSessionEnd,
			Data: bus.SessionEndData{Cancelled: isErr},
		})
	}
}

func (t *eventTranslator) recordAssistant(am llm.AssistantMessage) {
	// If the message carries an explicit error, remember it so the final
	// translation can surface it.
	if am.ErrorMessage != "" {
		t.mu.Lock()
		t.lastStopIsErr = true
		t.errorMessage = am.ErrorMessage
		t.mu.Unlock()
	}
}

func (t *eventTranslator) publish(e bus.AgentEvent) {
	e.SessionKey = t.sessionKey
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	t.bus.PublishAgentEvent(e)
}

// extractFinalAssistant scans the run's new messages backwards for the last
// assistant message and returns its concatenated text and thinking blocks.
func extractFinalAssistant(msgs []runtime.AgentMessage) (text, reasoning string) {
	for i := len(msgs) - 1; i >= 0; i-- {
		underlying, ok := runtime.Unwrap(msgs[i])
		if !ok {
			continue
		}
		am, ok := underlying.(llm.AssistantMessage)
		if !ok {
			continue
		}
		// Skip tool-calling intermediate assistants: we want the final
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
		// Fall back to errorMessage when no text content is present.
		if tb.Len() == 0 && am.ErrorMessage != "" {
			return "Error: " + am.ErrorMessage, rb.String()
		}
		return tb.String(), rb.String()
	}
	return "", ""
}

func toolResultString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []llm.Content:
		var b strings.Builder
		for _, c := range x {
			if t, ok := c.(llm.TextContent); ok {
				b.WriteString(t.Text)
			}
		}
		return b.String()
	}
	return ""
}

func toolErrorString(d runtime.ToolEndData) string {
	if !d.IsError {
		return ""
	}
	return toolResultString(d.Result)
}

// finalCollector is a tiny mailbox used by the translator to hand the final
// assistant content back to the Dispatcher after agent_end.
type finalCollector struct {
	mu        sync.Mutex
	text      string
	reasoning string
	set       bool
}

func newFinalCollector() *finalCollector { return &finalCollector{} }

func (f *finalCollector) Set(text, reasoning string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.text = text
	f.reasoning = reasoning
	f.set = true
}

func (f *finalCollector) Result() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, f.reasoning
}

// messagesFromSession converts the session transcript into AgentMessage
// entries the runtime can consume. It preserves user / assistant / tool
// role ordering.
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
	// Fall back to %v formatting for maps / slices.
	return ""
}
