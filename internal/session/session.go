package session

import "time"

// Message represents a session message
type Message struct {
    Role    string `json:"role"`
    Content any    `json:"content"` // string or []any
}

// Session represents a conversation session
type Session struct {
    Key               string    `json:"key"`
    Messages          []Message `json:"messages"`
    CreatedAt         time.Time `json:"created_at"`
    UpdatedAt         time.Time `json:"updated_at"`
    Metadata          map[string]any `json:"metadata"`
    LastConsolidated  int       `json:"last_consolidated"`
}

// SessionInfo represents session metadata for listing
type SessionInfo struct {
    Key        string    `json:"key"`
    CreatedAt  time.Time `json:"created_at"`
    UpdatedAt  time.Time `json:"updated_at"`
    MessageCount int     `json:"message_count"`
}
