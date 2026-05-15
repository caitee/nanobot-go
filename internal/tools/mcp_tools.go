package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"

	"ori/internal/llm"
	"ori/internal/tool"
)

// NewMCPProxyTool creates the low-token proxy entrypoint for MCP servers.
func NewMCPProxyTool(manager *MCPManager) tool.AgentTool {
	return &mcpProxyTool{manager: manager}
}

// NewMCPSearchTool creates a task-oriented MCP metadata search tool.
func NewMCPSearchTool(manager *MCPManager) tool.AgentTool {
	return &mcpSearchTool{manager: manager}
}

// NewMCPDescribeTool creates a task-oriented MCP tool schema lookup tool.
func NewMCPDescribeTool(manager *MCPManager) tool.AgentTool {
	return &mcpDescribeTool{manager: manager}
}

// NewMCPCallTool creates a task-oriented MCP remote tool invocation wrapper.
func NewMCPCallTool(manager *MCPManager) tool.AgentTool {
	return &mcpCallTool{manager: manager}
}

type mcpProxyTool struct {
	manager *MCPManager
}

type mcpSearchTool struct {
	manager *MCPManager
}

func (t *mcpSearchTool) Name() string  { return "mcp_search" }
func (t *mcpSearchTool) Label() string { return "MCP Search" }
func (t *mcpSearchTool) Description() string {
	return "Search configured MCP server and tool metadata by task text. Use this to discover which MCP server/tool can help before calling mcp_describe or mcp_call."
}
func (t *mcpSearchTool) Parameters() map[string]any {
	props := map[string]any{
		"query": map[string]any{
			"type":        "string",
			"description": "Task, capability, server, or tool text to search for.",
		},
		"server": map[string]any{
			"type":        "string",
			"description": "Optional MCP server name to restrict results.",
		},
	}
	addMCPServerEnum(props, t.manager)
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []any{"query"},
	}
}
func (t *mcpSearchTool) ExecutionMode() tool.ExecutionMode { return tool.ExecutionDefault }
func (t *mcpSearchTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	return raw, nil
}
func (t *mcpSearchTool) Execute(ctx context.Context, callID string, args map[string]any, update tool.UpdateFn) (*tool.Result, error) {
	_ = callID
	_ = update
	query, _ := args["query"].(string)
	server, _ := args["server"].(string)
	tools, err := t.manager.SearchTools(ctx, query)
	if err != nil {
		return nil, err
	}
	return jsonTextResult(filterMCPToolsByServer(tools, server))
}

type mcpDescribeTool struct {
	manager *MCPManager
}

func (t *mcpDescribeTool) Name() string  { return "mcp_describe" }
func (t *mcpDescribeTool) Label() string { return "MCP Describe" }
func (t *mcpDescribeTool) Description() string {
	return "Describe one remote MCP tool, including its description and input schema. Use this before mcp_call when argument shape is unclear."
}
func (t *mcpDescribeTool) Parameters() map[string]any {
	props := map[string]any{
		"server": map[string]any{
			"type":        "string",
			"description": "MCP server name.",
		},
		"tool": map[string]any{
			"type":        "string",
			"description": "Remote MCP tool name.",
		},
	}
	addMCPServerEnum(props, t.manager)
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []any{"server", "tool"},
	}
}
func (t *mcpDescribeTool) ExecutionMode() tool.ExecutionMode { return tool.ExecutionDefault }
func (t *mcpDescribeTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	return raw, nil
}
func (t *mcpDescribeTool) Execute(ctx context.Context, callID string, args map[string]any, update tool.UpdateFn) (*tool.Result, error) {
	_ = callID
	_ = update
	server, _ := args["server"].(string)
	name, _ := args["tool"].(string)
	meta, err := t.manager.DescribeTool(ctx, server, name)
	if err != nil {
		return nil, err
	}
	return jsonTextResult(meta)
}

type mcpCallTool struct {
	manager *MCPManager
}

func (t *mcpCallTool) Name() string  { return "mcp_call" }
func (t *mcpCallTool) Label() string { return "MCP Call" }
func (t *mcpCallTool) Description() string {
	return "Call one remote MCP tool by server and tool name. Put the remote tool input object in arguments."
}
func (t *mcpCallTool) Parameters() map[string]any {
	props := map[string]any{
		"server": map[string]any{
			"type":        "string",
			"description": "MCP server name.",
		},
		"tool": map[string]any{
			"type":        "string",
			"description": "Remote MCP tool name.",
		},
		"arguments": map[string]any{
			"type":        "object",
			"description": "Arguments for the remote MCP tool.",
		},
	}
	addMCPServerEnum(props, t.manager)
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []any{"server", "tool", "arguments"},
	}
}
func (t *mcpCallTool) ExecutionMode() tool.ExecutionMode { return tool.ExecutionDefault }
func (t *mcpCallTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	prepared := make(map[string]any, len(raw))
	for k, v := range raw {
		prepared[k] = v
	}
	if parsed, ok := parseObjectArgument(prepared["arguments"]); ok {
		prepared["arguments"] = parsed
	}
	return prepared, nil
}
func (t *mcpCallTool) Execute(ctx context.Context, callID string, args map[string]any, update tool.UpdateFn) (*tool.Result, error) {
	_ = callID
	_ = update
	server, _ := args["server"].(string)
	name, _ := args["tool"].(string)
	arguments, _ := args["arguments"].(map[string]any)
	result, err := t.manager.CallTool(ctx, server, name, arguments)
	if err != nil {
		return nil, err
	}
	return &tool.Result{Content: ensureContent(result.Content), Details: result.Details}, nil
}

func addMCPServerEnum(props map[string]any, manager *MCPManager) {
	serverProp, ok := props["server"].(map[string]any)
	if !ok || manager == nil {
		return
	}
	var servers []any
	for _, server := range manager.serverList() {
		servers = append(servers, server.Name)
	}
	if len(servers) > 0 {
		serverProp["enum"] = servers
	}
}

func (t *mcpProxyTool) Name() string  { return "mcp" }
func (t *mcpProxyTool) Label() string { return "MCP" }
func (t *mcpProxyTool) Description() string {
	return "Discover and call tools, resources, and prompt templates from configured MCP servers. action=search only searches MCP tool metadata; it does not run remote tools. To use a remote MCP tool, use action=call with server, tool, and arguments."
}
func (t *mcpProxyTool) ExecutionMode() tool.ExecutionMode { return tool.ExecutionDefault }
func (t *mcpProxyTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	prepared := make(map[string]any, len(raw)+1)
	for k, v := range raw {
		prepared[k] = v
	}
	action, _ := prepared["action"].(string)
	if action != "call" && action != "tools" {
		return prepared, nil
	}
	if hasToolArguments(prepared["arguments"]) {
		if parsed, ok := parseObjectArgument(prepared["arguments"]); ok {
			prepared["arguments"] = parsed
		}
		return prepared, nil
	}
	if parsed, ok := parseObjectArgument(prepared["uri"]); ok {
		prepared["arguments"] = parsed
		return prepared, nil
	}
	if query, ok := prepared["query"].(string); ok && strings.TrimSpace(query) != "" {
		prepared["arguments"] = map[string]any{"query": query}
	}
	return prepared, nil
}

func (t *mcpProxyTool) Parameters() map[string]any {
	props := map[string]any{
		"action": map[string]any{
			"type": "string",
			"enum": []any{
				"status", "list", "search", "describe", "connect", "call",
				"tools", "resources", "prompts",
			},
			"description": "MCP action. search finds MCP tool metadata only. call invokes a remote MCP tool. Legacy aliases: tools/resources/prompts.",
		},
		"server": map[string]any{"type": "string", "description": "MCP server name"},
		"query": map[string]any{
			"type":        "string",
			"description": "Metadata search text for action=search. Do not put remote tool input here; use arguments with action=call instead.",
		},
		"tool": map[string]any{"type": "string", "description": "Remote MCP tool name for call/describe, for example web_search"},
		"name": map[string]any{"type": "string", "description": "Prompt name for prompts/get"},
		"arguments": map[string]any{
			"type":        "object",
			"description": "Arguments for a remote MCP tool call or prompt template. Put remote tool inputs here when action=call.",
		},
		"resource_action": map[string]any{
			"type":        "string",
			"enum":        []any{"list", "read"},
			"description": "Legacy resources action",
		},
		"prompt_action": map[string]any{
			"type":        "string",
			"enum":        []any{"list", "get"},
			"description": "Legacy prompts action",
		},
		"uri": map[string]any{"type": "string", "description": "Resource URI for resources/read"},
	}
	var servers []any
	for _, server := range t.manager.serverList() {
		servers = append(servers, server.Name)
	}
	if len(servers) > 0 {
		props["server"].(map[string]any)["enum"] = servers
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []any{"action"},
	}
}

func (t *mcpProxyTool) Execute(ctx context.Context, callID string, args map[string]any, update tool.UpdateFn) (*tool.Result, error) {
	_ = callID
	_ = update
	action, _ := args["action"].(string)
	switch action {
	case "status", "":
		return textResult(formatMCPStatus(t.manager.Status()), nil), nil
	case "connect":
		return t.executeConnect(ctx, args)
	case "list":
		return t.executeList(ctx, args)
	case "search":
		server, _ := args["server"].(string)
		query, _ := args["query"].(string)
		tools, err := t.manager.SearchTools(ctx, query)
		if err != nil {
			return nil, err
		}
		tools = filterMCPToolsByServer(tools, server)
		return jsonTextResult(tools)
	case "describe":
		server, _ := args["server"].(string)
		name, _ := args["tool"].(string)
		if name == "" {
			name, _ = args["name"].(string)
		}
		meta, err := t.manager.DescribeTool(ctx, server, name)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(meta)
	case "call", "tools":
		return t.executeToolCall(ctx, args, action)
	case "resources":
		return t.executeResources(ctx, args)
	case "prompts":
		return t.executePrompts(ctx, args)
	default:
		return nil, fmt.Errorf("unknown MCP action: %s", action)
	}
}

func (t *mcpProxyTool) executeConnect(ctx context.Context, args map[string]any) (*tool.Result, error) {
	server, _ := args["server"].(string)
	if server != "" {
		if err := t.manager.ConnectServer(ctx, server); err != nil {
			return nil, err
		}
		return textResult("connected "+server, nil), nil
	}
	var connected []string
	for _, cfg := range t.manager.serverList() {
		if !cfg.Enabled {
			continue
		}
		if err := t.manager.ConnectServer(ctx, cfg.Name); err != nil {
			return nil, err
		}
		connected = append(connected, cfg.Name)
	}
	return textResult("connected "+strings.Join(connected, ", "), connected), nil
}

func (t *mcpProxyTool) executeList(ctx context.Context, args map[string]any) (*tool.Result, error) {
	server, _ := args["server"].(string)
	tools, err := t.manager.ListTools(ctx, server)
	if err != nil {
		return nil, err
	}
	resources, _ := t.manager.ListResources(ctx, server)
	prompts, _ := t.manager.ListPrompts(ctx, server)
	return jsonTextResult(map[string]any{
		"tools":     tools,
		"resources": resources,
		"prompts":   prompts,
	})
}

func (t *mcpProxyTool) executeToolCall(ctx context.Context, args map[string]any, action string) (*tool.Result, error) {
	server, _ := args["server"].(string)
	name, _ := args["tool"].(string)
	if action == "tools" && name == "" {
		tools, err := t.manager.ListTools(ctx, server)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(tools)
	}
	arguments, _ := args["arguments"].(map[string]any)
	result, err := t.manager.CallTool(ctx, server, name, arguments)
	if err != nil {
		return nil, err
	}
	return &tool.Result{Content: ensureContent(result.Content), Details: result.Details}, nil
}

func (t *mcpProxyTool) executeResources(ctx context.Context, args map[string]any) (*tool.Result, error) {
	server, _ := args["server"].(string)
	action, _ := args["resource_action"].(string)
	if action == "" || action == "list" {
		resources, err := t.manager.ListResources(ctx, server)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(resources)
	}
	if action != "read" {
		return nil, fmt.Errorf("unknown resource action: %s", action)
	}
	uri, _ := args["uri"].(string)
	result, err := t.manager.ReadResource(ctx, server, uri)
	if err != nil {
		return nil, err
	}
	return &tool.Result{Content: ensureContent(result.Content), Details: result.Details}, nil
}

func (t *mcpProxyTool) executePrompts(ctx context.Context, args map[string]any) (*tool.Result, error) {
	server, _ := args["server"].(string)
	action, _ := args["prompt_action"].(string)
	if action == "" || action == "list" {
		prompts, err := t.manager.ListPrompts(ctx, server)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(prompts)
	}
	if action != "get" {
		return nil, fmt.Errorf("unknown prompt action: %s", action)
	}
	name, _ := args["name"].(string)
	arguments, _ := args["arguments"].(map[string]any)
	result, err := t.manager.GetPrompt(ctx, server, name, arguments)
	if err != nil {
		return nil, err
	}
	return &tool.Result{Content: ensureContent(result.Content), Details: result.Details}, nil
}

type mcpDirectTool struct {
	manager       *MCPManager
	name          string
	serverName    string
	serverPurpose string
	meta          MCPToolMeta
}

func newMCPDirectTool(manager *MCPManager, name, serverName string, meta MCPToolMeta) tool.AgentTool {
	return newMCPDirectToolWithPurpose(manager, name, serverName, meta, "")
}

func newMCPDirectToolWithPurpose(
	manager *MCPManager, name, serverName string, meta MCPToolMeta, serverPurpose string,
) tool.AgentTool {
	return &mcpDirectTool{
		manager:       manager,
		name:          name,
		serverName:    serverName,
		serverPurpose: shortMCPPurpose(serverPurpose),
		meta:          meta,
	}
}

// IsMCPDirectTool reports whether t is a direct wrapper for one remote MCP
// tool. Hosts use it to refresh only generated MCP tools.
func IsMCPDirectTool(t tool.AgentTool) bool {
	_, ok := t.(*mcpDirectTool)
	return ok
}

func (t *mcpDirectTool) Name() string  { return t.name }
func (t *mcpDirectTool) Label() string { return t.meta.Name }
func (t *mcpDirectTool) Description() string {
	desc := strings.TrimSpace(t.meta.Description)
	if desc == "" {
		desc = "MCP tool"
	}
	if t.serverPurpose != "" {
		return fmt.Sprintf("%s (MCP server: %s; server purpose: %s; tool: %s)", desc, t.serverName, t.serverPurpose, t.meta.Name)
	}
	return fmt.Sprintf("%s (MCP server: %s, tool: %s)", desc, t.serverName, t.meta.Name)
}
func (t *mcpDirectTool) Parameters() map[string]any {
	return normalizeMCPJSONSchema(t.meta.InputSchema)
}
func (t *mcpDirectTool) ExecutionMode() tool.ExecutionMode { return tool.ExecutionDefault }
func (t *mcpDirectTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	return raw, nil
}
func (t *mcpDirectTool) Execute(ctx context.Context, callID string, args map[string]any, update tool.UpdateFn) (*tool.Result, error) {
	_ = callID
	_ = update
	result, err := t.manager.CallTool(ctx, t.serverName, t.meta.Name, args)
	if err != nil {
		return nil, err
	}
	return &tool.Result{Content: ensureContent(result.Content), Details: result.Details}, nil
}

func stableMCPDirectToolName(serverName, toolName string, used map[string]bool) string {
	base := sanitizeToolName("mcp_" + serverName + "_" + toolName)
	if base == "" {
		base = "mcp_tool"
	}
	name := truncateToolName(base)
	if !used[name] {
		return name
	}
	hash := shortHash(serverName + ":" + toolName)
	name = truncateToolName(base + "_" + hash)
	for i := 2; used[name]; i++ {
		name = truncateToolName(fmt.Sprintf("%s_%s_%d", base, hash, i))
	}
	return name
}

var nonToolNameChar = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeToolName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "_")
	name = nonToolNameChar.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return name
}

func truncateToolName(name string) string {
	const maxToolName = 64
	if len(name) <= maxToolName {
		return name
	}
	hash := shortHash(name)
	prefixLen := maxToolName - len(hash) - 1
	if prefixLen < 1 {
		return hash
	}
	return strings.TrimRight(name[:prefixLen], "_") + "_" + hash
}

func shortHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

func mcpServerPurpose(server MCPServerConfig, meta MCPServerMetadata) string {
	for _, item := range []string{server.Description, server.Instructions, meta.Instructions} {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shortMCPPurpose(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	const maxPurpose = 140
	if len(value) <= maxPurpose {
		return value
	}
	return strings.TrimSpace(value[:maxPurpose-3]) + "..."
}

func textResult(text string, details any) *tool.Result {
	return &tool.Result{
		Content: []llm.Content{llm.TextContent{Text: text}},
		Details: details,
	}
}

func jsonTextResult(v any) (*tool.Result, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return textResult(string(data), v), nil
}

func ensureContent(content []llm.Content) []llm.Content {
	if len(content) > 0 {
		return content
	}
	return []llm.Content{llm.TextContent{Text: "(no output)"}}
}

func filterMCPToolsByServer(tools []MCPToolMeta, server string) []MCPToolMeta {
	server = strings.TrimSpace(server)
	if server == "" {
		return tools
	}
	out := make([]MCPToolMeta, 0, len(tools))
	for _, item := range tools {
		if item.ServerName == server {
			out = append(out, item)
		}
	}
	return out
}

func hasToolArguments(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case map[string]any:
		return len(x) > 0
	case string:
		return strings.TrimSpace(x) != ""
	default:
		return true
	}
}

func parseObjectArgument(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			return nil, false
		}
		return x, true
	case string:
		x = strings.TrimSpace(x)
		if x == "" || !strings.HasPrefix(x, "{") {
			return nil, false
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(x), &out); err != nil || len(out) == 0 {
			return nil, false
		}
		return out, true
	default:
		return nil, false
	}
}

func formatMCPStatus(status []MCPServerStatus) string {
	if len(status) == 0 {
		return "No MCP servers configured."
	}
	sort.Slice(status, func(i, j int) bool { return status[i].Name < status[j].Name })
	var b strings.Builder
	for _, item := range status {
		state := "disconnected"
		if item.Connected {
			state = "connected"
		}
		enabled := "disabled"
		if item.Enabled {
			enabled = "enabled"
		}
		fmt.Fprintf(&b, "%s: %s, %s, lifecycle=%s, tools=%d, resources=%d, prompts=%d",
			item.Name, enabled, state, item.Lifecycle, item.Tools, item.Resources, item.Prompts)
		if item.LastError != "" {
			fmt.Fprintf(&b, ", last_error=%s", item.LastError)
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}
