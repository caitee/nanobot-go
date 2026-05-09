package tools

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"ori/internal/llm"
	"ori/internal/tool"
)

// ShellOperations abstracts how commands are executed. Replace the default
// local implementation with SSH, Docker, or any remote backend.
type ShellOperations interface {
	Exec(ctx context.Context, command string, opts ExecOptions) (ExecResult, error)
}

// ExecOptions carries execution context that the backend may use.
type ExecOptions struct {
	Cwd     string
	Env     map[string]string
	Timeout time.Duration
	OnData  func(chunk []byte) // optional streaming callback
}

// ExecResult is returned by ShellOperations.Exec after the command finishes.
type ExecResult struct {
	ExitCode int
	TimedOut bool
}

// ShellSpawnContext is the mutable context passed through hooks before execution.
type ShellSpawnContext struct {
	Command string
	Cwd     string
	Env     map[string]string
}

// ShellSpawnHook intercepts a command before execution. It can inspect,
// modify, or reject (by returning an error) the spawn context.
type ShellSpawnHook func(ctx ShellSpawnContext) (ShellSpawnContext, error)

// ShellToolOptions configures the shell tool at construction time.
type ShellToolOptions struct {
	Operations    ShellOperations
	SpawnHook     ShellSpawnHook
	CommandPrefix string
	Cwd           string
}

// --- Default local operations ---

type localShellOps struct{}

func (l *localShellOps) Exec(ctx context.Context, command string, opts ExecOptions) (ExecResult, error) {
	execCtx := ctx
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	if len(opts.Env) > 0 {
		env := cmd.Environ()
		for k, v := range opts.Env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ExecResult{}, err
	}

	if err := cmd.Start(); err != nil {
		return ExecResult{}, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	pump := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 && opts.OnData != nil {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				opts.OnData(chunk)
			}
			if err != nil {
				return
			}
		}
	}
	go pump(stdout)
	go pump(stderr)
	wg.Wait()

	waitErr := cmd.Wait()

	result := ExecResult{ExitCode: 0}
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			result.ExitCode = ee.ExitCode()
		} else {
			return result, waitErr
		}
	}
	if execCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
	}
	return result, nil
}

// LocalShellOperations returns the default local execution backend.
func LocalShellOperations() ShellOperations {
	return &localShellOps{}
}

// --- Hook constructors ---

// AllowDenyHook builds a SpawnHook from allow/deny lists. If allow is
// non-empty, only listed programs may run. Deny always takes precedence.
func AllowDenyHook(allow, deny []string) ShellSpawnHook {
	return func(ctx ShellSpawnContext) (ShellSpawnContext, error) {
		program := extractProgram(ctx.Command)

		for _, d := range deny {
			if program == d {
				return ctx, fmt.Errorf("command %q is denied", program)
			}
		}

		if len(allow) > 0 {
			allowed := false
			for _, a := range allow {
				if program == a {
					allowed = true
					break
				}
			}
			if !allowed {
				return ctx, fmt.Errorf("command %q is not in allow list", program)
			}
		}

		return ctx, nil
	}
}

// ChainHooks composes multiple hooks into one. They run in order; the first
// error short-circuits.
func ChainHooks(hooks ...ShellSpawnHook) ShellSpawnHook {
	return func(ctx ShellSpawnContext) (ShellSpawnContext, error) {
		var err error
		for _, h := range hooks {
			if h == nil {
				continue
			}
			ctx, err = h(ctx)
			if err != nil {
				return ctx, err
			}
		}
		return ctx, nil
	}
}

// extractProgram parses the first token from a shell command string.
func extractProgram(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// --- Shell AgentTool ---

// shellTool implements tool.AgentTool directly so it can stream output
// through the UpdateFn provided by the runtime.
type shellTool struct {
	ops           ShellOperations
	hook          ShellSpawnHook
	commandPrefix string
	cwd           string
}

// NewShellTool creates a shell tool with pluggable operations and hooks.
func NewShellTool(opts ShellToolOptions) tool.AgentTool {
	ops := opts.Operations
	if ops == nil {
		ops = LocalShellOperations()
	}
	return &shellTool{
		ops:           ops,
		hook:          opts.SpawnHook,
		commandPrefix: opts.CommandPrefix,
		cwd:           opts.Cwd,
	}
}

func (t *shellTool) Name() string  { return "shell" }
func (t *shellTool) Label() string { return "Shell" }
func (t *shellTool) Description() string {
	return "Execute shell commands on the local system. Returns combined stdout/stderr. Long output is truncated to the tail (most recent content); the full output is saved to a temp file referenced in the result. Supply an optional timeout in seconds."
}

func (t *shellTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds (optional)",
				"minimum":     1,
			},
		},
		"required": []any{"command"},
	}
}

func (t *shellTool) ExecutionMode() tool.ExecutionMode { return tool.ExecutionDefault }

func (t *shellTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	return raw, nil
}

func (t *shellTool) Execute(
	ctx context.Context, _ string, args map[string]any, update tool.UpdateFn,
) (*tool.Result, error) {
	cmd, ok := args["command"].(string)
	if !ok || cmd == "" {
		return nil, fmt.Errorf("command must be a non-empty string")
	}

	if t.commandPrefix != "" {
		cmd = t.commandPrefix + "\n" + cmd
	}

	spawnCtx := ShellSpawnContext{Command: cmd, Cwd: t.cwd}
	if t.hook != nil {
		var err error
		spawnCtx, err = t.hook(spawnCtx)
		if err != nil {
			return nil, err
		}
	}

	var timeout time.Duration
	if v, ok := args["timeout"].(int); ok && v > 0 {
		timeout = time.Duration(v) * time.Second
	}

	accum := NewOutputAccumulator("ori-shell")
	defer accum.Close()

	lastEmit := time.Now()
	var emitMu sync.Mutex
	emit := func(force bool) {
		if update == nil {
			return
		}
		emitMu.Lock()
		defer emitMu.Unlock()
		if !force && time.Since(lastEmit) < 100*time.Millisecond {
			return
		}
		lastEmit = time.Now()
		snap := accum.Snapshot()
		update(tool.Result{
			Content: []llm.Content{llm.TextContent{Text: snap.Content}},
			Details: snap,
		})
	}

	result, err := t.ops.Exec(ctx, spawnCtx.Command, ExecOptions{
		Cwd:     spawnCtx.Cwd,
		Env:     spawnCtx.Env,
		Timeout: timeout,
		OnData: func(chunk []byte) {
			accum.Append(chunk)
			emit(false)
		},
	})

	snap := accum.Snapshot()
	text := snap.Content + FormatTruncationNote(snap)

	if err != nil {
		return nil, fmt.Errorf("%s: %w", text, err)
	}
	if result.TimedOut {
		return nil, fmt.Errorf("%s\n\n[Command timed out after %s]", text, timeout)
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("%s\n\n[Command exited with code %d]", text, result.ExitCode)
	}

	return &tool.Result{
		Content: []llm.Content{llm.TextContent{Text: text}},
		Details: snap,
	}, nil
}
