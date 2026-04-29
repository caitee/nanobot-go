package bus

import "time"

// InboundMessage represents a message received from a chat channel
type InboundMessage struct {
	Channel    string
	SenderID   string
	ChatID     string
	Content    string
	Timestamp  time.Time
	Media      []string
	Metadata   map[string]any
	SessionKey string
}

// OutboundMessage represents a message to send to a chat channel
type OutboundMessage struct {
	Channel   string
	ChatID    string
	Content   string
	ReplyTo   string
	Media     []string
	Reasoning string
	Metadata  map[string]any
}

const (
	// OutboundMetadataAgentEventFinal marks an outbound response whose final
	// content was already published through EventLLMFinal for rich UIs.
	OutboundMetadataAgentEventFinal = "agent_event_final"
)

// AgentEvent represents a detailed agent state event for UI updates
type AgentEvent struct {
	SessionKey string
	Type       AgentEventType
	Timestamp  time.Time
	Data       any // Event-specific payload
}

// StreamChunk returns the payload for EventLLMStreamChunk.
func (e AgentEvent) StreamChunk() (StreamChunkData, bool) {
	if e.Type != EventLLMStreamChunk {
		return StreamChunkData{}, false
	}
	data, ok := e.Data.(StreamChunkData)
	return data, ok
}

// LLMFinal returns the payload for EventLLMFinal.
func (e AgentEvent) LLMFinal() (LLMFinalData, bool) {
	if e.Type != EventLLMFinal {
		return LLMFinalData{}, false
	}
	data, ok := e.Data.(LLMFinalData)
	return data, ok
}

// ToolCalls returns the payload for EventLLMToolCalls.
func (e AgentEvent) ToolCalls() ([]ToolCallInfo, bool) {
	if e.Type != EventLLMToolCalls {
		return nil, false
	}
	data, ok := e.Data.([]ToolCallInfo)
	return data, ok
}

// ToolCall returns the payload for EventToolStart.
func (e AgentEvent) ToolCall() (ToolCallInfo, bool) {
	if e.Type != EventToolStart {
		return ToolCallInfo{}, false
	}
	data, ok := e.Data.(ToolCallInfo)
	return data, ok
}

// ToolResult returns the payload for EventToolEnd and EventToolError.
func (e AgentEvent) ToolResult() (ToolResultEventData, bool) {
	if e.Type != EventToolEnd && e.Type != EventToolError {
		return ToolResultEventData{}, false
	}
	data, ok := e.Data.(ToolResultEventData)
	return data, ok
}

// SessionEnd returns the payload for EventSessionEnd.
func (e AgentEvent) SessionEnd() (SessionEndData, bool) {
	if e.Type != EventSessionEnd {
		return SessionEndData{}, false
	}
	data, ok := e.Data.(SessionEndData)
	return data, ok
}

// AgentEventType is the enum-like string type for agent event kinds.
type AgentEventType string

// Agent event type constants.
const (
	// LLM related events
	EventLLMThinking    AgentEventType = "llm_thinking"     // Agent started thinking
	EventLLMResponding  AgentEventType = "llm_responding"   // Agent started generating response
	EventLLMStreamChunk AgentEventType = "llm_stream_chunk" // Streaming content chunk received
	EventLLMStreamEnd   AgentEventType = "llm_stream_end"   // Streaming ended
	EventLLMToolCalls   AgentEventType = "llm_tool_calls"   // LLM requested tool calls
	EventLLMFinal       AgentEventType = "llm_final"        // Final response completed

	// Tool related events
	EventToolStart    AgentEventType = "tool_start"    // Tool execution started
	EventToolProgress AgentEventType = "tool_progress" // Tool execution progress update
	EventToolEnd      AgentEventType = "tool_end"      // Tool execution completed
	EventToolError    AgentEventType = "tool_error"    // Tool execution failed

	// Session related events
	EventSessionStart    AgentEventType = "session_start"    // Session started
	EventSessionEnd      AgentEventType = "session_end"      // Session ended
	EventCommandReceived AgentEventType = "command_received" // Command received (e.g., /help, /stop)
)

// StreamChunkData holds streaming response chunk data
type StreamChunkData struct {
	Delta       string // Incremental text
	FullText    string // Accumulated complete text
	IsReasoning bool   // True if this chunk is reasoning content
}

// ToolCallInfo represents a single tool call request
type ToolCallInfo struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResultEventData holds tool execution result data
type ToolResultEventData struct {
	ToolName   string
	ToolID     string
	Success    bool
	Result     string // Tool execution output
	Error      string
	DurationMs int64 // Execution duration in milliseconds
}

// LLMFinalData holds the final LLM response after all tool calls complete
type LLMFinalData struct {
	Content          string
	ReasoningContent string // Separate thinking/reasoning content if available
	Error            string // Non-empty when generation failed
}

// SessionEndData holds session completion metadata.
type SessionEndData struct {
	Cancelled bool
}
