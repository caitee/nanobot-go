package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type ShellTool struct {
	enabled bool
	allow   []string
	deny    []string
}

func NewShellTool(enabled bool, allow, deny []string) *ShellTool {
	return &ShellTool{enabled: enabled, allow: allow, deny: deny}
}

func (t *ShellTool) Name() string   { return "shell" }
func (t *ShellTool) Description() string { return "Execute shell commands on the local system. Use for running scripts, system commands, or git operations. Returns stdout and stderr output." }
func (t *ShellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute (e.g., 'ls -la', 'git status', 'python script.py')",
			},
		},
		"required": []any{"command"},
		"examples": []any{
			map[string]any{"command": "ls -la"},
			map[string]any{"command": "git status"},
			map[string]any{"command": "pwd"},
			map[string]any{"command": "cat /etc/os-release"},
		},
	}
}

func (t *ShellTool) Execute(ctx context.Context, params map[string]any) (any, error) {
	if !t.enabled {
		return nil, fmt.Errorf("shell execution is disabled")
	}

	cmd, ok := params["command"].(string)
	if !ok {
		return nil, fmt.Errorf("command must be a string")
	}

	// Security check
	for _, denied := range t.deny {
		if strings.Contains(cmd, denied) {
			return nil, fmt.Errorf("command contains denied pattern: %s", denied)
		}
	}

	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	output, err := execCmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}
