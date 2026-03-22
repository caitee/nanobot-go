package providers

import "context"

// Message represents a chat message
type Message struct {
    Role    string
    Content any // string or []ContentBlock
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
    Temperature    float64
    MaxTokens      int
    Model          string
    ReasoningEffort string
}

// LLMProvider is the interface for LLM providers
type LLMProvider interface {
    Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error)
    ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error)
    GetDefaultModel() string
}
