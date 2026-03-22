package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type WhatsAppConfig struct {
	BridgeURL string
}

type WhatsAppChannel struct {
	config WhatsAppConfig
	bus    bus.MessageBus
}

func NewWhatsAppChannel(config WhatsAppConfig, bus bus.MessageBus) *WhatsAppChannel {
	return &WhatsAppChannel{config: config, bus: bus}
}

func (c *WhatsAppChannel) Name() string       { return "whatsapp" }
func (c *WhatsAppChannel) DisplayName() string { return "WhatsApp" }
func (c *WhatsAppChannel) IsRunning() bool   { return false }
func (c *WhatsAppChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *WhatsAppChannel) Stop(ctx context.Context) error { return nil }
func (c *WhatsAppChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
