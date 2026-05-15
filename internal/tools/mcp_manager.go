package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"ori/internal/llm"
	"ori/internal/tool"
)

// MCPManagerOptions configures MCPManager.
type MCPManagerOptions struct {
	Config        *MCPConfig
	Cache         *MCPMetadataCache
	CachePath     string
	ClientFactory MCPClientFactory
	Now           func() time.Time
}

// MCPManager owns MCP sessions, metadata, cache refresh, and lifecycle.
type MCPManager struct {
	mu            sync.Mutex
	config        *MCPConfig
	cache         *MCPMetadataCache
	cachePath     string
	clientFactory MCPClientFactory
	now           func() time.Time
	sessions      map[string]*mcpSessionState
	metadataHook  func()
}

type mcpSessionState struct {
	session      MCPClientSession
	lastUsed     time.Time
	failureUntil time.Time
	lastError    string
}

// MCPServerStatus is a user-facing server status snapshot.
type MCPServerStatus struct {
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Lifecycle string `json:"lifecycle"`
	Tools     int    `json:"tools"`
	Resources int    `json:"resources"`
	Prompts   int    `json:"prompts"`
	LastError string `json:"lastError,omitempty"`
}

// MCPServerCatalogItem describes a configured MCP server for model-facing
// discovery without exposing transport secrets.
type MCPServerCatalogItem struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName,omitempty"`
	Enabled      bool   `json:"enabled"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Tools        int    `json:"tools"`
	Resources    int    `json:"resources"`
	Prompts      int    `json:"prompts"`
}

// NewMCPManager creates a manager.
func NewMCPManager(opts MCPManagerOptions) *MCPManager {
	cfg := opts.Config
	if cfg == nil {
		cfg = &MCPConfig{Servers: map[string]MCPServerConfig{}}
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]MCPServerConfig{}
	}
	for name, server := range cfg.Servers {
		if !server.Enabled && !server.EnabledSet {
			server.Enabled = true
			cfg.Servers[name] = server
		}
	}
	if cfg.Settings.IdleTimeout == 0 {
		cfg.Settings.IdleTimeout = defaultMCPIdleTimeout
	}
	if cfg.Settings.FailureBackoff == 0 {
		cfg.Settings.FailureBackoff = defaultMCPBackoff
	}
	cache := opts.Cache
	if cache == nil {
		cache = &MCPMetadataCache{Version: mcpCacheVersion, Servers: map[string]MCPServerMetadata{}}
	}
	if cache.Servers == nil {
		cache.Servers = map[string]MCPServerMetadata{}
	}
	factory := opts.ClientFactory
	if factory == nil {
		factory = &sdkMCPClientFactory{}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	cachePath := opts.CachePath
	if cachePath == "" {
		cachePath = cfg.Settings.CachePath
	}
	return &MCPManager{
		config:        cfg,
		cache:         cache,
		cachePath:     cachePath,
		clientFactory: factory,
		now:           now,
		sessions:      map[string]*mcpSessionState{},
	}
}

// Start opens eager and keep-alive sessions.
func (m *MCPManager) Start(ctx context.Context) error {
	var errs []string
	for _, server := range m.serverList() {
		if !server.Enabled {
			continue
		}
		if server.Lifecycle == MCPLifecycleEager || server.Lifecycle == MCPLifecycleKeepAlive {
			if err := m.ConnectServer(ctx, server.Name); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", server.Name, err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// Close closes all active sessions and saves cache metadata.
func (m *MCPManager) Close() error {
	m.mu.Lock()
	sessions := make([]MCPClientSession, 0, len(m.sessions))
	for _, state := range m.sessions {
		if state.session != nil {
			sessions = append(sessions, state.session)
		}
	}
	m.sessions = map[string]*mcpSessionState{}
	cache := m.cache
	cachePath := m.cachePath
	m.mu.Unlock()

	var firstErr error
	for _, session := range sessions {
		if err := session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := cache.Save(cachePath); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

// Config returns the manager config.
func (m *MCPManager) Config() *MCPConfig {
	return m.config
}

// SetMetadataChangeHook installs a callback invoked after metadata refreshes
// successfully. Hosts use it to update model-visible MCP direct tools.
func (m *MCPManager) SetMetadataChangeHook(hook func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metadataHook = hook
}

// Status returns status for all configured servers.
func (m *MCPManager) Status() []MCPServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MCPServerStatus, 0, len(m.config.Servers))
	for _, server := range m.serverListLocked() {
		state := m.sessions[server.Name]
		meta := m.cache.Servers[server.Name]
		status := MCPServerStatus{
			Name:      server.Name,
			Enabled:   server.Enabled,
			Lifecycle: string(server.Lifecycle),
			Tools:     len(meta.Tools),
			Resources: len(meta.Resources),
			Prompts:   len(meta.Prompts),
		}
		if state != nil {
			status.Connected = state.session != nil
			status.LastError = state.lastError
		}
		out = append(out, status)
	}
	return out
}

// ServerCatalog returns model-facing server summaries and cached metadata
// counts. Configured instructions override remote server instructions.
func (m *MCPManager) ServerCatalog() []MCPServerCatalogItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MCPServerCatalogItem, 0, len(m.config.Servers))
	for _, server := range m.serverListLocked() {
		meta, hasMeta := m.cache.Servers[server.Name]
		validMeta := hasMeta && meta.ConfigHash == HashMCPServerConfig(server)
		item := MCPServerCatalogItem{
			Name:         server.Name,
			DisplayName:  server.Name,
			Enabled:      server.Enabled,
			Description:  strings.TrimSpace(server.Description),
			Instructions: strings.TrimSpace(server.Instructions),
		}
		if validMeta {
			if meta.DisplayName != "" {
				item.DisplayName = meta.DisplayName
			}
			if item.Instructions == "" {
				item.Instructions = strings.TrimSpace(meta.Instructions)
			}
			item.Tools = len(meta.Tools)
			item.Resources = len(meta.Resources)
			item.Prompts = len(meta.Prompts)
		}
		out = append(out, item)
	}
	return out
}

// SetServerEnabled updates one server's enabled flag. Disabling closes any
// active session immediately; callers handle persistence separately.
func (m *MCPManager) SetServerEnabled(ctx context.Context, name string, enabled bool) error {
	_ = ctx
	m.mu.Lock()
	server, ok := m.config.Servers[name]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("MCP server not found: %s", name)
	}
	server.Enabled = enabled
	server.EnabledSet = true
	m.config.Servers[name] = server
	state := m.sessions[name]
	if enabled || state == nil || state.session == nil {
		m.mu.Unlock()
		return nil
	}
	session := state.session
	state.session = nil
	state.lastUsed = time.Time{}
	m.mu.Unlock()
	return session.Close()
}

// ConnectServer opens a session and refreshes metadata.
func (m *MCPManager) ConnectServer(ctx context.Context, name string) error {
	session, cfg, err := m.ensureSession(ctx, name)
	if err != nil {
		return err
	}
	meta, err := collectMCPMetadata(ctx, session, cfg)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.cache.Servers[name] = meta
	cache := m.cache
	cachePath := m.cachePath
	hook := m.metadataHook
	m.mu.Unlock()
	if err := cache.Save(cachePath); err != nil {
		return err
	}
	if hook != nil {
		hook()
	}
	return nil
}

// ListTools returns tools for one server or all servers.
func (m *MCPManager) ListTools(ctx context.Context, serverName string) ([]MCPToolMeta, error) {
	if err := m.ensureMetadata(ctx, serverName); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MCPToolMeta
	for name, meta := range m.cache.Servers {
		if serverName != "" && name != serverName {
			continue
		}
		for _, tool := range meta.Tools {
			tool.ServerName = name
			out = append(out, tool)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ServerName == out[j].ServerName {
			return out[i].Name < out[j].Name
		}
		return out[i].ServerName < out[j].ServerName
	})
	return out, nil
}

// ListResources returns resources for one server or all servers.
func (m *MCPManager) ListResources(ctx context.Context, serverName string) ([]MCPResourceMeta, error) {
	if err := m.ensureMetadata(ctx, serverName); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MCPResourceMeta
	for name, meta := range m.cache.Servers {
		if serverName != "" && name != serverName {
			continue
		}
		for _, item := range meta.Resources {
			item.ServerName = name
			out = append(out, item)
		}
	}
	return out, nil
}

// ListPrompts returns prompt metadata for one server or all servers.
func (m *MCPManager) ListPrompts(ctx context.Context, serverName string) ([]MCPPromptMeta, error) {
	if err := m.ensureMetadata(ctx, serverName); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MCPPromptMeta
	for name, meta := range m.cache.Servers {
		if serverName != "" && name != serverName {
			continue
		}
		for _, item := range meta.Prompts {
			item.ServerName = name
			out = append(out, item)
		}
	}
	return out, nil
}

// SearchTools returns tools whose server/name/description include query.
func (m *MCPManager) SearchTools(ctx context.Context, query string) ([]MCPToolMeta, error) {
	tools, err := m.ListTools(ctx, "")
	if err != nil {
		return nil, err
	}
	terms := mcpQueryTerms(query)
	if len(terms) == 0 {
		return tools, nil
	}
	catalog := map[string]MCPServerCatalogItem{}
	for _, item := range m.ServerCatalog() {
		catalog[item.Name] = item
	}
	type scoredTool struct {
		tool  MCPToolMeta
		score int
	}
	scored := make([]scoredTool, 0, len(tools))
	for _, tool := range tools {
		server := catalog[tool.ServerName]
		score := mcpRelevanceScore(terms, mcpToolSearchText(tool, server))
		if score > 0 {
			scored = append(scored, scoredTool{tool: tool, score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].tool.ServerName == scored[j].tool.ServerName {
			return scored[i].tool.Name < scored[j].tool.Name
		}
		return scored[i].tool.ServerName < scored[j].tool.ServerName
	})
	out := make([]MCPToolMeta, 0, len(scored))
	for _, item := range scored {
		out = append(out, item.tool)
	}
	return out, nil
}

// DescribeTool returns metadata for one tool.
func (m *MCPManager) DescribeTool(ctx context.Context, serverName, toolName string) (MCPToolMeta, error) {
	if serverName == "" || toolName == "" {
		return MCPToolMeta{}, fmt.Errorf("server and tool are required")
	}
	tools, err := m.ListTools(ctx, serverName)
	if err != nil {
		return MCPToolMeta{}, err
	}
	for _, tool := range tools {
		if tool.Name == toolName {
			return tool, nil
		}
	}
	return MCPToolMeta{}, fmt.Errorf("MCP tool not found: %s/%s", serverName, toolName)
}

// CallTool calls a server tool.
func (m *MCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]any) (MCPCallResult, error) {
	if serverName == "" || toolName == "" {
		return MCPCallResult{}, fmt.Errorf("server and tool are required")
	}
	session, server, err := m.ensureSession(ctx, serverName)
	if err != nil {
		return MCPCallResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, toolTimeout(server))
	defer cancel()
	result, err := session.CallTool(ctx, toolName, args)
	if err != nil {
		return MCPCallResult{}, err
	}
	if result.IsError {
		return result, errors.New(callResultText(result))
	}
	return result, nil
}

// ReadResource reads a server resource.
func (m *MCPManager) ReadResource(ctx context.Context, serverName, uri string) (MCPCallResult, error) {
	if serverName == "" || uri == "" {
		return MCPCallResult{}, fmt.Errorf("server and uri are required")
	}
	session, server, err := m.ensureSession(ctx, serverName)
	if err != nil {
		return MCPCallResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, toolTimeout(server))
	defer cancel()
	return session.ReadResource(ctx, uri)
}

// GetPrompt fetches a rendered prompt.
func (m *MCPManager) GetPrompt(ctx context.Context, serverName, name string, args map[string]any) (MCPCallResult, error) {
	if serverName == "" || name == "" {
		return MCPCallResult{}, fmt.Errorf("server and name are required")
	}
	session, server, err := m.ensureSession(ctx, serverName)
	if err != nil {
		return MCPCallResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, toolTimeout(server))
	defer cancel()
	return session.GetPrompt(ctx, name, args)
}

// DirectTools builds direct AgentTool wrappers from valid cached metadata.
func (m *MCPManager) DirectTools() []tool.AgentTool {
	m.mu.Lock()
	defer m.mu.Unlock()
	used := map[string]bool{}
	var out []tool.AgentTool
	for _, server := range m.serverListLocked() {
		if !server.Enabled {
			continue
		}
		meta, ok := m.cache.Servers[server.Name]
		if !ok || meta.ConfigHash != HashMCPServerConfig(server) {
			continue
		}
		selector := m.config.Settings.DirectTools
		if server.DirectTools.Explicit || server.DirectTools.Enabled() {
			selector = server.DirectTools
		}
		if !selector.Enabled() {
			continue
		}
		for _, remoteTool := range meta.Tools {
			if !selector.Contains(remoteTool.Name) || excluded(remoteTool.Name, server.ExcludeTools) {
				continue
			}
			name := stableMCPDirectToolName(server.Name, remoteTool.Name, used)
			used[name] = true
			out = append(out, newMCPDirectToolWithPurpose(m, name, server.Name, remoteTool, mcpServerPurpose(server, meta)))
		}
	}
	return out
}

// RelevantDirectTools builds direct wrappers for cached MCP tools relevant to
// the current user request. It is intentionally independent from directTools
// configuration so long-tail tools can be offered per turn without loading all
// schemas upfront.
func (m *MCPManager) RelevantDirectTools(query string, limit int) []tool.AgentTool {
	terms := mcpQueryTerms(query)
	if len(terms) == 0 || limit == 0 {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	type candidate struct {
		server  MCPServerConfig
		meta    MCPServerMetadata
		tool    MCPToolMeta
		score   int
		sortKey string
	}
	candidates := make([]candidate, 0)
	for _, server := range m.serverListLocked() {
		if !server.Enabled {
			continue
		}
		meta, ok := m.cache.Servers[server.Name]
		if !ok || meta.ConfigHash != HashMCPServerConfig(server) {
			continue
		}
		catalog := MCPServerCatalogItem{
			Name:         server.Name,
			DisplayName:  server.Name,
			Description:  strings.TrimSpace(server.Description),
			Instructions: strings.TrimSpace(server.Instructions),
		}
		if meta.DisplayName != "" {
			catalog.DisplayName = meta.DisplayName
		}
		if catalog.Instructions == "" {
			catalog.Instructions = strings.TrimSpace(meta.Instructions)
		}
		for _, remoteTool := range meta.Tools {
			if excluded(remoteTool.Name, server.ExcludeTools) {
				continue
			}
			remoteTool.ServerName = server.Name
			score := mcpRelevanceScore(terms, mcpToolSearchText(remoteTool, catalog))
			if score == 0 {
				continue
			}
			candidates = append(candidates, candidate{
				server:  server,
				meta:    meta,
				tool:    remoteTool,
				score:   score,
				sortKey: server.Name + "/" + remoteTool.Name,
			})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].sortKey < candidates[j].sortKey
	})
	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}
	used := map[string]bool{}
	out := make([]tool.AgentTool, 0, len(candidates))
	for _, item := range candidates {
		name := stableMCPDirectToolName(item.server.Name, item.tool.Name, used)
		used[name] = true
		out = append(out, newMCPDirectToolWithPurpose(
			m, name, item.server.Name, item.tool, mcpServerPurpose(item.server, item.meta),
		))
	}
	return out
}

func (m *MCPManager) ensureMetadata(ctx context.Context, serverName string) error {
	targets := m.serverList()
	for _, server := range targets {
		if serverName != "" && server.Name != serverName {
			continue
		}
		if !server.Enabled {
			continue
		}
		m.mu.Lock()
		meta, ok := m.cache.Servers[server.Name]
		valid := ok && meta.ConfigHash == HashMCPServerConfig(server)
		m.mu.Unlock()
		if valid {
			continue
		}
		if err := m.ConnectServer(ctx, server.Name); err != nil {
			return err
		}
	}
	return nil
}

func (m *MCPManager) ensureSession(ctx context.Context, name string) (MCPClientSession, MCPServerConfig, error) {
	m.closeIdle()
	m.mu.Lock()
	server, ok := m.config.Servers[name]
	if !ok {
		m.mu.Unlock()
		return nil, MCPServerConfig{}, fmt.Errorf("MCP server not found: %s", name)
	}
	if !server.Enabled {
		m.mu.Unlock()
		return nil, MCPServerConfig{}, fmt.Errorf("MCP server disabled: %s", name)
	}
	state := m.sessions[name]
	now := m.now()
	if state != nil && !state.failureUntil.IsZero() && now.Before(state.failureUntil) {
		err := state.lastError
		m.mu.Unlock()
		return nil, server, fmt.Errorf("MCP server %s is in backoff: %s", name, err)
	}
	if state != nil && state.session != nil {
		state.lastUsed = now
		session := state.session
		m.mu.Unlock()
		return session, server, nil
	}
	m.mu.Unlock()

	session, err := m.clientFactory.Connect(ctx, server)
	m.mu.Lock()
	defer m.mu.Unlock()
	state = m.sessions[name]
	if state == nil {
		state = &mcpSessionState{}
		m.sessions[name] = state
	}
	if err != nil {
		state.failureUntil = m.now().Add(m.config.Settings.FailureBackoff)
		state.lastError = err.Error()
		return nil, server, err
	}
	state.session = session
	state.lastUsed = m.now()
	state.failureUntil = time.Time{}
	state.lastError = ""
	return session, server, nil
}

func (m *MCPManager) closeIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.config.Settings.IdleTimeout <= 0 {
		return
	}
	now := m.now()
	for name, state := range m.sessions {
		server := m.config.Servers[name]
		if server.Lifecycle == MCPLifecycleKeepAlive {
			continue
		}
		if state.session == nil || state.lastUsed.IsZero() || now.Sub(state.lastUsed) < m.config.Settings.IdleTimeout {
			continue
		}
		_ = state.session.Close()
		state.session = nil
	}
}

func (m *MCPManager) serverList() []MCPServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.serverListLocked()
}

func (m *MCPManager) serverListLocked() []MCPServerConfig {
	out := make([]MCPServerConfig, 0, len(m.config.Servers))
	for name, server := range m.config.Servers {
		server.Name = name
		out = append(out, server)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func mcpToolSearchText(tool MCPToolMeta, server MCPServerCatalogItem) string {
	return strings.ToLower(strings.Join([]string{
		tool.ServerName,
		server.DisplayName,
		server.Description,
		server.Instructions,
		tool.Name,
		tool.Description,
	}, " "))
}

func mcpRelevanceScore(terms []string, text string) int {
	score := 0
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			score++
		}
	}
	return score
}

func mcpQueryTerms(query string) []string {
	query = strings.ToLower(query)
	raw := strings.FieldsFunc(query, func(r rune) bool {
		return !(r == '_' || r == '-' || r == '.' || unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	seen := map[string]bool{}
	terms := make([]string, 0, len(raw))
	for _, term := range raw {
		term = strings.Trim(term, "_-.")
		if len(term) < 3 || mcpSearchStopWords[term] || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

var mcpSearchStopWords = map[string]bool{
	"and":    true,
	"for":    true,
	"need":   true,
	"needs":  true,
	"please": true,
	"take":   true,
	"task":   true,
	"the":    true,
	"tool":   true,
	"use":    true,
	"with":   true,
}

func collectMCPMetadata(ctx context.Context, session MCPClientSession, server MCPServerConfig) (MCPServerMetadata, error) {
	meta := MCPServerMetadata{
		ConfigHash:   HashMCPServerConfig(server),
		UpdatedAt:    time.Now(),
		DisplayName:  strings.TrimSpace(session.ServerDisplayName()),
		Instructions: strings.TrimSpace(session.ServerInstructions()),
	}
	tools, err := session.ListTools(ctx)
	if err != nil {
		return meta, err
	}
	for _, item := range tools {
		if !allowedRemoteTool(item.Name, server) {
			continue
		}
		item.ServerName = server.Name
		meta.Tools = append(meta.Tools, item)
	}
	if resources, err := session.ListResources(ctx); err == nil {
		for _, item := range resources {
			item.ServerName = server.Name
			meta.Resources = append(meta.Resources, item)
		}
	}
	if prompts, err := session.ListPrompts(ctx); err == nil {
		for _, item := range prompts {
			item.ServerName = server.Name
			meta.Prompts = append(meta.Prompts, item)
		}
	}
	return meta, nil
}

func toolTimeout(server MCPServerConfig) time.Duration {
	if server.ToolTimeout > 0 {
		return time.Duration(server.ToolTimeout) * time.Second
	}
	if server.Timeout > 0 {
		return time.Duration(server.Timeout) * time.Second
	}
	return 30 * time.Second
}

func callResultText(result MCPCallResult) string {
	if len(result.Content) == 0 {
		return "MCP tool returned an error"
	}
	var parts []string
	for _, c := range result.Content {
		switch text := c.(type) {
		case llm.TextContent:
			parts = append(parts, text.Text)
		default:
			data, _ := json.Marshal(text)
			parts = append(parts, string(data))
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func excluded(name string, exclusions []string) bool {
	for _, item := range exclusions {
		if item == name {
			return true
		}
	}
	return false
}

func allowedRemoteTool(name string, server MCPServerConfig) bool {
	if excluded(name, server.ExcludeTools) {
		return false
	}
	if len(server.EnabledTools) == 0 {
		return true
	}
	for _, item := range server.EnabledTools {
		if item == name {
			return true
		}
	}
	return false
}
