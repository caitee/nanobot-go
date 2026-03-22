package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type MatrixConfig struct {
	Homeserver   string
	UserID       string
	AccessToken  string
}

type MatrixChannel struct {
	config MatrixConfig
	bus    bus.MessageBus
}

func NewMatrixChannel(config MatrixConfig, bus bus.MessageBus) *MatrixChannel {
	return &MatrixChannel{config: config, bus: bus}
}

func (c *MatrixChannel) Name() string       { return "matrix" }
func (c *MatrixChannel) DisplayName() string { return "Matrix" }
func (c *MatrixChannel) IsRunning() bool    { return false }
func (c *MatrixChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *MatrixChannel) Stop(ctx context.Context) error { return nil }
func (c *MatrixChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
