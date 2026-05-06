package hooks_test

import (
	"context"
	"testing"

	"nanobot-go/internal/llm"
	"nanobot-go/internal/runtime"
	"nanobot-go/internal/runtime/hooks"
	"nanobot-go/internal/tool"
)

func TestDenyListBlocksNamedTool(t *testing.T) {
	h := hooks.DenyList("shell")
	res, err := h(context.Background(), runtime.BeforeToolCallContext{
		ToolCall: llm.ToolCallContent{Name: "shell"},
	})
	if err != nil || res == nil || !res.Block {
		t.Fatalf("expected block, got %+v (err=%v)", res, err)
	}

	res, err = h(context.Background(), runtime.BeforeToolCallContext{
		ToolCall: llm.ToolCallContent{Name: "read_file"},
	})
	if err != nil || (res != nil && res.Block) {
		t.Fatalf("expected allow, got %+v (err=%v)", res, err)
	}
}

func TestAllowListPermitsOnlyNamed(t *testing.T) {
	h := hooks.AllowList("read", "write")
	if _, err := h(context.Background(), runtime.BeforeToolCallContext{
		ToolCall: llm.ToolCallContent{Name: "read"},
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	res, _ := h(context.Background(), runtime.BeforeToolCallContext{
		ToolCall: llm.ToolCallContent{Name: "shell"},
	})
	if res == nil || !res.Block {
		t.Fatalf("shell should be blocked, got %+v", res)
	}
}

func TestChainBeforeShortCircuits(t *testing.T) {
	var called int
	count := func(ctx context.Context, c runtime.BeforeToolCallContext) (*runtime.BeforeToolCallResult, error) {
		called++
		return nil, nil
	}
	blocker := func(ctx context.Context, c runtime.BeforeToolCallContext) (*runtime.BeforeToolCallResult, error) {
		return &runtime.BeforeToolCallResult{Block: true, Reason: "no"}, nil
	}
	h := hooks.ChainBefore(count, blocker, count)
	res, err := h(context.Background(), runtime.BeforeToolCallContext{})
	if err != nil || res == nil || !res.Block {
		t.Fatalf("expected block: %+v err=%v", res, err)
	}
	if called != 1 {
		t.Fatalf("count hook should have run once, got %d", called)
	}
}

func TestRedactReplacesTextContent(t *testing.T) {
	h := hooks.Redact("***", "secret")
	res, err := h(context.Background(), runtime.AfterToolCallContext{
		Result: &tool.Result{
			Content: []llm.Content{llm.TextContent{Text: "the secret is out"}},
		},
	})
	if err != nil || res == nil {
		t.Fatalf("expected override, got %+v err=%v", res, err)
	}
	newContent := res.Content[0].(llm.TextContent).Text
	if newContent != "the *** is out" {
		t.Fatalf("unexpected redaction: %q", newContent)
	}
}
