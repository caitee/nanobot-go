package plugin

import "context"

type Plugin interface {
	Name() string
	Type() Type
	Init(ctx context.Context, app AppContext) error
	Close() error
}

type Type string

const (
	TypeProvider Type = "provider"
	TypeChannel  Type = "channel"
	TypeTool     Type = "tool"
)

// AppContext uses any returns to avoid circular imports with concrete packages.
type AppContext interface {
	GetConfig() any
	GetBus() any
	GetSessionStore() any
	GetToolRegistry() any
	GetProviderRegistry() any
	GetChannelManager() any
	GetCronService() any
}
