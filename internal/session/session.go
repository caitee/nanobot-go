package session

import (
	"time"
)

// ToolCall represents a tool call from an assistant message
type ToolCall struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Message represents a session message
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"` // string or []any
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// Session represents a conversation session
type Session struct {
	Key              string    `json:"key"`
	Messages         []Message `json:"messages"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Metadata         map[string]any `json:"metadata"`
	LastConsolidated int       `json:"last_consolidated"`
}

// SessionInfo represents session metadata for listing
type SessionInfo struct {
	Key          string    `json:"key"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	MessageCount int       `json:"message_count"`
}

// _findLegalStart finds the first index where every tool result has a matching
// assistant tool_call. This ensures we don't start a history window mid-tool-call.
func _findLegalStart(messages []Message) int {
	declared := make(map[string]bool)
	start := 0

	for i, msg := range messages {
		if msg.Role == "assistant" {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					declared[tc.ID] = true
				}
			}
		} else if msg.Role == "tool" {
			tid := msg.ToolCallID
			if tid != "" && !declared[tid] {
				// Orphan tool result: move start past this
				start = i + 1
				// Rebuild declared set from messages[start:i+1]
				declared = make(map[string]bool)
				for j := start; j <= i; j++ {
					if messages[j].Role == "assistant" {
						for _, tc := range messages[j].ToolCalls {
							if tc.ID != "" {
								declared[tc.ID] = true
							}
						}
					}
				}
			}
		}
	}
	return start
}

// GetHistory returns unconsolidated messages for LLM input, aligned to a legal
// tool-call boundary. It supports maxMessages limitation and handles orphan tool results.
func (s *Session) GetHistory(maxMessages int) []Message {
	if maxMessages <= 0 {
		maxMessages = 500
	}

	// Get unconsolidated messages
	unconsolidated := s.Messages[s.LastConsolidated:]

	// Take the last maxMessages
	if len(unconsolidated) > maxMessages {
		unconsolidated = unconsolidated[len(unconsolidated)-maxMessages:]
	}

	// Drop leading non-user messages to avoid starting mid-turn
	for i, msg := range unconsolidated {
		if msg.Role == "user" {
			unconsolidated = unconsolidated[i:]
			break
		}
	}

	// Some providers reject orphan tool results if the matching assistant
	// tool_calls message fell outside the fixed-size history window.
	start := _findLegalStart(unconsolidated)
	if start > 0 {
		unconsolidated = unconsolidated[start:]
	}

	// Build output with relevant fields
	out := make([]Message, 0, len(unconsolidated))
	for _, msg := range unconsolidated {
		entry := Message{
			Role:    msg.Role,
			Content: msg.Content,
		}
		if msg.ToolCalls != nil {
			entry.ToolCalls = msg.ToolCalls
		}
		if msg.ToolCallID != "" {
			entry.ToolCallID = msg.ToolCallID
		}
		if msg.Name != "" {
			entry.Name = msg.Name
		}
		out = append(out, entry)
	}

	return out
}

// AddMessage adds a message to the session
func (s *Session) AddMessage(role, content string) {
	s.Messages = append(s.Messages, Message{
		Role:    role,
		Content: content,
	})
	s.UpdatedAt = time.Now()
}

// AddToolCallMessage adds an assistant message with tool calls
func (s *Session) AddToolCallMessage(role, content string, toolCalls []ToolCall) {
	s.Messages = append(s.Messages, Message{
		Role:      role,
		Content:   content,
		ToolCalls: toolCalls,
	})
	s.UpdatedAt = time.Now()
}

// AddToolResultMessage adds a tool result message
func (s *Session) AddToolResultMessage(toolCallID, content string) {
	s.Messages = append(s.Messages, Message{
		Role:       "tool",
		Content:    content,
		ToolCallID: toolCallID,
	})
	s.UpdatedAt = time.Now()
}

// Consolidate marks messages as consolidated up to the current length
func (s *Session) Consolidate() {
	s.LastConsolidated = len(s.Messages)
	s.UpdatedAt = time.Now()
}

// Clear resets the session to initial state
func (s *Session) Clear() {
	s.Messages = nil
	s.LastConsolidated = 0
	s.UpdatedAt = time.Now()
}

// HasPendingToolResults returns true if there are tool results without matching tool calls
func (s *Session) HasPendingToolResults() bool {
	if len(s.Messages) <= s.LastConsolidated {
		return false
	}
	// Get unconsolidated messages
	unconsolidated := s.Messages[s.LastConsolidated:]
	// Check for orphan tool results
	start := _findLegalStart(unconsolidated)
	return start > 0
}
