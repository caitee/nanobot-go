package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type FeishuConfig struct {
	AppID     string
	AppSecret string
}

type FeishuChannel struct {
	config FeishuConfig
	bus    bus.MessageBus
}

func NewFeishuChannel(config FeishuConfig, bus bus.MessageBus) *FeishuChannel {
	return &FeishuChannel{config: config, bus: bus}
}

func (c *FeishuChannel) Name() string       { return "feishu" }
func (c *FeishuChannel) DisplayName() string { return "Feishu" }
func (c *FeishuChannel) IsRunning() bool  { return false }
func (c *FeishuChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *FeishuChannel) Stop(ctx context.Context) error { return nil }
func (c *FeishuChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
