package channels

import (
	"context"

	"ori/internal/bus"
)

// Channel defines the interface for chat channels
type Channel interface {
	Name() string
	DisplayName() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(msg bus.OutboundMessage) error
	IsRunning() bool
}
