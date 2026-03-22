package providers

import (
    "context"
    "fmt"
    "net/http"
    "time"
)

// AzureProvider implements LLMProvider for Azure OpenAI
type AzureProvider struct {
    Endpoint    string // e.g., https://xxx.openai.azure.com/openai/deployments/gpt-4
    APIKey     string
    APIVersion string
    HTTPClient *http.Client
}

// NewAzureProvider creates a new Azure OpenAI provider
func NewAzureProvider(endpoint, apiKey, apiVersion string) *AzureProvider {
    return &AzureProvider{
        Endpoint:    endpoint,
        APIKey:      apiKey,
        APIVersion:  apiVersion,
        HTTPClient: &http.Client{Timeout: 60 * time.Second},
    }
}

func (p *AzureProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
    // Azure uses OpenAI-compatible endpoint
    openaiProvider := &OpenAIProvider{
        BaseURL:     p.Endpoint,
        APIKey:      p.APIKey,
        DefaultModel: opts.Model,
        HTTPClient:  p.HTTPClient,
    }

    reqBody := map[string]any{
        "messages": messages,
    }
    if opts.MaxTokens > 0 {
        reqBody["max_tokens"] = opts.MaxTokens
    }
    if opts.Temperature > 0 {
        reqBody["temperature"] = opts.Temperature
    }
    if len(tools) > 0 {
        reqBody["tools"] = tools
    }

    // Azure requires api-version query param
    _ = fmt.Sprintf("%s/chat/completions?api-version=%s", p.Endpoint, p.APIVersion)
    return openaiProvider.Chat(ctx, messages, tools, opts)
}

func (p *AzureProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
    return p.Chat(ctx, messages, tools, opts)
}

func (p *AzureProvider) GetDefaultModel() string {
    return ""
}
