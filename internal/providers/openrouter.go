package providers

import (
    "context"
    "net/http"
    "time"
)

// OpenRouterProvider implements LLMProvider for OpenRouter
type OpenRouterProvider struct {
    APIKey      string
    BaseURL     string
    DefaultModel string
    HTTPClient  *http.Client
}

// NewOpenRouterProvider creates a new OpenRouter provider
func NewOpenRouterProvider(apiKey, defaultModel string) *OpenRouterProvider {
    return &OpenRouterProvider{
        APIKey:      apiKey,
        BaseURL:     "https://openrouter.ai/api/v1",
        DefaultModel: defaultModel,
        HTTPClient: &http.Client{Timeout: 60 * time.Second},
    }
}

func (p *OpenRouterProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
    model := opts.Model
    if model == "" {
        model = p.DefaultModel
    }

    reqBody := map[string]any{
        "model":    model,
        "messages": messages,
    }

    if len(tools) > 0 {
        reqBody["tools"] = tools
    }
    if opts.MaxTokens > 0 {
        reqBody["max_tokens"] = opts.MaxTokens
    }
    if opts.Temperature > 0 {
        reqBody["temperature"] = opts.Temperature
    }

    openaiProvider := &OpenAIProvider{
        BaseURL:     p.BaseURL,
        APIKey:      p.APIKey,
        DefaultModel: model,
        HTTPClient:  &http.Client{Timeout: 60 * time.Second},
    }

    return openaiProvider.Chat(ctx, messages, tools, opts)
}

func (p *OpenRouterProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
    var lastErr error
    for i := 0; i < 3; i++ {
        resp, err := p.Chat(ctx, messages, tools, opts)
        if err == nil {
            return resp, nil
        }
        lastErr = err
        time.Sleep(time.Duration(i+1) * time.Second)
    }
    return nil, lastErr
}

func (p *OpenRouterProvider) GetDefaultModel() string {
    return p.DefaultModel
}

// StreamGenerate fallback - delegates to OpenAI provider
func (p *OpenRouterProvider) StreamGenerate(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) <-chan StreamResponse {
    openaiProvider := &OpenAIProvider{
        BaseURL:     p.BaseURL,
        APIKey:      p.APIKey,
        DefaultModel: opts.Model,
        HTTPClient:  p.HTTPClient,
    }
    return openaiProvider.StreamGenerate(ctx, messages, tools, opts)
}
