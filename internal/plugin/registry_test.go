package plugin

import (
	"context"
	"testing"
)

// mockPlugin 是一个用于测试的 mock plugin
type mockPlugin struct {
	name         string
	pluginType   Type
	metadata     Metadata
	initCalled   bool
	closeCalled  bool
	startCalled  bool
	stopCalled   bool
	initErr      error
	closeErr     error
	startErr     error
	stopErr      error
}

func (m *mockPlugin) Name() string {
	return m.name
}

func (m *mockPlugin) Type() Type {
	return m.pluginType
}

func (m *mockPlugin) Init(ctx context.Context, app AppContext) error {
	m.initCalled = true
	return m.initErr
}

func (m *mockPlugin) Close() error {
	m.closeCalled = true
	return m.closeErr
}

func (m *mockPlugin) GetMetadata() Metadata {
	return m.metadata
}

func (m *mockPlugin) Start(ctx context.Context) error {
	m.startCalled = true
	return m.startErr
}

func (m *mockPlugin) Stop(ctx context.Context) error {
	m.stopCalled = true
	return m.stopErr
}

// TestPluginMetadata 测试 plugin 元数据功能
func TestPluginMetadata(t *testing.T) {
	meta := Metadata{
		Name:        "test-plugin",
		Type:        TypeProvider,
		Source:      "builtin",
		Version:     "1.0.0",
		Description: "A test plugin",
		Author:      "test-author",
		Dependencies: []string{"dep1", "dep2"},
		Removable:   true,
	}

	plugin := &mockPlugin{
		name:       "test-plugin",
		pluginType: TypeProvider,
		metadata:   meta,
	}

	got := plugin.GetMetadata()
	if got.Name != meta.Name {
		t.Errorf("Name = %v, want %v", got.Name, meta.Name)
	}
	if got.Source != meta.Source {
		t.Errorf("Source = %v, want %v", got.Source, meta.Source)
	}
	if got.Version != meta.Version {
		t.Errorf("Version = %v, want %v", got.Version, meta.Version)
	}
	if !got.Removable {
		t.Errorf("Removable = %v, want true", got.Removable)
	}
	if len(got.Dependencies) != 2 {
		t.Errorf("Dependencies length = %v, want 2", len(got.Dependencies))
	}
}

// TestRegistryGetMetadata 测试从 registry 获取 plugin 元数据
func TestRegistryGetMetadata(t *testing.T) {
	registry := NewRegistry()

	meta := Metadata{
		Name:         "test-plugin",
		Type:         TypeProvider,
		Source:       "builtin",
		Version:      "1.0.0",
		Dependencies: []string{"dep1"},
		Removable:    true,
	}

	plugin := &mockPlugin{
		name:       "test-plugin",
		pluginType: TypeProvider,
		metadata:   meta,
	}

	err := registry.Register(plugin)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	gotMeta, ok := registry.GetMetadata("test-plugin")
	if !ok {
		t.Fatal("GetMetadata returned false")
	}

	if gotMeta.Name != meta.Name {
		t.Errorf("Name = %v, want %v", gotMeta.Name, meta.Name)
	}
	if gotMeta.Source != meta.Source {
		t.Errorf("Source = %v, want %v", gotMeta.Source, meta.Source)
	}
}

// TestRegistryGetDependencies 测试查询 plugin 依赖关系
func TestRegistryGetDependencies(t *testing.T) {
	registry := NewRegistry()

	plugin1 := &mockPlugin{
		name:       "plugin1",
		pluginType: TypeProvider,
		metadata: Metadata{
			Name:         "plugin1",
			Dependencies: []string{"dep1", "dep2"},
		},
	}

	err := registry.Register(plugin1)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	deps := registry.GetDependencies("plugin1")
	if len(deps) != 2 {
		t.Errorf("GetDependencies length = %v, want 2", len(deps))
	}
}

// TestRegistryLifecycle 测试 plugin 生命周期管理
func TestRegistryLifecycle(t *testing.T) {
	registry := NewRegistry()
	ctx := context.Background()

	plugin := &mockPlugin{
		name:       "test-plugin",
		pluginType: TypeProvider,
		metadata: Metadata{
			Name: "test-plugin",
		},
	}

	err := registry.Register(plugin)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Test Start
	err = registry.Start(ctx, "test-plugin")
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}
	if !plugin.startCalled {
		t.Error("Start was not called on plugin")
	}

	// Test Stop
	err = registry.Stop(ctx, "test-plugin")
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	if !plugin.stopCalled {
		t.Error("Stop was not called on plugin")
	}
}

// TestRegistryUnload 测试 plugin 卸载功能
func TestRegistryUnload(t *testing.T) {
	registry := NewRegistry()

	plugin := &mockPlugin{
		name:       "test-plugin",
		pluginType: TypeProvider,
		metadata: Metadata{
			Name:      "test-plugin",
			Removable: true,
		},
	}

	err := registry.Register(plugin)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// 验证 plugin 已注册
	_, ok := registry.Get("test-plugin")
	if !ok {
		t.Fatal("Plugin not found after registration")
	}

	// 卸载 plugin
	err = registry.Unload("test-plugin")
	if err != nil {
		t.Errorf("Unload failed: %v", err)
	}

	// 验证 plugin 已被移除
	_, ok = registry.Get("test-plugin")
	if ok {
		t.Error("Plugin still exists after unload")
	}
}

// TestRegistryUnloadNonRemovable 测试不可卸载的 plugin
func TestRegistryUnloadNonRemovable(t *testing.T) {
	registry := NewRegistry()

	plugin := &mockPlugin{
		name:       "builtin-plugin",
		pluginType: TypeProvider,
		metadata: Metadata{
			Name:      "builtin-plugin",
			Removable: false,
		},
	}

	err := registry.Register(plugin)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// 尝试卸载不可移除的 plugin
	err = registry.Unload("builtin-plugin")
	if err == nil {
		t.Error("Expected error when unloading non-removable plugin")
	}
}

// TestRegistryListMetadata 测试列出所有 plugin 的元数据
func TestRegistryListMetadata(t *testing.T) {
	registry := NewRegistry()

	plugin1 := &mockPlugin{
		name:       "plugin1",
		pluginType: TypeProvider,
		metadata: Metadata{
			Name:   "plugin1",
			Source: "builtin",
		},
	}

	plugin2 := &mockPlugin{
		name:       "plugin2",
		pluginType: TypeChannel,
		metadata: Metadata{
			Name:   "plugin2",
			Source: "external",
		},
	}

	registry.Register(plugin1)
	registry.Register(plugin2)

	metadataList := registry.ListMetadata()
	if len(metadataList) != 2 {
		t.Errorf("ListMetadata length = %v, want 2", len(metadataList))
	}
}
