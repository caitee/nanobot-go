package channels

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"nanobot-go/internal/bus"

	"github.com/gorilla/websocket"
)

type DiscordConfig struct {
	Token     string
	GuildID   string
	AllowFrom []string
}

type DiscordChannel struct {
	config  DiscordConfig
	bus     bus.MessageBus
	gateway *websocket.Conn
	running bool
	stopCh  chan struct{}
	session *discordSession
	mu      sync.Mutex
}

type discordSession struct {
	ID       string `json:"session_id"`
	Token    string `json:"token"`
	Endpoint string `json:"endpoint"`
}

func NewDiscordChannel(config DiscordConfig, bus bus.MessageBus) *DiscordChannel {
	return &DiscordChannel{
		config: config,
		bus:    bus,
		stopCh: make(chan struct{}),
	}
}

func (c *DiscordChannel) Name() string       { return "discord" }
func (c *DiscordChannel) DisplayName() string { return "Discord" }
func (c *DiscordChannel) IsRunning() bool  { return c.running }

func (c *DiscordChannel) Start(ctx context.Context) error {
	if c.config.Token == "" {
		return fmt.Errorf("discord token not configured")
	}

	c.running = true
	go c.connectGateway(ctx)
	slog.Info("discord channel started")
	return nil
}

func (c *DiscordChannel) Stop(ctx context.Context) error {
	c.running = false
	close(c.stopCh)
	if c.gateway != nil {
		c.gateway.Close()
	}
	slog.Info("discord channel stopped")
	return nil
}

func (c *DiscordChannel) Send(msg bus.OutboundMessage) error {
	// Implementation would send via Discord API
	return nil
}

func (c *DiscordChannel) connectGateway(ctx context.Context) {
	// Simplified gateway connection
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
			time.Sleep(time.Second)
		}
	}
}
