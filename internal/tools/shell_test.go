package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"nanobot-go/internal/llm"
	"nanobot-go/internal/tool"
)

func TestShellTool_BasicExecution(t *testing.T) {
	st := NewShellTool(ShellToolOptions{})
	res, err := st.Execute(context.Background(), "id1", map[string]any{
		"command": "echo hello",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("expected content, got %+v", res)
	}
	text := textOf(res)
	if !strings.Contains(text, "hello") {
		t.Fatalf("expected output to contain 'hello', got %q", text)
	}
}

func TestShellTool_StreamingUpdates(t *testing.T) {
	st := NewShellTool(ShellToolOptions{})
	var updates int32
	update := func(partial tool.Result) { atomic.AddInt32(&updates, 1) }

	// Three lines spaced 150ms apart to defeat the 100ms throttle.
	_, err := st.Execute(context.Background(), "id1", map[string]any{
		"command": "for i in 1 2 3; do echo line$i; sleep 0.15; done",
	}, update)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&updates) == 0 {
		t.Fatalf("expected at least one streaming update, got 0")
	}
}

func TestShellTool_TruncationWithTempFile(t *testing.T) {
	st := NewShellTool(ShellToolOptions{})
	// Produce well over DefaultMaxOutputLines lines.
	cmd := fmt.Sprintf("for i in $(seq 1 %d); do echo $i; done", DefaultMaxOutputLines+500)

	res, err := st.Execute(context.Background(), "id1", map[string]any{
		"command": cmd,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap, ok := res.Details.(OutputSnapshot)
	if !ok {
		t.Fatalf("expected OutputSnapshot in Details, got %T", res.Details)
	}
	if !snap.Truncation.Truncated {
		t.Fatalf("expected truncation to trigger, got %+v", snap.Truncation)
	}
	if snap.FullOutputPath == "" {
		t.Fatalf("expected full output to be persisted, got empty path")
	}
	// Full file must exist and contain the last line (which the tail view may not).
	data, err := os.ReadFile(snap.FullOutputPath)
	if err != nil {
		t.Fatalf("cannot read full output file: %v", err)
	}
	last := fmt.Sprintf("%d\n", DefaultMaxOutputLines+500)
	if !strings.HasSuffix(string(data), last) {
		t.Fatalf("full output file missing last line %q; tail=%q", last, tailOf(string(data), 30))
	}

	// Visible content should contain the truncation note pointing at the file.
	text := textOf(res)
	if !strings.Contains(text, snap.FullOutputPath) {
		t.Fatalf("truncation note should reference full output path; got %q", text)
	}
}

func TestShellTool_AllowDenyHook(t *testing.T) {
	hook := AllowDenyHook([]string{"echo"}, []string{"rm"})
	st := NewShellTool(ShellToolOptions{SpawnHook: hook})

	if _, err := st.Execute(context.Background(), "id1", map[string]any{"command": "echo ok"}, nil); err != nil {
		t.Fatalf("allowed command should succeed, got %v", err)
	}
	if _, err := st.Execute(context.Background(), "id1", map[string]any{"command": "ls"}, nil); err == nil {
		t.Fatalf("command outside allow list should fail")
	}
	if _, err := st.Execute(context.Background(), "id1", map[string]any{"command": "rm /tmp/x"}, nil); err == nil {
		t.Fatalf("denied command should fail")
	}
}

// --- helpers ---

func textOf(r *tool.Result) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tx, ok := r.Content[0].(llm.TextContent); ok {
		return tx.Text
	}
	return fmt.Sprintf("%v", r.Content[0])
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
