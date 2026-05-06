package plugin

import "context"

// Metadata 包含 plugin 的元数据信息
type Metadata struct {
	Name         string   // plugin 名称
	Type         Type     // plugin 类型
	Source       string   // 来源：builtin, external, dynamic 等
	Version      string   // 版本号
	Description  string   // 描述
	Author       string   // 作者
	Dependencies []string // 依赖的其他 plugin 名称
	Removable    bool     // 是否可以被卸载
}

type Plugin interface {
	Name() string
	Type() Type
	Init(ctx context.Context, app AppContext) error
	Close() error
}

// MetadataProvider 是可选接口，plugin 可以实现它来提供元数据
type MetadataProvider interface {
	GetMetadata() Metadata
}

// Lifecycle 是可选接口，plugin 可以实现它来支持启动/停止
type Lifecycle interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
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
	GetLLMRegistry() any
	GetChannelManager() any
	GetCronService() any
}
