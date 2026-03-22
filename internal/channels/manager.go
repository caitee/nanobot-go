package channels

import (
	"context"
	"log/slog"
	"sync"

	"nanobot-go/internal/bus"
)

type Manager struct {
	channels map[string]Channel
	bus      bus.MessageBus
	mu       sync.RWMutex
}

func NewManager(bus bus.MessageBus) *Manager {
	return &Manager{
		channels: make(map[string]Channel),
		bus:      bus,
	}
}

func (m *Manager) Register(ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[ch.Name()] = ch
	slog.Info("channel registered", "name", ch.Name())
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		if err := ch.Start(ctx); err != nil {
			slog.Error("failed to start channel", "name", ch.Name(), "error", err)
		}
	}
	return nil
}

func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		if err := ch.Stop(context.Background()); err != nil {
			slog.Error("failed to stop channel", "name", ch.Name(), "error", err)
		}
	}
}

func (m *Manager) Get(name string) Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channels[name]
}

func (m *Manager) List() []Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []Channel
	for _, ch := range m.channels {
		list = append(list, ch)
	}
	return list
}
