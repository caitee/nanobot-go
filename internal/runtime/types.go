package runtime

import (
	"context"
	"time"

	"nanobot-go/internal/llm"
	"nanobot-go/internal/tool"
)

// AgentMessage is the union visible to the agent runtime. It includes every
// LLM-facing message (via llm.Message) plus custom agent-only messages that
// are filtered out before a provider call.
//
// Implementations must also satisfy llm.Message when they want to be sent to
// the model; purely presentational messages can implement only AgentMessage.
type AgentMessage interface {
	AgentRole() string
	AgentTimestamp() time.Time
}

// llmAgentMessage wraps any llm.Message as an AgentMessage.
type llmAgentMessage struct{ m llm.Message }

func (w llmAgentMessage) AgentRole() string         { return string(w.m.MessageRole()) }
func (w llmAgentMessage) AgentTimestamp() time.Time { return w.m.MessageTimestamp() }

// WrapLLM lifts an llm.Message into the AgentMessage universe.
func WrapLLM(m llm.Message) AgentMessage { return llmAgentMessage{m: m} }

// Unwrap returns the underlying llm.Message if msg was produced by WrapLLM.
func Unwrap(msg AgentMessage) (llm.Message, bool) {
	w, ok := msg.(llmAgentMessage)
	if !ok {
		return nil, false
	}
	return w.m, true
}

// Options configure a new Agent. Fields map directly to pi-mono AgentOptions.
type Options struct {
	SystemPrompt   string
	Model          llm.Model
	ThinkingLevel  string
	Tools          []tool.AgentTool
	InitialHistory []AgentMessage

	StreamFn         llm.StreamFn
	ConvertToLLM     ConvertToLLM
	TransformContext TransformContext
	GetAPIKey        GetAPIKey
	ShouldStopAfter  ShouldStopAfter
	BeforeToolCall   BeforeToolCall
	AfterToolCall    AfterToolCall

	ToolExecution tool.ExecutionMode
	SteeringMode  QueueMode
	FollowUpMode  QueueMode

	SessionID string
}

// QueueMode controls how a PendingMessageQueue drains.
type QueueMode string

const (
	QueueAll         QueueMode = "all"
	QueueOneAtAtTime QueueMode = "one-at-a-time"
	QueueDefaultMode           = QueueOneAtAtTime
)

// ConvertToLLM converts AgentMessage[] to llm.Message[] before a provider call.
// Non-LLM messages (purely presentational) must be filtered out here.
type ConvertToLLM func(msgs []AgentMessage) ([]llm.Message, error)

// TransformContext runs before ConvertToLLM and works at AgentMessage level:
// context window pruning, external context injection, etc.
type TransformContext func(ctx context.Context, msgs []AgentMessage) ([]AgentMessage, error)

// GetAPIKey resolves an API key dynamically (e.g., short-lived OAuth tokens).
type GetAPIKey func(ctx context.Context, provider string) (string, error)

// ShouldStopContext carries the data passed to ShouldStopAfter.
type ShouldStopContext struct {
	Assistant   llm.AssistantMessage
	ToolResults []llm.ToolResultMessage
	Transcript  []AgentMessage
	NewMessages []AgentMessage
}

// ShouldStopAfter can request a graceful halt after the current turn.
type ShouldStopAfter func(ctx context.Context, c ShouldStopContext) (bool, error)

// BeforeToolCallContext is passed to the pre-execution hook.
type BeforeToolCallContext struct {
	Assistant llm.AssistantMessage
	ToolCall  llm.ToolCallContent
	Args      map[string]any
}

// BeforeToolCallResult optionally blocks execution.
type BeforeToolCallResult struct {
	Block  bool
	Reason string
}

// BeforeToolCall is called after schema validation, before Execute.
type BeforeToolCall func(ctx context.Context, c BeforeToolCallContext) (*BeforeToolCallResult, error)

// AfterToolCallContext is passed to the post-execution hook.
type AfterToolCallContext struct {
	Assistant llm.AssistantMessage
	ToolCall  llm.ToolCallContent
	Args      map[string]any
	Result    *tool.Result
	IsError   bool
}

// AfterToolCallResult can override any field on the executed result.
// Fields that are nil leave the original value untouched.
type AfterToolCallResult struct {
	Content   []llm.Content // nil means "keep original"
	Details   any           // nil means "keep original"
	IsError   *bool         // nil means "keep original"
	Terminate *bool         // nil means "keep original"
}

// AfterToolCall runs after Execute, before tool_execution_end is emitted.
type AfterToolCall func(ctx context.Context, c AfterToolCallContext) (*AfterToolCallResult, error)
