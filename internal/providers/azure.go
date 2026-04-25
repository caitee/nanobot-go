package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AzureProvider implements LLMProvider for Azure OpenAI
type AzureProvider struct {
	Endpoint     string // e.g., https://xxx.openai.azure.com/openai/deployments/gpt-4
	APIKey       string
	APIVersion   string
	DefaultModel string
	HTTPClient   *http.Client
}

// NewAzureProvider creates a new Azure OpenAI provider
func NewAzureProvider(endpoint, apiKey, apiVersion, defaultModel string) *AzureProvider {
	return &AzureProvider{
		Endpoint:     endpoint,
		APIKey:       apiKey,
		APIVersion:   apiVersion,
		DefaultModel: defaultModel,
		HTTPClient:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *AzureProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
	model := opts.Model
	if model == "" {
		model = p.DefaultModel
	}

	// Azure URL: https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version={version}
	url := fmt.Sprintf("%s/chat/completions?api-version=%s", p.Endpoint, p.APIVersion)

	reqBody := map[string]any{
		"messages": messages,
	}

	// Azure API 2024-10-21 uses max_completion_tokens instead of max_tokens
	if opts.MaxTokens > 0 {
		reqBody["max_completion_tokens"] = opts.MaxTokens
	}
	if opts.Temperature > 0 {
		reqBody["temperature"] = opts.Temperature
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
		reqBody["tool_choice"] = "auto"
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", p.APIKey)

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Azure OpenAI API error: status %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return p.parseResponse(result)
}

func (p *AzureProvider) parseResponse(result map[string]any) (*LLMResponse, error) {
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice, ok := choices[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid choice format")
	}

	msg, ok := choice["message"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("no message in choice")
	}

	content, _ := msg["content"].(string)

	resp := &LLMResponse{
		Content: content,
	}

	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tc := range toolCalls {
			if tcMap, ok := tc.(map[string]any); ok {
				funcMap, ok := tcMap["function"].(map[string]any)
				if !ok {
					continue
				}
				argsStr, ok := funcMap["arguments"].(string)
				if !ok {
					continue
				}
				var argsMap map[string]any
				if err := json.Unmarshal([]byte(argsStr), &argsMap); err != nil {
					continue
				}

				id, _ := tcMap["id"].(string)
				name, _ := funcMap["name"].(string)
				resp.ToolCalls = append(resp.ToolCalls, ToolCall{
					ID:        id,
					Name:      name,
					Arguments: argsMap,
				})
			}
		}
	}

	if finishReason, ok := choice["finish_reason"].(string); ok {
		resp.FinishReason = finishReason
	}

	if usage, ok := result["usage"].(map[string]any); ok {
		if promptTokens, ok := usage["prompt_tokens"].(float64); ok {
			resp.Usage.PromptTokens = int(promptTokens)
		}
		if completionTokens, ok := usage["completion_tokens"].(float64); ok {
			resp.Usage.CompletionTokens = int(completionTokens)
		}
	}

	return resp, nil
}

func (p *AzureProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
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

func (p *AzureProvider) GetDefaultModel() string {
	return p.DefaultModel
}

// StreamGenerate fallback implementation - collects full response and sends as single chunk
func (p *AzureProvider) StreamGenerate(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) <-chan StreamResponse {
	ch := make(chan StreamResponse, 1)
	go func() {
		defer close(ch)
		resp, err := p.Chat(ctx, messages, tools, opts)
		if err != nil {
			ch <- StreamResponse{Error: err}
			return
		}
		emitStreamingResponse(ch, resp)
	}()
	return ch
}
