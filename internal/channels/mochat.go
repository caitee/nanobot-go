package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type MochatConfig struct {
	APIURL string
	APIKey string
}

type MochatChannel struct {
	config MochatConfig
	bus    bus.MessageBus
}

func NewMochatChannel(config MochatConfig, bus bus.MessageBus) *MochatChannel {
	return &MochatChannel{config: config, bus: bus}
}

func (c *MochatChannel) Name() string       { return "mochat" }
func (c *MochatChannel) DisplayName() string { return "MoChat" }
func (c *MochatChannel) IsRunning() bool     { return false }
func (c *MochatChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *MochatChannel) Stop(ctx context.Context) error { return nil }
func (c *MochatChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
