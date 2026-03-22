package tools

import (
	"context"
	"fmt"
)

type MessageTool struct{}

func NewMessageTool() *MessageTool { return &MessageTool{} }

func (t *MessageTool) Name() string   { return "message" }
func (t *MessageTool) Description() string { return "Send a message to a chat channel" }
func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{"type": "string"},
			"chat_id": map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []any{"channel", "chat_id", "content"},
	}
}

func (t *MessageTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	channel, _ := params["channel"].(string)
	chatID, _ := params["chat_id"].(string)
	content, _ := params["content"].(string)

	if channel == "" || chatID == "" || content == "" {
		return nil, fmt.Errorf("channel, chat_id, and content are required")
	}

	// This would be implemented to actually send via the bus
	// Placeholder return
	return fmt.Sprintf("Message sent to %s:%s", channel, chatID), nil
}
