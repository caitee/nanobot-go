package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenAIProvider implements LLMProvider for OpenAI-compatible APIs
type OpenAIProvider struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(baseURL, apiKey, defaultModel string) *OpenAIProvider {
	return &OpenAIProvider{
		BaseURL:      baseURL,
		APIKey:       apiKey,
		DefaultModel: defaultModel,
		HTTPClient:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
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

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/chat/completions", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return p.parseResponse(result)
}

func (p *OpenAIProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
	retryCfg := opts.RetryConfig
	if retryCfg == nil {
		retryCfg = &RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   time.Second,
			MaxDelay:    10 * time.Second,
		}
	}
	return ChatWithRetryConfig(ctx, p, messages, tools, opts, *retryCfg)
}

func (p *OpenAIProvider) GetDefaultModel() string {
	return p.DefaultModel
}

// StreamGenerate fallback implementation - collects full response and sends as single chunk
func (p *OpenAIProvider) StreamGenerate(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) <-chan StreamResponse {
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

func emitStreamingResponse(ch chan<- StreamResponse, resp *LLMResponse) {
	final := StreamResponse{
		Done:             true,
		Content:          resp.Content,
		ToolCalls:        resp.ToolCalls,
		FinishReason:     resp.FinishReason,
		Usage:            resp.Usage,
		ReasoningContent: resp.ReasoningContent,
	}

	lines := splitIntoLines(resp.Content)
	for _, line := range lines[:max(len(lines)-1, 0)] {
		ch <- StreamResponse{Chunk: line, Done: false}
	}
	if len(lines) > 0 {
		final.Chunk = lines[len(lines)-1]
	}
	ch <- final
}

func splitIntoLines(s string) []string {
	if s == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	if len(lines) == 0 {
		lines = []string{s}
	}
	return lines
}

func (p *OpenAIProvider) parseResponse(result map[string]any) (*LLMResponse, error) {
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
