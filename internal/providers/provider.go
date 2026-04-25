package providers

import (
	"context"
	"encoding/json"
)

// Message represents a chat message
type Message struct {
	Role       string
	Content    any        // string or []ContentBlock
	ToolCalls  []ToolCall // for assistant messages
	ToolCallID string     // for tool result messages
	Name       string     // for tool result messages
}

// ContentBlock represents multimodal content
type ContentBlock struct {
	Type     string
	Text     string
	ImageURL string
}

// ToolCall represents a tool call request from LLM
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolDef represents a tool definition for LLM
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema
}

// LLMResponse represents a response from LLM
type LLMResponse struct {
	Content          string
	ToolCalls        []ToolCall
	FinishReason     string
	Usage            TokenUsage
	ReasoningContent string
}

// TokenUsage represents token usage statistics
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
}

// ChatOptions for LLM calls
type ChatOptions struct {
	Temperature     float64
	MaxTokens       int
	Model           string
	ReasoningEffort string
	RetryConfig     *RetryConfig // Optional retry configuration
}

// LLMProvider is the interface for LLM providers
type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error)
	ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error)
	StreamGenerate(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) <-chan StreamResponse
	GetDefaultModel() string
}

// StreamResponse represents a chunk of streaming response
type StreamResponse struct {
	Chunk            string // Incremental text content
	Done             bool   // True if this is the final chunk
	Error            error
	IsReasoning      bool // True if this chunk is reasoning/thinking content (not final answer)
	Content          string
	ToolCalls        []ToolCall
	FinishReason     string
	Usage            TokenUsage
	ReasoningContent string
}

// ToMap converts a Message to map[string]any for compatibility.
func (m *Message) ToMap() map[string]any {
	result := map[string]any{
		"role": m.Role,
	}
	switch c := m.Content.(type) {
	case string:
		result["content"] = c
	case []ContentBlock:
		blocks := make([]map[string]any, 0, len(c))
		for _, b := range c {
			block := map[string]any{"type": b.Type}
			if b.Text != "" {
				block["text"] = b.Text
			}
			if b.ImageURL != "" {
				block["image_url"] = b.ImageURL
			}
			blocks = append(blocks, block)
		}
		result["content"] = blocks
	default:
		data, _ := json.Marshal(c)
		result["content"] = string(data)
	}
	if m.ToolCallID != "" {
		result["tool_call_id"] = m.ToolCallID
	}
	if m.Name != "" {
		result["name"] = m.Name
	}
	if len(m.ToolCalls) > 0 {
		result["tool_calls"] = m.ToolCalls
	}
	return result
}
