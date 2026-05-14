package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ori/internal/llm"
	"ori/internal/tool"
)

func TestMCPProxyParametersSupportNewAndLegacyActions(t *testing.T) {
	manager := NewMCPManager(MCPManagerOptions{
		Config: &MCPConfig{
			Servers: map[string]MCPServerConfig{
				"alpha": {Name: "alpha", Command: "server"},
			},
		},
	})
	proxy := NewMCPProxyTool(manager)

	schema := proxy.Parameters()
	props := schema["properties"].(map[string]any)
	action := props["action"].(map[string]any)
	enums := action["enum"].([]any)

	for _, want := range []string{"status", "list", "search", "describe", "connect", "call", "tools", "resources", "prompts"} {
		if !containsEnum(enums, want) {
			t.Fatalf("action enum missing %q: %#v", want, enums)
		}
	}

	server := props["server"].(map[string]any)
	if !containsEnum(server["enum"].([]any), "alpha") {
		t.Fatalf("server enum missing configured server: %#v", server)
	}
}

func TestMCPProxyLegacyToolActionCallsManager(t *testing.T) {
	manager := NewMCPManager(MCPManagerOptions{
		ClientFactory: &fakeMCPClientFactory{
			tools: []MCPToolMeta{
				{Name: "echo", Description: "echo input", InputSchema: map[string]any{"type": "object"}},
			},
			callResult: MCPCallResult{
				Content: []llm.Content{llm.TextContent{Text: "hello"}},
			},
		},
		Config: &MCPConfig{
			Servers: map[string]MCPServerConfig{
				"alpha": {Name: "alpha", Command: "server"},
			},
		},
	})
	proxy := NewMCPProxyTool(manager)

	res, err := proxy.Execute(context.Background(), "call-1", map[string]any{
		"action":    "tools",
		"server":    "alpha",
		"tool":      "echo",
		"arguments": map[string]any{"text": "hello"},
	}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	text := textContent(t, res)
	if text != "hello" {
		t.Fatalf("result text = %q", text)
	}
}

func TestMCPProxyCallAcceptsJSONArgumentsFromURI(t *testing.T) {
	factory := &fakeMCPClientFactory{
		tools: []MCPToolMeta{
			{Name: "web_search", Description: "search web", InputSchema: map[string]any{"type": "object"}},
		},
		callResult: MCPCallResult{
			Content: []llm.Content{llm.TextContent{Text: "ok"}},
		},
	}
	manager := NewMCPManager(MCPManagerOptions{
		ClientFactory: factory,
		Config: &MCPConfig{
			Servers: map[string]MCPServerConfig{
				"MiniMax": {Name: "MiniMax", Command: "server"},
			},
		},
	})
	reg := tool.NewRegistry()
	reg.Register(NewMCPProxyTool(manager))

	_, err := reg.Execute(context.Background(), "mcp", "call-1", map[string]any{
		"action": "call",
		"server": "MiniMax",
		"tool":   "web_search",
		"uri":    `{"query":"百合竹怎么养护"}`,
	}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if factory.lastCallArgs["query"] != "百合竹怎么养护" {
		t.Fatalf("query argument = %#v; want forwarded query", factory.lastCallArgs)
	}
}

func TestMCPDirectToolsUseCachedMetadataAndStableNames(t *testing.T) {
	cfg := &MCPConfig{
		Settings: MCPSettings{DirectTools: DirectToolSelector{All: true}},
		Servers: map[string]MCPServerConfig{
			"chrome-devtools": {Name: "chrome-devtools", DirectTools: DirectToolSelector{Names: []string{"take_screenshot"}}},
			"skip":            {Name: "skip", ExcludeTools: []string{"hidden"}},
		},
	}
	manager := NewMCPManager(MCPManagerOptions{
		Config: cfg,
		Cache: &MCPMetadataCache{Servers: map[string]MCPServerMetadata{
			"chrome-devtools": {
				ConfigHash: HashMCPServerConfig(cfg.Servers["chrome-devtools"]),
				Tools: []MCPToolMeta{
					{Name: "take_screenshot", Description: "capture", InputSchema: map[string]any{"type": "object"}},
					{Name: "navigate-page", Description: "navigate", InputSchema: map[string]any{"type": "object"}},
				},
			},
			"skip": {
				ConfigHash: HashMCPServerConfig(cfg.Servers["skip"]),
				Tools: []MCPToolMeta{
					{Name: "hidden", Description: "hidden", InputSchema: map[string]any{"type": "object"}},
				},
			},
		}},
	})

	direct := manager.DirectTools()
	if len(direct) != 1 {
		t.Fatalf("direct tools = %d; want 1", len(direct))
	}
	if direct[0].Name() != "mcp_chrome_devtools_take_screenshot" {
		t.Fatalf("direct tool name = %q", direct[0].Name())
	}
	if !strings.Contains(direct[0].Description(), "chrome-devtools") {
		t.Fatalf("description should name server: %q", direct[0].Description())
	}
}

func TestMCPDirectToolPropagatesMCPIsError(t *testing.T) {
	manager := NewMCPManager(MCPManagerOptions{
		ClientFactory: &fakeMCPClientFactory{
			callResult: MCPCallResult{
				Content: []llm.Content{llm.TextContent{Text: "server failed"}},
				IsError: true,
			},
		},
		Config: &MCPConfig{
			Servers: map[string]MCPServerConfig{
				"alpha": {Name: "alpha", Command: "server"},
			},
		},
	})
	direct := newMCPDirectTool(manager, "mcp_alpha_echo", "alpha", MCPToolMeta{
		Name:        "echo",
		Description: "echo input",
		InputSchema: map[string]any{"type": "object"},
	})

	_, err := direct.Execute(context.Background(), "call-1", map[string]any{}, nil)
	if err == nil || !strings.Contains(err.Error(), "server failed") {
		t.Fatalf("expected MCP tool error, got %v", err)
	}
}

func TestMCPManagerStartConnectsEagerServersAndCachesMetadata(t *testing.T) {
	factory := &fakeMCPClientFactory{
		tools: []MCPToolMeta{{Name: "echo", InputSchema: map[string]any{"type": "object"}}},
	}
	manager := NewMCPManager(MCPManagerOptions{
		ClientFactory: factory,
		Config: &MCPConfig{Servers: map[string]MCPServerConfig{
			"alpha": {Name: "alpha", Command: "server", Lifecycle: MCPLifecycleEager},
		}},
	})

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if factory.connectCalls != 1 {
		t.Fatalf("connect calls = %d; want 1", factory.connectCalls)
	}
	status := manager.Status()
	if len(status) != 1 || !status[0].Connected || status[0].Tools != 1 {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestMCPManagerBacksOffFailedConnections(t *testing.T) {
	factory := &fakeMCPClientFactory{connectErr: errors.New("dial failed")}
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	manager := NewMCPManager(MCPManagerOptions{
		ClientFactory: factory,
		Now:           func() time.Time { return now },
		Config: &MCPConfig{Servers: map[string]MCPServerConfig{
			"alpha": {Name: "alpha", Command: "server"},
		}},
	})

	_, err := manager.CallTool(context.Background(), "alpha", "echo", nil)
	if err == nil || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("first call error = %v", err)
	}
	_, err = manager.CallTool(context.Background(), "alpha", "echo", nil)
	if err == nil || !strings.Contains(err.Error(), "backoff") {
		t.Fatalf("second call should use backoff, got %v", err)
	}
	if factory.connectCalls != 1 {
		t.Fatalf("connect calls = %d; want 1", factory.connectCalls)
	}
}

func containsEnum(enums []any, want string) bool {
	for _, item := range enums {
		if item == want {
			return true
		}
	}
	return false
}

func textContent(t *testing.T, res *tool.Result) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	text, ok := res.Content[0].(llm.TextContent)
	if !ok {
		t.Fatalf("content type = %T", res.Content[0])
	}
	return text.Text
}

type fakeMCPClientFactory struct {
	tools        []MCPToolMeta
	resources    []MCPResourceMeta
	prompts      []MCPPromptMeta
	callResult   MCPCallResult
	connectErr   error
	connectCalls int
	lastCallArgs map[string]any
}

func (f *fakeMCPClientFactory) Connect(ctx context.Context, cfg MCPServerConfig) (MCPClientSession, error) {
	f.connectCalls++
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return &fakeMCPClientSession{factory: f}, nil
}

type fakeMCPClientSession struct {
	factory *fakeMCPClientFactory
	closed  bool
}

func (s *fakeMCPClientSession) ListTools(ctx context.Context) ([]MCPToolMeta, error) {
	return s.factory.tools, nil
}
func (s *fakeMCPClientSession) CallTool(ctx context.Context, name string, args map[string]any) (MCPCallResult, error) {
	s.factory.lastCallArgs = args
	return s.factory.callResult, nil
}
func (s *fakeMCPClientSession) ListResources(ctx context.Context) ([]MCPResourceMeta, error) {
	return s.factory.resources, nil
}
func (s *fakeMCPClientSession) ReadResource(ctx context.Context, uri string) (MCPCallResult, error) {
	return s.factory.callResult, nil
}
func (s *fakeMCPClientSession) ListPrompts(ctx context.Context) ([]MCPPromptMeta, error) {
	return s.factory.prompts, nil
}
func (s *fakeMCPClientSession) GetPrompt(ctx context.Context, name string, args map[string]any) (MCPCallResult, error) {
	return s.factory.callResult, nil
}
func (s *fakeMCPClientSession) Close() error {
	s.closed = true
	return nil
}
