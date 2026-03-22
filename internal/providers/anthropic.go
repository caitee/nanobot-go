package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AnthropicProvider implements LLMProvider for Anthropic Claude API
type AnthropicProvider struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	HTTPClient   *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey, baseURL, defaultModel string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &AnthropicProvider{
		APIKey:       apiKey,
		BaseURL:      baseURL,
		DefaultModel: defaultModel,
		HTTPClient:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (p *AnthropicProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
	model := opts.Model
	if model == "" {
		model = p.DefaultModel
	}

	// Extract system message for Anthropic (expects max one system message at start)
	var systemContent string
	anthropicMsgs := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			if systemContent == "" {
				systemContent, _ = msg.Content.(string)
			}
			continue
		}
		anthropicMsgs = append(anthropicMsgs, map[string]any{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	reqBody := map[string]any{
		"model":      model,
		"messages":   anthropicMsgs,
		"max_tokens": opts.MaxTokens,
	}

	if systemContent != "" {
		reqBody["system"] = systemContent
	}

	if opts.Temperature > 0 {
		reqBody["temperature"] = opts.Temperature
	}

	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	if opts.ReasoningEffort != "" {
		reqBody["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": parseReasoningEffort(opts.ReasoningEffort),
		}
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic API error: status %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	content := ""
	var toolCalls []ToolCall
	reasoningContent := ""
	if contentArr, ok := result["content"].([]any); ok {
		for _, block := range contentArr {
			if blockMap, ok := block.(map[string]any); ok {
				switch blockMap["type"] {
				case "text":
					if text, ok := blockMap["text"].(string); ok {
						content = text
					}
				case "thinking":
					if thinking, ok := blockMap["thinking"].(string); ok {
						reasoningContent = thinking
					}
				case "tool_use":
					id, _ := blockMap["id"].(string)
					name, _ := blockMap["name"].(string)
					input, _ := blockMap["input"].(map[string]any)
					toolCalls = append(toolCalls, ToolCall{
						ID:        id,
						Name:      name,
						Arguments: input,
					})
				}
			}
		}
	}

	finishReason := ""
	if sr, ok := result["stop_reason"].(string); ok {
		finishReason = sr
	}

	resp2 := &LLMResponse{
		Content:          content,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		ReasoningContent: reasoningContent,
	}

	if usage, ok := result["usage"].(map[string]any); ok {
		if pt, ok := usage["input_tokens"].(float64); ok {
			resp2.Usage.PromptTokens = int(pt)
		}
		if ct, ok := usage["output_tokens"].(float64); ok {
			resp2.Usage.CompletionTokens = int(ct)
		}
	}

	return resp2, nil
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

// parseReasoningEffort converts effort string to token budget
func parseReasoningEffort(effort string) int {
	switch effort {
	case "low":
		return 1024
	case "medium":
		return 4096
	case "high":
		return 8192
	default:
		return 4096
	}
}
