package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FilesystemTool struct {
	allowedDirs []string
}

func NewFilesystemTool(allowedDirs []string) *FilesystemTool {
	return &FilesystemTool{allowedDirs: allowedDirs}
}

func (t *FilesystemTool) Name() string   { return "filesystem" }
func (t *FilesystemTool) Description() string { return "Read and write files" }
func (t *FilesystemTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string", "enum": []any{"read", "write", "list"}},
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []any{"action", "path"},
	}
}

func (t *FilesystemTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	action, _ := params["action"].(string)
	path, _ := params["path"].(string)

	if path == "" {
		return nil, fmt.Errorf("path is required")
	}

	// Security: check allowed dirs
	if len(t.allowedDirs) > 0 {
		allowed := false
		absPath, _ := filepath.Abs(path)
		for _, dir := range t.allowedDirs {
			allowedDir, _ := filepath.Abs(dir)
			if strings.HasPrefix(absPath, allowedDir) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("path not in allowed directories")
		}
	}

	switch action {
	case "read":
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	case "write":
		content, _ := params["content"].(string)
		err := os.WriteFile(path, []byte(content), 0644)
		return nil, err
	case "list":
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names, nil
	default:
		return nil, fmt.Errorf("unknown action: %s", action)
	}
}
