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

type mcpProxyTool struct {
	manager *MCPManager
}

func (t *mcpProxyTool) Name() string  { return "mcp" }
func (t *mcpProxyTool) Label() string { return "MCP" }
func (t *mcpProxyTool) Description() string {
	return "Discover and call tools, resources, and prompt templates from configured MCP servers. Prefer search or describe before call when the exact server/tool is unknown."
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
			"description": "MCP action. New actions: status/list/search/describe/connect/call. Legacy aliases: tools/resources/prompts.",
		},
		"server": map[string]any{"type": "string", "description": "MCP server name"},
		"query":  map[string]any{"type": "string", "description": "Search query"},
		"tool":   map[string]any{"type": "string", "description": "Tool name for call/describe"},
		"name":   map[string]any{"type": "string", "description": "Prompt name for prompts/get"},
		"arguments": map[string]any{
			"type":        "object",
			"description": "Arguments for a tool call or prompt template",
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
		query, _ := args["query"].(string)
		tools, err := t.manager.SearchTools(ctx, query)
		if err != nil {
			return nil, err
		}
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
	manager    *MCPManager
	name       string
	serverName string
	meta       MCPToolMeta
}

func newMCPDirectTool(manager *MCPManager, name, serverName string, meta MCPToolMeta) tool.AgentTool {
	return &mcpDirectTool{manager: manager, name: name, serverName: serverName, meta: meta}
}

func (t *mcpDirectTool) Name() string  { return t.name }
func (t *mcpDirectTool) Label() string { return t.meta.Name }
func (t *mcpDirectTool) Description() string {
	desc := strings.TrimSpace(t.meta.Description)
	if desc == "" {
		desc = "MCP tool"
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
