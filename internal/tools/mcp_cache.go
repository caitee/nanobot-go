package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const mcpCacheVersion = 1

// MCPToolMeta is cached metadata for a server tool.
type MCPToolMeta struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
	ServerName  string         `json:"serverName,omitempty"`
}

// MCPResourceMeta is cached metadata for a server resource.
type MCPResourceMeta struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
	ServerName  string `json:"serverName,omitempty"`
}

// MCPPromptMeta is cached metadata for a server prompt template.
type MCPPromptMeta struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Arguments   []MCPPromptArgMeta `json:"arguments,omitempty"`
	ServerName  string             `json:"serverName,omitempty"`
}

// MCPPromptArgMeta is cached metadata for one prompt argument.
type MCPPromptArgMeta struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// MCPMetadataCache stores safe server metadata and config hashes.
type MCPMetadataCache struct {
	Version int                          `json:"version"`
	Servers map[string]MCPServerMetadata `json:"servers"`
}

// MCPServerMetadata is metadata for one configured server.
type MCPServerMetadata struct {
	ConfigHash string            `json:"configHash"`
	UpdatedAt  time.Time         `json:"updatedAt"`
	Tools      []MCPToolMeta     `json:"tools,omitempty"`
	Resources  []MCPResourceMeta `json:"resources,omitempty"`
	Prompts    []MCPPromptMeta   `json:"prompts,omitempty"`
}

// LoadMCPMetadataCache loads a metadata cache, returning an empty cache when absent.
func LoadMCPMetadataCache(path string) (*MCPMetadataCache, error) {
	cache := &MCPMetadataCache{Version: mcpCacheVersion, Servers: map[string]MCPServerMetadata{}}
	if path == "" {
		return cache, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cache, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, cache); err != nil {
		return nil, err
	}
	if cache.Servers == nil {
		cache.Servers = map[string]MCPServerMetadata{}
	}
	return cache, nil
}

// Save writes the cache to disk.
func (c *MCPMetadataCache) Save(path string) error {
	if c == nil || path == "" {
		return nil
	}
	c.Version = mcpCacheVersion
	if c.Servers == nil {
		c.Servers = map[string]MCPServerMetadata{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// HashMCPServerConfig returns a stable hash for cache invalidation.
func HashMCPServerConfig(cfg MCPServerConfig) string {
	type hashConfig struct {
		Transport    string            `json:"transport,omitempty"`
		Command      string            `json:"command,omitempty"`
		Args         []string          `json:"args,omitempty"`
		Env          map[string]string `json:"env,omitempty"`
		URL          string            `json:"url,omitempty"`
		Headers      map[string]string `json:"headers,omitempty"`
		Timeout      int               `json:"timeout,omitempty"`
		EnabledTools []string          `json:"enabledTools,omitempty"`
		ExcludeTools []string          `json:"excludeTools,omitempty"`
	}
	data, _ := json.Marshal(hashConfig{
		Transport:    cfg.Transport,
		Command:      cfg.Command,
		Args:         cfg.Args,
		Env:          cfg.Env,
		URL:          cfg.URL,
		Headers:      cfg.Headers,
		Timeout:      cfg.Timeout,
		EnabledTools: cfg.EnabledTools,
		ExcludeTools: cfg.ExcludeTools,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
