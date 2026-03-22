package bus

import "time"

// InboundMessage represents a message received from a chat channel
type InboundMessage struct {
    Channel    string
    SenderID   string
    ChatID     string
    Content    string
    Timestamp  time.Time
    Media      []string
    Metadata   map[string]any
    SessionKey string
}

// OutboundMessage represents a message to send to a chat channel
type OutboundMessage struct {
    Channel  string
    ChatID   string
    Content  string
    ReplyTo  string
    Media    []string
    Metadata map[string]any
}
