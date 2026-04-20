package tools

import (
	"context"
	"fmt"
)

type MessageTool struct{}

func NewMessageTool() *MessageTool { return &MessageTool{} }

func (t *MessageTool) Name() string   { return "message" }
func (t *MessageTool) Description() string { return "Send a message to a chat channel. Use this when the user asks to send a message to someone or somewhere." }
func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"channel": map[string]any{"type": "string", "description": "Channel name (e.g., telegram, discord, wechat)"},
			"chat_id": map[string]any{"type": "string", "description": "Target chat or conversation ID"},
			"content": map[string]any{"type": "string", "description": "Message text to send"},
		},
		"required": []any{"channel", "chat_id", "content"},
		"examples": []any{
			map[string]any{"channel": "telegram", "chat_id": "123456", "content": "Hello!"},
			map[string]any{"channel": "discord", "chat_id": "987654321", "content": "Reminder: standup at 10am"},
		},
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
