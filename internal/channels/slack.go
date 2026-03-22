package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type SlackConfig struct {
	Token  string
	TeamID string
}

type SlackChannel struct {
	config SlackConfig
	bus    bus.MessageBus
}

func NewSlackChannel(config SlackConfig, bus bus.MessageBus) *SlackChannel {
	return &SlackChannel{config: config, bus: bus}
}

func (c *SlackChannel) Name() string       { return "slack" }
func (c *SlackChannel) DisplayName() string { return "Slack" }
func (c *SlackChannel) IsRunning() bool   { return false }
func (c *SlackChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *SlackChannel) Stop(ctx context.Context) error { return nil }
func (c *SlackChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
