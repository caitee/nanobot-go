package heartbeat

import (
	"context"

	"nanobot-go/internal/providers"
)

// LLMProviderAdapter adapts providers.LLMProvider to HeartbeatProvider
type LLMProviderAdapter struct {
	provider providers.LLMProvider
	model    string
}

// NewLLMProviderAdapter creates a new adapter
func NewLLMProviderAdapter(provider providers.LLMProvider, model string) *LLMProviderAdapter {
	return &LLMProviderAdapter{
		provider: provider,
		model:    model,
	}
}

// Chat implements HeartbeatProvider.Chat
func (a *LLMProviderAdapter) Chat(ctx context.Context, messages []ChatMessage, tools []map[string]any) (*HeartbeatResponse, error) {
	// Convert messages
	providerMsgs := make([]providers.Message, len(messages))
	for i, msg := range messages {
		providerMsgs[i] = providers.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Convert tools
	providerTools := make([]providers.ToolDef, len(tools))
	for i, tool := range tools {
		providerTools[i] = providers.ToolDef{
			Name:        getString(tool, "name"),
			Description: getString(tool, "description"),
			Parameters:  getMap(tool, "parameters"),
		}
	}

	resp, err := a.provider.Chat(ctx, providerMsgs, providerTools, providers.ChatOptions{
		Model:       a.model,
		MaxTokens:   4096,
		Temperature: 0.7,
	})
	if err != nil {
		return nil, err
	}

	// Convert response
	heartbeatResp := &HeartbeatResponse{
		Content: resp.Content,
	}

	for _, tc := range resp.ToolCalls {
		heartbeatResp.ToolCalls = append(heartbeatResp.ToolCalls, HeartbeatToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}

	return heartbeatResp, nil
}

// ChatWithRetry implements HeartbeatProvider.ChatWithRetry
func (a *LLMProviderAdapter) ChatWithRetry(ctx context.Context, messages []ChatMessage, tools []map[string]any) (*HeartbeatResponse, error) {
	// Convert messages
	providerMsgs := make([]providers.Message, len(messages))
	for i, msg := range messages {
		providerMsgs[i] = providers.Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
	}

	// Convert tools
	providerTools := make([]providers.ToolDef, len(tools))
	for i, tool := range tools {
		providerTools[i] = providers.ToolDef{
			Name:        getString(tool, "name"),
			Description: getString(tool, "description"),
			Parameters:  getMap(tool, "parameters"),
		}
	}

	resp, err := a.provider.ChatWithRetry(ctx, providerMsgs, providerTools, providers.ChatOptions{
		Model:       a.model,
		MaxTokens:   4096,
		Temperature: 0.7,
	})
	if err != nil {
		return nil, err
	}

	// Convert response
	heartbeatResp := &HeartbeatResponse{
		Content: resp.Content,
	}

	for _, tc := range resp.ToolCalls {
		heartbeatResp.ToolCalls = append(heartbeatResp.ToolCalls, HeartbeatToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}

	return heartbeatResp, nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}
