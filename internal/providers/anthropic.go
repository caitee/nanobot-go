package providers

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "time"
)

// AnthropicProvider implements LLMProvider for Anthropic Claude API
type AnthropicProvider struct {
    APIKey      string
    BaseURL     string
    DefaultModel string
    HTTPClient  *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey, baseURL, defaultModel string) *AnthropicProvider {
    if baseURL == "" {
        baseURL = "https://api.anthropic.com"
    }
    return &AnthropicProvider{
        APIKey:      apiKey,
        BaseURL:     baseURL,
        DefaultModel: defaultModel,
        HTTPClient: &http.Client{Timeout: 60 * time.Second},
    }
}

func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
    model := opts.Model
    if model == "" {
        model = p.DefaultModel
    }

    // Convert messages to Anthropic format
    anthropicMsgs := make([]map[string]any, 0, len(messages))
    for _, msg := range messages {
        if msg.Role == "system" {
            continue // System messages handled separately
        }
        anthropicMsgs = append(anthropicMsgs, map[string]any{
            "role":    msg.Role,
            "content": msg.Content,
        })
    }

    reqBody := map[string]any{
        "model":       model,
        "messages":    anthropicMsgs,
        "max_tokens":   opts.MaxTokens,
    }

    if opts.Temperature > 0 {
        reqBody["temperature"] = opts.Temperature
    }

    if len(tools) > 0 {
        reqBody["tools"] = tools
    }

    jsonBody, _ := json.Marshal(reqBody)
    req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewBuffer(jsonBody))
    if err != nil {
        return nil, err
    }

    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("x-api-key", p.APIKey)
    req.Header.Set("anthropic-version", "2023-06-01")

    resp, err := p.HTTPClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result map[string]any
    json.NewDecoder(resp.Body).Decode(&result)

    content := ""
    if contentArr, ok := result["content"].([]any); ok {
        for _, block := range contentArr {
            if blockMap, ok := block.(map[string]any); ok {
                if blockMap["type"] == "text" {
                    content = blockMap["text"].(string)
                }
            }
        }
    }

    return &LLMResponse{
        Content:      content,
        FinishReason: result["stop_reason"].(string),
    }, nil
}

func (p *AnthropicProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
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

func (p *AnthropicProvider) GetDefaultModel() string {
    return p.DefaultModel
}
