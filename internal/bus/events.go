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
	Channel   string
	ChatID    string
	Content   string
	ReplyTo   string
	Media     []string
	Reasoning string
	Metadata  map[string]any
}

const (
	// OutboundMetadataAgentEventFinal marks an outbound response whose final
	// content was already published through the runtime event stream. Rich UIs
	// that subscribe to runtime.Event use this to decide whether they still
	// need to render the outbound message or have already drawn the answer.
	OutboundMetadataAgentEventFinal = "agent_event_final"
)
