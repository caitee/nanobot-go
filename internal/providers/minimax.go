package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// MinimaxProvider implements LLMProvider for MiniMax Anthropic-compatible API
type MinimaxProvider struct {
	APIKey       string
	BaseURL      string
	DefaultModel string
	HTTPClient   *http.Client
}

// NewMinimaxProvider creates a new MiniMax provider
func NewMinimaxProvider(apiKey, baseURL, defaultModel string) *MinimaxProvider {
	if baseURL == "" {
		baseURL = "https://api.minimaxi.com/anthropic"
	}
	return &MinimaxProvider{
		APIKey:       apiKey,
		BaseURL:      baseURL,
		DefaultModel: defaultModel,
		HTTPClient:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *MinimaxProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
	model := opts.Model
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		model = "MiniMax-M2.7"
	}

	// Extract system message for Anthropic (expects max one system message at start)
	var systemContent string
	minimaxMsgs := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "system" {
			if systemContent == "" {
				systemContent, _ = msg.Content.(string)
			}
			continue
		}

		// MiniMax API expects content as array of content blocks
		var contentBlocks []map[string]any

		switch c := msg.Content.(type) {
		case string:
			if c != "" {
				if msg.Role == "tool" {
					// Tool result messages need tool_result content block with tool_use_id
					contentBlocks = []map[string]any{{"type": "tool_result", "tool_use_id": msg.ToolCallID, "content": c}}
				} else {
					contentBlocks = []map[string]any{{"type": "text", "text": c}}
				}
			} else {
				contentBlocks = []map[string]any{}
			}
		case []ContentBlock:
			for _, b := range c {
				block := map[string]any{"type": b.Type}
				if b.Text != "" {
					block["text"] = b.Text
				}
				if b.ImageURL != "" {
					block["image_url"] = b.ImageURL
				}
				contentBlocks = append(contentBlocks, block)
			}
		case []any:
			// Already in correct format (from JSON unmarshal)
			contentBlocks = make([]map[string]any, 0, len(c))
			for _, b := range c {
				if blockMap, ok := b.(map[string]any); ok {
					contentBlocks = append(contentBlocks, blockMap)
				}
			}
		case nil:
			contentBlocks = []map[string]any{}
		default:
			contentBlocks = []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", c)}}
		}

		// Add tool_use blocks from ToolCalls (assistant messages with tool calls)
		for _, tc := range msg.ToolCalls {
			toolUse := map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Name,
				"input": tc.Arguments,
			}
			contentBlocks = append(contentBlocks, toolUse)
		}

		content := contentBlocks

		role := msg.Role
		// For tool results, Anthropic/MiniMax expects role "user" with tool_use_id in content
		minimaxMsg := map[string]any{
			"role":    role,
			"content": content,
		}
		if msg.Role == "tool" {
			minimaxMsg["role"] = "user"
			// tool_use_id is already set in the tool_result content block at line 62
			log.Printf("DEBUG tool result: role=%s tool_call_id=%s", role, msg.ToolCallID)
		}
		minimaxMsgs = append(minimaxMsgs, minimaxMsg)
	}

	reqBody := map[string]any{
		"model":      model,
		"messages":   minimaxMsgs,
		"max_tokens": opts.MaxTokens,
	}

	if systemContent != "" {
		reqBody["system"] = systemContent
	}

	if opts.Temperature > 0 {
		reqBody["temperature"] = opts.Temperature
	}

	// Enable extended thinking/thinking blocks
	if opts.ReasoningEffort != "" {
		reqBody["thinking_effort"] = opts.ReasoningEffort
	} else {
		reqBody["thinking_effort"] = "low"
	}
	if len(tools) > 0 {
		// Transform tools to MiniMax/Anthropic format
		validTools := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			if t.Name == "" {
				continue
			}
			params := t.Parameters
			if params == nil {
				params = map[string]any{"type": "object"}
			}
			validTools = append(validTools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": params,
			})
		}
		if len(validTools) > 0 {
			reqBody["tools"] = validTools
		}
	}

	jsonBody, _ := json.Marshal(reqBody)
	log.Printf("minimax request: url=%s body=%s", p.BaseURL+"/v1/messages", string(jsonBody))
	req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("minimax API error: status %d, body: %s", resp.StatusCode, string(body))
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
					log.Printf("DEBUG tool_use block: id=%s name=%s input=%v", id, name, input)
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

func (p *MinimaxProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
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

func (p *MinimaxProvider) GetDefaultModel() string {
	return p.DefaultModel
}

type minimaxStreamToolCall struct {
	ID        string
	Name      string
	InputJSON string
	Arguments map[string]any
}

func finalizeMinimaxStreamToolCalls(blocks map[int]*minimaxStreamToolCall, order []int) []ToolCall {
	toolCalls := make([]ToolCall, 0, len(order))
	for _, index := range order {
		block := blocks[index]
		if block == nil || block.Name == "" {
			continue
		}

		args := block.Arguments
		if args == nil {
			args = map[string]any{}
			if strings.TrimSpace(block.InputJSON) != "" {
				if err := json.Unmarshal([]byte(block.InputJSON), &args); err != nil {
					log.Printf("failed to parse streamed tool input: id=%s name=%s input=%q error=%v", block.ID, block.Name, block.InputJSON, err)
				}
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:        block.ID,
			Name:      block.Name,
			Arguments: args,
		})
	}
	return toolCalls
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

// StreamGenerate implements streaming response for MiniMax API using SSE
func (p *MinimaxProvider) StreamGenerate(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) <-chan StreamResponse {
	ch := make(chan StreamResponse, 100)
	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				ch <- StreamResponse{Error: fmt.Errorf("panic recovered: %v", r)}
			}
		}()

		// MiniMax streaming uses SSE with content_block_delta format
		// StreamGenerate will handle both flat format and event-wrapped format

		model := opts.Model
		if model == "" {
			model = p.DefaultModel
		}
		if model == "" {
			model = "MiniMax-M2.7"
		}

		// Extract system message for Anthropic
		var systemContent string
		minimaxMsgs := make([]map[string]any, 0, len(messages))
		for _, msg := range messages {
			if msg.Role == "system" {
				if systemContent == "" {
					systemContent, _ = msg.Content.(string)
				}
				continue
			}

			var contentBlocks []map[string]any

			switch c := msg.Content.(type) {
			case string:
				if c != "" {
					if msg.Role == "tool" {
						contentBlocks = []map[string]any{{"type": "tool_result", "tool_use_id": msg.ToolCallID, "content": c}}
					} else {
						contentBlocks = []map[string]any{{"type": "text", "text": c}}
					}
				} else {
					contentBlocks = []map[string]any{}
				}
			case []ContentBlock:
				for _, b := range c {
					block := map[string]any{"type": b.Type}
					if b.Text != "" {
						block["text"] = b.Text
					}
					if b.ImageURL != "" {
						block["image_url"] = b.ImageURL
					}
					contentBlocks = append(contentBlocks, block)
				}
			case nil:
				contentBlocks = []map[string]any{}
			default:
				contentBlocks = []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", c)}}
			}

			// Add tool_use blocks from ToolCalls
			for _, tc := range msg.ToolCalls {
				toolUse := map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": tc.Arguments,
				}
				contentBlocks = append(contentBlocks, toolUse)
			}

			role := msg.Role
			minimaxMsg := map[string]any{
				"role":    role,
				"content": contentBlocks,
			}
			if msg.Role == "tool" {
				minimaxMsg["role"] = "user"
			}
			minimaxMsgs = append(minimaxMsgs, minimaxMsg)
		}

		reqBody := map[string]any{
			"model":      model,
			"messages":   minimaxMsgs,
			"max_tokens": opts.MaxTokens,
			"stream":     true,
		}

		if systemContent != "" {
			reqBody["system"] = systemContent
		}

		if opts.Temperature > 0 {
			reqBody["temperature"] = opts.Temperature
		}

		// Enable extended thinking/thinking blocks streaming
		if opts.ReasoningEffort != "" {
			reqBody["thinking_effort"] = opts.ReasoningEffort
		} else {
			reqBody["thinking_effort"] = "low"
		}

		if len(tools) > 0 {
			validTools := make([]map[string]any, 0, len(tools))
			for _, t := range tools {
				if t.Name == "" {
					continue
				}
				params := t.Parameters
				if params == nil {
					params = map[string]any{"type": "object"}
				}
				validTools = append(validTools, map[string]any{
					"name":         t.Name,
					"description":  t.Description,
					"input_schema": params,
				})
			}
			if len(validTools) > 0 {
				reqBody["tools"] = validTools
			}
		}

		jsonBody, _ := json.Marshal(reqBody)
		log.Printf("minimax stream request: url=%s", p.BaseURL+"/v1/messages")

		req, err := http.NewRequestWithContext(ctx, "POST", p.BaseURL+"/v1/messages", bytes.NewBuffer(jsonBody))
		if err != nil {
			ch <- StreamResponse{Error: err}
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := p.HTTPClient.Do(req)
		if err != nil {
			ch <- StreamResponse{Error: err}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			ch <- StreamResponse{Error: fmt.Errorf("minimax stream API error: status %d, body: %s", resp.StatusCode, string(body))}
			return
		}

		// Read SSE stream using bufio.Scanner for line-by-line processing
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 4096), 1024*1024) // 1MB max token size
		var fullText string
		var reasoningText string
		var finishReason string
		toolCallBlocks := make(map[int]*minimaxStreamToolCall)
		toolCallOrder := make([]int, 0)
		doneResponse := func() StreamResponse {
			return StreamResponse{
				Done:             true,
				Content:          fullText,
				ToolCalls:        finalizeMinimaxStreamToolCalls(toolCallBlocks, toolCallOrder),
				FinishReason:     finishReason,
				ReasoningContent: reasoningText,
			}
		}

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				resp := doneResponse()
				resp.Error = ctx.Err()
				ch <- resp
				return
			default:
			}

			line := scanner.Text()
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				ch <- doneResponse()
				return
			}

			var event map[string]any
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}

			// MiniMax may wrap the Anthropic event in {"event":"...","data":{...}}.
			payload := event
			if dataPayload := asMap(event["data"]); dataPayload != nil {
				payload = dataPayload
			}

			msgType := stringValue(payload["type"])
			if msgType == "" {
				msgType = stringValue(event["type"])
			}
			if msgType == "" {
				msgType = stringValue(event["event"])
			}

			index, hasIndex := intValue(payload["index"])
			if !hasIndex {
				index, hasIndex = intValue(event["index"])
			}
			if !hasIndex {
				index = 0
			}

			switch msgType {
			case "content_block_start":
				block := asMap(payload["content_block"])
				if stringValue(block["type"]) != "tool_use" {
					continue
				}

				if _, ok := toolCallBlocks[index]; !ok {
					toolCallOrder = append(toolCallOrder, index)
				}

				streamTool := toolCallBlocks[index]
				if streamTool == nil {
					streamTool = &minimaxStreamToolCall{}
					toolCallBlocks[index] = streamTool
				}
				streamTool.ID = stringValue(block["id"])
				streamTool.Name = stringValue(block["name"])
				if input := asMap(block["input"]); input != nil {
					streamTool.Arguments = input
				}
			case "content_block_delta":
				delta := asMap(payload["delta"])
				if delta == nil {
					continue
				}
				if deltaType, ok := delta["type"].(string); ok {
					switch deltaType {
					case "text_delta":
						if text, ok := delta["text"].(string); ok {
							fullText += text
							ch <- StreamResponse{Chunk: text, Done: false}
						}
					case "thinking_delta":
						if thinking, ok := delta["thinking"].(string); ok {
							reasoningText += thinking
							ch <- StreamResponse{Chunk: thinking, Done: false, IsReasoning: true}
						}
					case "thinking":
						// Direct thinking content block (not delta)
						if thinking, ok := delta["thinking"].(string); ok {
							reasoningText += thinking
							ch <- StreamResponse{Chunk: thinking, Done: false, IsReasoning: true}
						}
					case "input_json_delta":
						streamTool := toolCallBlocks[index]
						if streamTool == nil {
							streamTool = &minimaxStreamToolCall{}
							toolCallBlocks[index] = streamTool
							toolCallOrder = append(toolCallOrder, index)
						}
						streamTool.InputJSON += stringValue(delta["partial_json"])
					}
				}
			case "message_delta":
				if delta := asMap(payload["delta"]); delta != nil {
					finishReason = stringValue(delta["stop_reason"])
				}
				if finishReason == "" {
					finishReason = stringValue(payload["stop_reason"])
				}
			case "message_stop":
				ch <- doneResponse()
				return
			case "thinking":
				// Direct thinking content block (not a delta)
				delta := asMap(payload["delta"])
				if thinking := stringValue(payload["thinking"]); thinking != "" {
					reasoningText += thinking
					ch <- StreamResponse{Chunk: thinking, Done: false, IsReasoning: true}
				} else if thinking := stringValue(delta["thinking"]); thinking != "" {
					reasoningText += thinking
					ch <- StreamResponse{Chunk: thinking, Done: false, IsReasoning: true}
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- StreamResponse{Error: err}
			return
		}

		ch <- doneResponse()
	}()
	return ch
}
