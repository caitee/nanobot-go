package channels

import (
	"context"
	"fmt"
	"nanobot-go/internal/bus"
)

type DingTalkConfig struct {
	ClientID     string
	ClientSecret string
}

type DingTalkChannel struct {
	config DingTalkConfig
	bus    bus.MessageBus
}

func NewDingTalkChannel(config DingTalkConfig, bus bus.MessageBus) *DingTalkChannel {
	return &DingTalkChannel{config: config, bus: bus}
}

func (c *DingTalkChannel) Name() string       { return "dingtalk" }
func (c *DingTalkChannel) DisplayName() string { return "DingTalk" }
func (c *DingTalkChannel) IsRunning() bool  { return false }
func (c *DingTalkChannel) Start(ctx context.Context) error {
	return nil // Not implemented
}
func (c *DingTalkChannel) Stop(ctx context.Context) error { return nil }
func (c *DingTalkChannel) Send(msg bus.OutboundMessage) error {
	return fmt.Errorf("not implemented")
}
