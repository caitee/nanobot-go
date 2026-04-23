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
	Channel  string
	ChatID   string
	Content  string
	ReplyTo  string
	Media    []string
	Reasoning    string
	Metadata   map[string]any
}

// ToolEvent represents a tool execution event for UI updates
type ToolEvent struct {
	SessionKey string
	Type       string // "tool_start" | "tool_end" | "tool_error"
	ToolName   string
	ToolID     string
	Args       string
	Result     string
}

// AgentEvent represents a detailed agent state event for UI updates
type AgentEvent struct {
	SessionKey string
	Type      string         // Event type (see agent/events.go constants)
	Timestamp time.Time
	Data      map[string]any // Event-specific data
}

// StreamChunkData holds streaming response chunk data
type StreamChunkData struct {
	Delta       string // Incremental text
	FullText    string // Accumulated complete text
	IsReasoning bool   // True if this chunk is reasoning content
}

// ToolCallEventData holds data when LLM requests tool calls
type ToolCallEventData struct {
	ToolCalls []ToolCallInfo
}

// ToolCallInfo represents a single tool call request
type ToolCallInfo struct {
	ID       string
	Name     string
	Args     map[string]any
}

// ToolResultEventData holds tool execution result data
type ToolResultEventData struct {
	ToolName   string
	ToolID     string
	Success    bool
	Result     string // Not displayed by default, only on demand
	Error      string
	DurationMs int64 // Execution duration in milliseconds
}

// LLMFinalData holds the final LLM response after all tool calls complete
type LLMFinalData struct {
	Content          string
	ReasoningContent string // Separate thinking/reasoning content if available
}

// ErrorData holds error information
type ErrorData struct {
	Message string
}
