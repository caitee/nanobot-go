package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type EmailConfig struct {
	IMAPHost string
	SMTPHost string
	Username string
	Password string
}

type EmailChannel struct {
	config EmailConfig
	bus    bus.MessageBus
}

func NewEmailChannel(config EmailConfig, bus bus.MessageBus) *EmailChannel {
	return &EmailChannel{config: config, bus: bus}
}

func (c *EmailChannel) Name() string       { return "email" }
func (c *EmailChannel) DisplayName() string { return "Email" }
func (c *EmailChannel) IsRunning() bool    { return false }
func (c *EmailChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *EmailChannel) Stop(ctx context.Context) error { return nil }
func (c *EmailChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
