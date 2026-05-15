package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"ori/internal/llm"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPCallResult is an Ori-shaped MCP operation result.
type MCPCallResult struct {
	Content []llm.Content
	Details any
	IsError bool
}

// MCPClientSession is the small protocol surface MCPManager needs.
type MCPClientSession interface {
	ListTools(ctx context.Context) ([]MCPToolMeta, error)
	CallTool(ctx context.Context, name string, args map[string]any) (MCPCallResult, error)
	ListResources(ctx context.Context) ([]MCPResourceMeta, error)
	ReadResource(ctx context.Context, uri string) (MCPCallResult, error)
	ListPrompts(ctx context.Context) ([]MCPPromptMeta, error)
	GetPrompt(ctx context.Context, name string, args map[string]any) (MCPCallResult, error)
	ServerInstructions() string
	ServerDisplayName() string
	Close() error
}

// MCPClientFactory opens one client session for a server.
type MCPClientFactory interface {
	Connect(ctx context.Context, cfg MCPServerConfig) (MCPClientSession, error)
}

type sdkMCPClientFactory struct {
	httpClient *http.Client
}

func (f *sdkMCPClientFactory) Connect(ctx context.Context, cfg MCPServerConfig) (MCPClientSession, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "ori", Version: "0.1.0-go"}, nil)
	if cfg.Transport == "" && cfg.URL != "" && cfg.Command == "" {
		transport, err := f.transport(cfg, "streamable_http")
		if err == nil {
			session, connectErr := client.Connect(ctx, transport, nil)
			if connectErr == nil {
				return &sdkMCPClientSession{session: session}, nil
			}
			err = connectErr
		}
		sseCfg := cfg
		if !strings.HasSuffix(strings.TrimRight(sseCfg.URL, "/"), "/sse") {
			sseCfg.URL = strings.TrimRight(sseCfg.URL, "/") + "/sse"
		}
		transport, sseErr := f.transport(sseCfg, "sse")
		if sseErr != nil {
			return nil, fmt.Errorf("streamable_http failed: %v; sse setup failed: %w", err, sseErr)
		}
		session, sseErr := client.Connect(ctx, transport, nil)
		if sseErr != nil {
			return nil, fmt.Errorf("streamable_http failed: %v; sse failed: %w", err, sseErr)
		}
		return &sdkMCPClientSession{session: session}, nil
	}

	transport, err := f.transport(cfg, "")
	if err != nil {
		return nil, err
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	return &sdkMCPClientSession{session: session}, nil
}

func (f *sdkMCPClientFactory) transport(cfg MCPServerConfig, override string) (sdkmcp.Transport, error) {
	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	httpClient := f.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	if len(cfg.Headers) > 0 {
		httpClient = &http.Client{
			Timeout:   httpClient.Timeout,
			Transport: headerRoundTripper{headers: cfg.Headers, base: httpClient.Transport},
		}
	}

	transport := cfg.Transport
	if override != "" {
		transport = override
	}
	if transport == "" {
		if cfg.Command != "" {
			transport = "stdio"
		} else {
			transport = "streamable_http"
		}
	}
	switch transport {
	case "stdio":
		if cfg.Command == "" {
			return nil, fmt.Errorf("stdio MCP server requires command")
		}
		cmd := exec.Command(cfg.Command, cfg.Args...)
		if len(cfg.Env) > 0 {
			cmd.Env = os.Environ()
			for k, v := range cfg.Env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
		}
		return &sdkmcp.CommandTransport{Command: cmd}, nil
	case "streamable_http", "streamableHttp", "http":
		if cfg.URL == "" {
			return nil, fmt.Errorf("streamable_http MCP server requires url")
		}
		return &sdkmcp.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient}, nil
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("sse MCP server requires url")
		}
		return &sdkmcp.SSEClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %s", transport)
	}
}

type headerRoundTripper struct {
	headers map[string]string
	base    http.RoundTripper
}

func (rt headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	next := rt.base
	if next == nil {
		next = http.DefaultTransport
	}
	req = req.Clone(req.Context())
	for k, v := range rt.headers {
		req.Header.Set(k, v)
	}
	return next.RoundTrip(req)
}

type sdkMCPClientSession struct {
	session *sdkmcp.ClientSession
}

func (s *sdkMCPClientSession) ServerInstructions() string {
	if s == nil || s.session == nil || s.session.InitializeResult() == nil {
		return ""
	}
	return s.session.InitializeResult().Instructions
}

func (s *sdkMCPClientSession) ServerDisplayName() string {
	if s == nil || s.session == nil || s.session.InitializeResult() == nil {
		return ""
	}
	info := s.session.InitializeResult().ServerInfo
	if info == nil {
		return ""
	}
	return info.Name
}

func (s *sdkMCPClientSession) ListTools(ctx context.Context) ([]MCPToolMeta, error) {
	res, err := s.session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]MCPToolMeta, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, MCPToolMeta{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schemaToMap(t.InputSchema),
		})
	}
	return out, nil
}

func (s *sdkMCPClientSession) CallTool(ctx context.Context, name string, args map[string]any) (MCPCallResult, error) {
	res, err := s.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return MCPCallResult{}, err
	}
	return callToolResult(res), nil
}

func (s *sdkMCPClientSession) ListResources(ctx context.Context) ([]MCPResourceMeta, error) {
	res, err := s.session.ListResources(ctx, &sdkmcp.ListResourcesParams{})
	if err != nil {
		return nil, err
	}
	out := make([]MCPResourceMeta, 0, len(res.Resources))
	for _, r := range res.Resources {
		out = append(out, MCPResourceMeta{
			URI:         r.URI,
			Name:        firstNonEmpty(r.Title, r.Name),
			Description: r.Description,
			MIMEType:    r.MIMEType,
		})
	}
	return out, nil
}

func (s *sdkMCPClientSession) ReadResource(ctx context.Context, uri string) (MCPCallResult, error) {
	res, err := s.session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: uri})
	if err != nil {
		return MCPCallResult{}, err
	}
	content := make([]llm.Content, 0, len(res.Contents))
	for _, item := range res.Contents {
		if item.Text != "" {
			content = append(content, llm.TextContent{Text: item.Text})
			continue
		}
		if len(item.Blob) > 0 {
			content = append(content, llm.TextContent{Text: fmt.Sprintf("[resource %s: %d bytes]", item.URI, len(item.Blob))})
		}
	}
	return MCPCallResult{Content: content, Details: res.Contents}, nil
}

func (s *sdkMCPClientSession) ListPrompts(ctx context.Context) ([]MCPPromptMeta, error) {
	res, err := s.session.ListPrompts(ctx, &sdkmcp.ListPromptsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]MCPPromptMeta, 0, len(res.Prompts))
	for _, p := range res.Prompts {
		args := make([]MCPPromptArgMeta, 0, len(p.Arguments))
		for _, arg := range p.Arguments {
			args = append(args, MCPPromptArgMeta{
				Name:        arg.Name,
				Description: arg.Description,
				Required:    arg.Required,
			})
		}
		out = append(out, MCPPromptMeta{Name: p.Name, Description: p.Description, Arguments: args})
	}
	return out, nil
}

func (s *sdkMCPClientSession) GetPrompt(ctx context.Context, name string, args map[string]any) (MCPCallResult, error) {
	res, err := s.session.GetPrompt(ctx, &sdkmcp.GetPromptParams{Name: name, Arguments: stringArgs(args)})
	if err != nil {
		return MCPCallResult{}, err
	}
	data, err := json.MarshalIndent(res.Messages, "", "  ")
	if err != nil {
		return MCPCallResult{}, err
	}
	return MCPCallResult{
		Content: []llm.Content{llm.TextContent{Text: string(data)}},
		Details: res,
	}, nil
}

func (s *sdkMCPClientSession) Close() error {
	return s.session.Close()
}

func callToolResult(res *sdkmcp.CallToolResult) MCPCallResult {
	if res == nil {
		return MCPCallResult{}
	}
	return MCPCallResult{
		Content: convertMCPContent(res.Content),
		Details: map[string]any{
			"structuredContent": res.StructuredContent,
			"meta":              res.Meta,
		},
		IsError: res.IsError,
	}
}

func convertMCPContent(blocks []sdkmcp.Content) []llm.Content {
	out := make([]llm.Content, 0, len(blocks))
	for _, block := range blocks {
		switch c := block.(type) {
		case *sdkmcp.TextContent:
			out = append(out, llm.TextContent{Text: c.Text})
		case *sdkmcp.ImageContent:
			out = append(out, llm.ImageContent{Data: string(c.Data), MimeType: c.MIMEType})
		case *sdkmcp.EmbeddedResource:
			if c.Resource == nil {
				continue
			}
			if c.Resource.Text != "" {
				out = append(out, llm.TextContent{Text: c.Resource.Text})
			} else {
				out = append(out, llm.TextContent{Text: fmt.Sprintf("[resource %s: %d bytes]", c.Resource.URI, len(c.Resource.Blob))})
			}
		default:
			data, err := json.Marshal(block)
			if err != nil {
				out = append(out, llm.TextContent{Text: fmt.Sprintf("[%T]", block)})
			} else {
				out = append(out, llm.TextContent{Text: string(data)})
			}
		}
	}
	return out
}

func schemaToMap(v any) map[string]any {
	if v == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return normalizeMCPJSONSchema(out)
}

func normalizeMCPJSONSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if _, ok := schema["type"]; !ok {
		schema["type"] = "object"
	}
	if schema["type"] == "object" {
		if _, ok := schema["properties"]; !ok {
			schema["properties"] = map[string]any{}
		}
	}
	return schema
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func stringArgs(args map[string]any) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		switch x := v.(type) {
		case string:
			out[k] = x
		default:
			out[k] = fmt.Sprintf("%v", x)
		}
	}
	return out
}
