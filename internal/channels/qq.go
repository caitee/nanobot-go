package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type QQConfig struct {
	AppID     string
	AppSecret string
}

type QQChannel struct {
	config QQConfig
	bus    bus.MessageBus
}

func NewQQChannel(config QQConfig, bus bus.MessageBus) *QQChannel {
	return &QQChannel{config: config, bus: bus}
}

func (c *QQChannel) Name() string       { return "qq" }
func (c *QQChannel) DisplayName() string { return "QQ" }
func (c *QQChannel) IsRunning() bool   { return false }
func (c *QQChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *QQChannel) Stop(ctx context.Context) error { return nil }
func (c *QQChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
