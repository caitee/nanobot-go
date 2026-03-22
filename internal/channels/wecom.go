package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type WeComConfig struct {
	CorpID     string
	CorpSecret string
}

type WeComChannel struct {
	config WeComConfig
	bus    bus.MessageBus
}

func NewWeComChannel(config WeComConfig, bus bus.MessageBus) *WeComChannel {
	return &WeComChannel{config: config, bus: bus}
}

func (c *WeComChannel) Name() string       { return "wecom" }
func (c *WeComChannel) DisplayName() string { return "WeCom" }
func (c *WeComChannel) IsRunning() bool    { return false }
func (c *WeComChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *WeComChannel) Stop(ctx context.Context) error { return nil }
func (c *WeComChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
