package llm

import (
	"context"
	"time"
)

// Role identifies the author of a Message.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
	RoleSystem     Role = "system"
)

// StopReason describes why generation halted.
type StopReason string

const (
	StopReasonStop    StopReason = "stop"
	StopReasonLength  StopReason = "length"
	StopReasonToolUse StopReason = "toolUse"
	StopReasonAborted StopReason = "aborted"
	StopReasonError   StopReason = "error"
)

// Usage captures token accounting reported by a provider.
type Usage struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
}

// Model carries the metadata needed to route and bill a generation.
type Model struct {
	ID            string
	Name          string
	API           string
	Provider      string
	BaseURL       string
	Reasoning     bool
	ContextWindow int
	MaxTokens     int
	Cost          Cost
	Headers       map[string]string
}

// Cost is per-million-token pricing for a model.
type Cost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// Content is the sealed interface implemented by all content block variants.
// Callers discriminate via type switch:
//
//	switch c := content.(type) {
//	case TextContent:    ...
//	case ImageContent:   ...
//	case ThinkingContent: ...
//	case ToolCallContent: ...
//	}
type Content interface {
	contentMarker()
}

// TextContent is a plain-text block.
type TextContent struct {
	Text      string
	Signature string
}

func (TextContent) contentMarker() {}

// ImageContent carries a base64-encoded image.
type ImageContent struct {
	Data     string
	MimeType string
}

func (ImageContent) contentMarker() {}

// ThinkingContent is a model's internal reasoning block.
type ThinkingContent struct {
	Thinking  string
	Signature string
	Redacted  bool
}

func (ThinkingContent) contentMarker() {}

// ToolCallContent represents a tool call requested by the model.
type ToolCallContent struct {
	ID        string
	Name      string
	Arguments map[string]any
}

func (ToolCallContent) contentMarker() {}

// Message is the common interface for user / assistant / toolResult messages
// that are visible to the LLM. Custom agent-only messages live in the runtime
// package and are filtered out by convertToLLM before a provider call.
type Message interface {
	MessageRole() Role
	MessageTimestamp() time.Time
	messageMarker()
}

// UserMessage is a turn authored by the end user.
type UserMessage struct {
	Content   []Content
	Timestamp time.Time
}

func (m UserMessage) MessageRole() Role           { return RoleUser }
func (m UserMessage) MessageTimestamp() time.Time { return m.Timestamp }
func (UserMessage) messageMarker()                {}

// AssistantMessage is a turn authored by the model.
type AssistantMessage struct {
	Content      []Content
	API          string
	Provider     string
	Model        string
	ResponseID   string
	Usage        Usage
	StopReason   StopReason
	ErrorMessage string
	Timestamp    time.Time
}

func (m AssistantMessage) MessageRole() Role           { return RoleAssistant }
func (m AssistantMessage) MessageTimestamp() time.Time { return m.Timestamp }
func (AssistantMessage) messageMarker()                {}

// ToolResultMessage carries the outcome of a tool invocation.
// Terminate is a runtime-only flag (never sent to the provider) that signals
// the agent loop to halt after the current tool batch when every result in
// the batch agrees.
type ToolResultMessage struct {
	ToolCallID string
	ToolName   string
	Content    []Content
	Details    any
	IsError    bool
	Terminate  bool
	Timestamp  time.Time
}

func (m ToolResultMessage) MessageRole() Role           { return RoleToolResult }
func (m ToolResultMessage) MessageTimestamp() time.Time { return m.Timestamp }
func (ToolResultMessage) messageMarker()                {}

// Tool is the LLM-facing tool definition (name, description, JSON schema).
// The full tool implementation (Execute, hooks, labels) lives in internal/tool.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Context is a snapshot passed to the provider for a single generation call.
type Context struct {
	SystemPrompt string
	Messages     []Message
	Tools        []Tool
}

// StreamOptions control a streaming call. Fields mirror pi-mono's
// SimpleStreamOptions / ProviderStreamOptions without transport specifics.
type StreamOptions struct {
	Temperature    float64
	MaxTokens      int
	ModelOverride  string
	Reasoning      string // "off" | "minimal" | "low" | "medium" | "high" | "xhigh"
	SessionID      string
	APIKey         string
	MaxRetryDelay  time.Duration
	ExtraHeaders   map[string]string
	ExtraBodyProps map[string]any
}

// StreamEventKind enumerates every event a provider may emit.
type StreamEventKind string

const (
	StreamEventStart         StreamEventKind = "start"
	StreamEventTextStart     StreamEventKind = "text_start"
	StreamEventTextDelta     StreamEventKind = "text_delta"
	StreamEventTextEnd       StreamEventKind = "text_end"
	StreamEventThinkingStart StreamEventKind = "thinking_start"
	StreamEventThinkingDelta StreamEventKind = "thinking_delta"
	StreamEventThinkingEnd   StreamEventKind = "thinking_end"
	StreamEventToolCallStart StreamEventKind = "toolcall_start"
	StreamEventToolCallDelta StreamEventKind = "toolcall_delta"
	StreamEventToolCallEnd   StreamEventKind = "toolcall_end"
	StreamEventDone          StreamEventKind = "done"
	StreamEventError         StreamEventKind = "error"
)

// StreamEvent is the unified event envelope. Fields are populated according to
// Kind; consumers switch on Kind and read the relevant fields.
type StreamEvent struct {
	Kind         StreamEventKind
	ContentIndex int

	// text_delta / thinking_delta / toolcall_delta carry incremental text.
	Delta string

	// text_end / thinking_end carry the fully accumulated content.
	Text     string
	Thinking string

	// toolcall_end carries the fully parsed tool call.
	ToolCall *ToolCallContent

	// Every event carries a pointer to the in-progress assistant message so
	// consumers can render partial state without accumulating deltas manually.
	Partial *AssistantMessage

	// done / error carry the final assistant message.
	Message *AssistantMessage

	// done / error carry a stop reason; error additionally populates ErrorMessage.
	StopReason   StopReason
	ErrorMessage string
}

// EventStream is the channel of stream events delivered by a provider.
// The channel must be closed by the producer after emitting either a Done or
// Error event — those two kinds are terminal.
type EventStream <-chan StreamEvent

// StreamFn is the single entry point for running a generation. Implementations
// must not panic and must never return a nil channel; runtime failures should
// be encoded as a terminal StreamEventError followed by channel close.
type StreamFn func(ctx context.Context, model Model, c Context, opts StreamOptions) EventStream

// Provider is what a concrete provider implementation registers under an API
// name. StreamFn is required; additional capabilities can be added later.
type Provider interface {
	Stream(ctx context.Context, model Model, c Context, opts StreamOptions) EventStream
}
