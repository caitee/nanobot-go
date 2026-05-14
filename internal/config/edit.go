package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultConfigPath returns the default user config file path.
func DefaultConfigPath(home string) string {
	return filepath.Join(home, ".ori", "config.json")
}

// DefaultMCPConfigPath returns the default user MCP config file path.
func DefaultMCPConfigPath(home string) string {
	return filepath.Join(home, ".ori", "mcp.json")
}

// PatchJSONFile merges patch into a JSON object file and writes it atomically.
// Unknown fields already present in the file are preserved.
func PatchJSONFile(path string, patch map[string]any) error {
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	} else if len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return err
		}
	}
	mergeJSONObjects(root, patch)
	return writeJSONFileAtomic(path, root)
}

// PatchMCPServerEnabled persists an MCP server enabled override.
func PatchMCPServerEnabled(path, serverName string, enabled bool) error {
	return PatchJSONFile(path, map[string]any{
		"mcpServers": map[string]any{
			serverName: map[string]any{
				"enabled": enabled,
			},
		},
	})
}

func mergeJSONObjects(dst, patch map[string]any) {
	for key, value := range patch {
		patchMap, patchIsMap := value.(map[string]any)
		dstMap, dstIsMap := dst[key].(map[string]any)
		if patchIsMap && dstIsMap {
			mergeJSONObjects(dstMap, patchMap)
			continue
		}
		dst[key] = value
	}
}

func writeJSONFileAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
