package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"nanobot-go/internal/bus"

	"github.com/gorilla/websocket"
)

type TelegramConfig struct {
	Token     string
	AllowFrom []string
	BotName   string
}

type TelegramChannel struct {
	config    TelegramConfig
	bus       bus.MessageBus
	conn      *websocket.Conn
	running   bool
	stopCh    chan struct{}
	updatesMu sync.Mutex
}

func NewTelegramChannel(config TelegramConfig, bus bus.MessageBus) *TelegramChannel {
	return &TelegramChannel{
		config: config,
		bus:    bus,
		stopCh: make(chan struct{}),
	}
}

func (c *TelegramChannel) Name() string       { return "telegram" }
func (c *TelegramChannel) DisplayName() string { return "Telegram" }
func (c *TelegramChannel) IsRunning() bool     { return c.running }

func (c *TelegramChannel) Start(ctx context.Context) error {
	if c.config.Token == "" {
		return fmt.Errorf("telegram token not configured")
	}

	c.running = true
	go c.pollUpdates(ctx)
	slog.Info("telegram channel started")
	return nil
}

func (c *TelegramChannel) Stop(ctx context.Context) error {
	c.running = false
	close(c.stopCh)
	if c.conn != nil {
		c.conn.Close()
	}
	slog.Info("telegram channel stopped")
	return nil
}

func (c *TelegramChannel) Send(msg bus.OutboundMessage) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	// Send via Telegram API
	// Simplified - actual implementation would call telegram API
	return nil
}

func (c *TelegramChannel) pollUpdates(ctx context.Context) {
	offset := 0
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case <-ticker.C:
			updates, err := c.getUpdates(offset)
			if err != nil {
				slog.Error("get updates error", "error", err)
				continue
			}
			for _, update := range updates {
				c.handleUpdate(update)
				offset = update.ID + 1
			}
		}
	}
}

type telegramUpdate struct {
	ID int `json:"update_id"`
	Message struct {
		ID   int `json:"message_id"`
		From struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
		} `json:"from"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func (c *TelegramChannel) getUpdates(offset int) ([]telegramUpdate, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=30", c.config.Token, offset)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool             `json:"ok"`
		Result []telegramUpdate `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Result, nil
}

func (c *TelegramChannel) handleUpdate(update telegramUpdate) {
	msg := update.Message

	// ACL check
	if len(c.config.AllowFrom) > 0 {
		allowed := false
		for _, allow := range c.config.AllowFrom {
			if allow == fmt.Sprintf("%d", msg.From.ID) || allow == msg.From.Username {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	inbound := bus.InboundMessage{
		Channel:    "telegram",
		SenderID:   fmt.Sprintf("%d", msg.From.ID),
		ChatID:     fmt.Sprintf("%d", msg.Chat.ID),
		Content:    msg.Text,
		Timestamp:  time.Now(),
		SessionKey: fmt.Sprintf("telegram:%d", msg.Chat.ID),
	}
	c.bus.PublishInbound(inbound)
}
