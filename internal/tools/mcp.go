package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// MCP Tool - Model Context Protocol Client
// ============================================================================

// MCPTool provides access to MCP servers and their tools
type MCPTool struct {
	BaseTool
	servers     map[string]*MCPServerConfig
	tools       map[string]*MCPToolDef
	mu          sync.RWMutex
	httpClient  *http.Client
}

// MCPServerConfig holds MCP server configuration
type MCPServerConfig struct {
	Name         string
	Transport    string // "stdio", "sse", "streamable_http", "streamableHttp"
	Command      string
	Args         []string
	Env          map[string]string
	URL          string
	Headers      map[string]string
	Timeout      int
	EnabledTools []string
	ToolTimeout  int
}

// MCPToolDef represents a tool from an MCP server
type MCPToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	ServerName  string
}

// NewMCPTool creates a new MCP tool manager
func NewMCPTool() *MCPTool {
	return &MCPTool{
		BaseTool:   BaseTool{},
		servers:    make(map[string]*MCPServerConfig),
		tools:      make(map[string]*MCPToolDef),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ConfigureServer adds an MCP server configuration
func (t *MCPTool) ConfigureServer(name string, config *MCPServerConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	config.Name = name
	t.servers[name] = config
}

// Name and Description for the composite MCP tool
func (t *MCPTool) Name() string    { return "mcp" }
func (t *MCPTool) Description() string { return "Call tools from MCP (Model Context Protocol) servers" }

// Parameters returns the schema for calling an MCP tool
func (t *MCPTool) Parameters() map[string]any {
	t.mu.RLock()
	defer t.mu.RUnlock()

	props := map[string]any{
		"server": map[string]any{
			"type":        "string",
			"description": "MCP server name",
		},
		"tool": map[string]any{
			"type":        "string",
			"description": "Tool name on the MCP server",
		},
		"arguments": map[string]any{
			"type":        "object",
			"description": "Tool arguments as key-value pairs",
		},
	}

	// Build enum for server
	serverNames := make([]string, 0, len(t.servers))
	for name := range t.servers {
		serverNames = append(serverNames, name)
	}
	if len(serverNames) > 0 {
		props["server"].(map[string]any)["enum"] = serverNames
	}

	return map[string]any{
		"type": "object",
		"properties": props,
		"required": []any{"server", "tool", "arguments"},
	}
}

// Execute handles MCP tool calls
func (t *MCPTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	serverName, _ := params["server"].(string)
	toolName, _ := params["tool"].(string)
	args, _ := params["arguments"].(map[string]any)

	if serverName == "" || toolName == "" {
		return nil, fmt.Errorf("server and tool are required")
	}

	t.mu.RLock()
	server, ok := t.servers[serverName]
	t.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("MCP server not found: %s", serverName)
	}

	timeout := server.ToolTimeout
	if timeout <= 0 {
		timeout = 30
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	switch server.Transport {
	case "stdio":
		return t.callStdio(ctx, server, toolName, args)
	case "sse", "streamable_http", "streamableHttp":
		return t.callHTTP(ctx, server, toolName, args)
	default:
		return nil, fmt.Errorf("unsupported transport: %s", server.Transport)
	}
}

// callStdio calls an MCP tool via stdio transport
func (t *MCPTool) callStdio(ctx context.Context, server *MCPServerConfig, toolName string, args map[string]any) (any, error) {
	// For stdio transport, we would need to spawn the MCP server process
	// and communicate via its stdin/stdout. This requires proper process
	// management which is complex. For now, return an error indicating
	// HTTP transport should be used.
	//
	// In a production implementation, you would:
	// 1. Spawn the server process with the configured command/args/env
	// 2. Send JSON-RPC requests via stdin
	// 3. Read responses from stdout
	// 4. Handle process lifecycle (start, restart, stop)
	_ = toolName
	_ = args
	return nil, fmt.Errorf("stdio transport requires HTTP-based MCP server - use sse or streamable_http transport")
}

// callHTTP calls an MCP tool via HTTP transport
func (t *MCPTool) callHTTP(ctx context.Context, server *MCPServerConfig, toolName string, args map[string]any) (any, error) {
	// Build JSON-RPC request
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine endpoint
	endpoint := server.URL
	if server.Transport == "sse" {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/sse"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range server.Headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP server error: %d - %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var result MCPResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("MCP error: %s", result.Error.Message)
	}

	// Extract content from response
	if len(result.Result.Content) > 0 {
		return formatMCPContent(result.Result.Content), nil
	}

	return "(no output)", nil
}

// MCPJSONRPCRequest represents a JSON-RPC request
type MCPJSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

// MCPResponse represents a JSON-RPC response
type MCPResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID     int            `json:"id"`
	Result *MCPResult     `json:"result,omitempty"`
	Error  *MCPErrorDetail `json:"error,omitempty"`
}

// MCPResult represents a successful result
type MCPResult struct {
	Content []MCPContentBlock `json:"content"`
}

// MCPContentBlock represents a content block in the response
type MCPContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPErrorDetail represents an error
type MCPErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func formatMCPContent(blocks []MCPContentBlock) string {
	var parts []string
	for _, block := range blocks {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		} else {
			parts = append(parts, fmt.Sprintf("[%s: %s]", block.Type, block.Text))
		}
	}
	return strings.Join(parts, "\n")
}

// ListTools returns all available tools from all configured MCP servers
func (t *MCPTool) ListTools() []*MCPToolDef {
	t.mu.RLock()
	defer t.mu.RUnlock()

	tools := make([]*MCPToolDef, 0, len(t.tools))
	for _, tool := range t.tools {
		tools = append(tools, tool)
	}
	return tools
}

// MCPServerManager handles MCP server lifecycle
type MCPServerManager struct {
	mu      sync.RWMutex
	servers map[string]*MCPClientSession
}

// MCPClientSession represents an active MCP server session
type MCPClientSession struct {
	Name    string
	Config  *MCPServerConfig
	Tools   []*MCPToolDef
	Session map[string]any // Server-side session data
}

// NewMCPServerManager creates a new MCP server manager
func NewMCPServerManager() *MCPServerManager {
	return &MCPServerManager{
		servers: make(map[string]*MCPClientSession),
	}
}

// InitializeServer connects to an MCP server and lists its tools
func (m *MCPServerManager) InitializeServer(ctx context.Context, config *MCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session := &MCPClientSession{
		Name:   config.Name,
		Config: config,
		Tools:  []*MCPToolDef{},
	}

	switch config.Transport {
	case "stdio":
		// Initialize stdio connection - requires process management
		// For production, use proper subprocess handling with pty
	case "sse", "streamable_http", "streamableHttp":
		// Initialize HTTP connection
		tools, err := m.listToolsHTTP(ctx, config)
		if err != nil {
			return fmt.Errorf("failed to list tools from %s: %w", config.Name, err)
		}
		session.Tools = tools
	default:
		return fmt.Errorf("unsupported transport: %s", config.Transport)
	}

	m.servers[config.Name] = session
	return nil
}

func (m *MCPServerManager) listToolsHTTP(ctx context.Context, config *MCPServerConfig) ([]*MCPToolDef, error) {
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	endpoint := config.URL
	if config.Transport == "sse" {
		endpoint = strings.TrimSuffix(endpoint, "/") + "/sse"
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(requestBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range config.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP server error: %d - %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []struct {
				Name        string         `json:"name"`
				Description string         `json:"description"`
				InputSchema map[string]any `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	tools := make([]*MCPToolDef, len(result.Result.Tools))
	for i, t := range result.Result.Tools {
		tools[i] = &MCPToolDef{
			Name:        fmt.Sprintf("mcp_%s_%s", config.Name, t.Name),
			Description: t.Description,
			InputSchema: normalizeJSONSchema(t.InputSchema),
			ServerName:  config.Name,
		}
	}

	return tools, nil
}

// normalizeJSONSchema normalizes a JSON schema for OpenAI compatibility
func normalizeJSONSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}

	normalized := make(map[string]any)

	// Handle type unions like ["string", "null"]
	if t, ok := schema["type"].([]any); ok {
		nonNull := ""
		hasNull := false
		for _, tt := range t {
			if ts, ok := tt.(string); ok {
				if ts == "null" {
					hasNull = true
				} else {
					nonNull = ts
				}
			}
		}
		if nonNull != "" {
			normalized["type"] = nonNull
			if hasNull {
				normalized["nullable"] = true
			}
		}
	} else if t, ok := schema["type"].(string); ok {
		normalized["type"] = t
	}

	// Copy properties recursively
	if props, ok := schema["properties"].(map[string]any); ok {
		normalized["properties"] = props
	}

	// Copy other fields
	for k, v := range schema {
		if k != "type" && k != "properties" {
			normalized[k] = v
		}
	}

	return normalized
}

// GetServer returns a server session by name
func (m *MCPServerManager) GetServer(name string) (*MCPClientSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.servers[name]
	return s, ok
}

// StopServer shuts down a server session
func (m *MCPServerManager) StopServer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.servers[name]
	if !ok {
		return fmt.Errorf("server not found: %s", name)
	}

	// Clean up session based on transport type
	switch session.Config.Transport {
	case "stdio":
		// Kill the process
	case "sse", "streamable_http", "streamableHttp":
		// Close HTTP connections
	}

	delete(m.servers, name)
	return nil
}