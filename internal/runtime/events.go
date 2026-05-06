package runtime

import (
	"time"

	"nanobot-go/internal/llm"
)

// EventKind enumerates every event an Agent emits during a run.
type EventKind string

const (
	EventAgentStart         EventKind = "agent_start"
	EventAgentEnd           EventKind = "agent_end"
	EventTurnStart          EventKind = "turn_start"
	EventTurnEnd            EventKind = "turn_end"
	EventMessageStart       EventKind = "message_start"
	EventMessageUpdate      EventKind = "message_update"
	EventMessageEnd         EventKind = "message_end"
	EventToolExecutionStart EventKind = "tool_execution_start"
	EventToolExecUpdate     EventKind = "tool_execution_update"
	EventToolExecutionEnd   EventKind = "tool_execution_end"
)

// Event is the generic envelope delivered to subscribers. Data is populated
// according to Kind; use the typed accessors (.TurnEnd(), .ToolEnd(), ...) or
// switch on Data's concrete type.
type Event struct {
	Kind       EventKind
	SessionID  string
	Timestamp  time.Time
	Data       any
}

// Typed payloads ---------------------------------------------------------

// AgentEndData accompanies EventAgentEnd. Messages is the full list of messages
// appended during this run (not the entire transcript).
type AgentEndData struct {
	Messages []AgentMessage
}

// TurnEndData accompanies EventTurnEnd.
type TurnEndData struct {
	Assistant   llm.AssistantMessage
	ToolResults []llm.ToolResultMessage
}

// MessageStartData, MessageEndData accompany the message lifecycle events.
type MessageStartData struct {
	Message AgentMessage
}

type MessageEndData struct {
	Message AgentMessage
}

// MessageUpdateData accompanies EventMessageUpdate: a streaming partial.
type MessageUpdateData struct {
	Partial     AgentMessage
	StreamEvent llm.StreamEvent
}

// ToolStartData accompanies EventToolExecutionStart.
type ToolStartData struct {
	ToolCallID string
	ToolName   string
	Args       map[string]any
}

// ToolUpdateData accompanies EventToolExecUpdate.
type ToolUpdateData struct {
	ToolCallID string
	ToolName   string
	Args       map[string]any
	Partial    any
}

// ToolEndData accompanies EventToolExecutionEnd.
type ToolEndData struct {
	ToolCallID string
	ToolName   string
	Result     any
	IsError    bool
}

// Typed accessors --------------------------------------------------------

// AgentEnd returns the payload of an EventAgentEnd, or false otherwise.
func (e Event) AgentEnd() (AgentEndData, bool) {
	if e.Kind != EventAgentEnd {
		return AgentEndData{}, false
	}
	d, ok := e.Data.(AgentEndData)
	return d, ok
}

// TurnEnd returns the payload of an EventTurnEnd, or false otherwise.
func (e Event) TurnEnd() (TurnEndData, bool) {
	if e.Kind != EventTurnEnd {
		return TurnEndData{}, false
	}
	d, ok := e.Data.(TurnEndData)
	return d, ok
}

// MessageStart returns the payload of an EventMessageStart.
func (e Event) MessageStart() (MessageStartData, bool) {
	if e.Kind != EventMessageStart {
		return MessageStartData{}, false
	}
	d, ok := e.Data.(MessageStartData)
	return d, ok
}

// MessageUpdate returns the payload of an EventMessageUpdate.
func (e Event) MessageUpdate() (MessageUpdateData, bool) {
	if e.Kind != EventMessageUpdate {
		return MessageUpdateData{}, false
	}
	d, ok := e.Data.(MessageUpdateData)
	return d, ok
}

// MessageEnd returns the payload of an EventMessageEnd.
func (e Event) MessageEnd() (MessageEndData, bool) {
	if e.Kind != EventMessageEnd {
		return MessageEndData{}, false
	}
	d, ok := e.Data.(MessageEndData)
	return d, ok
}

// ToolStart returns the payload of an EventToolExecutionStart.
func (e Event) ToolStart() (ToolStartData, bool) {
	if e.Kind != EventToolExecutionStart {
		return ToolStartData{}, false
	}
	d, ok := e.Data.(ToolStartData)
	return d, ok
}

// ToolUpdate returns the payload of an EventToolExecUpdate.
func (e Event) ToolUpdate() (ToolUpdateData, bool) {
	if e.Kind != EventToolExecUpdate {
		return ToolUpdateData{}, false
	}
	d, ok := e.Data.(ToolUpdateData)
	return d, ok
}

// ToolEnd returns the payload of an EventToolExecutionEnd.
func (e Event) ToolEnd() (ToolEndData, bool) {
	if e.Kind != EventToolExecutionEnd {
		return ToolEndData{}, false
	}
	d, ok := e.Data.(ToolEndData)
	return d, ok
}

// EventSink is the callback used by the low-level loop to emit events.
type EventSink func(e Event)
