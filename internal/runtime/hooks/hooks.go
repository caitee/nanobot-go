// Package hooks provides small, composable hook factories for the agent
// runtime. Each factory returns a function suitable for direct assignment to
// runtime.Options. They are intentionally thin so applications can stack them
// (see Chain*) without giving up fine-grained control.
package hooks

import (
	"context"
	"log/slog"
	"strings"

	"ori/internal/llm"
	"ori/internal/runtime"
)

// ChainBefore composes multiple BeforeToolCall hooks. Hooks run in order;
// the first one that returns a non-nil result with Block=true short-circuits.
func ChainBefore(hooks ...runtime.BeforeToolCall) runtime.BeforeToolCall {
	return func(ctx context.Context, c runtime.BeforeToolCallContext) (*runtime.BeforeToolCallResult, error) {
		for _, h := range hooks {
			if h == nil {
				continue
			}
			res, err := h(ctx, c)
			if err != nil {
				return nil, err
			}
			if res != nil && res.Block {
				return res, nil
			}
		}
		return nil, nil
	}
}

// ChainAfter composes multiple AfterToolCall hooks. Later hooks see the
// unmodified tool result (we never merge chain outputs), but each hook's
// return value overrides the next one's input where fields are non-nil.
func ChainAfter(hooks ...runtime.AfterToolCall) runtime.AfterToolCall {
	return func(ctx context.Context, c runtime.AfterToolCallContext) (*runtime.AfterToolCallResult, error) {
		var acc *runtime.AfterToolCallResult
		for _, h := range hooks {
			if h == nil {
				continue
			}
			res, err := h(ctx, c)
			if err != nil {
				return nil, err
			}
			if res == nil {
				continue
			}
			acc = mergeAfter(acc, res)
		}
		return acc, nil
	}
}

func mergeAfter(a, b *runtime.AfterToolCallResult) *runtime.AfterToolCallResult {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if b.Content != nil {
		a.Content = b.Content
	}
	if b.Details != nil {
		a.Details = b.Details
	}
	if b.IsError != nil {
		a.IsError = b.IsError
	}
	if b.Terminate != nil {
		a.Terminate = b.Terminate
	}
	return a
}

// Logging returns a BeforeToolCall hook that logs each tool invocation.
// It never blocks. Use as the first link in a ChainBefore so later hooks
// still see the raw call.
func Logging(logger *slog.Logger) runtime.BeforeToolCall {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, c runtime.BeforeToolCallContext) (*runtime.BeforeToolCallResult, error) {
		logger.Info("tool call",
			"name", c.ToolCall.Name,
			"id", c.ToolCall.ID,
			"args", c.Args,
		)
		return nil, nil
	}
}

// DenyList returns a BeforeToolCall hook that blocks any tool whose name
// appears in the deny list. Comparison is case-sensitive to match tool
// definitions exactly.
func DenyList(names ...string) runtime.BeforeToolCall {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(ctx context.Context, c runtime.BeforeToolCallContext) (*runtime.BeforeToolCallResult, error) {
		if _, ok := set[c.ToolCall.Name]; ok {
			return &runtime.BeforeToolCallResult{
				Block:  true,
				Reason: "tool " + c.ToolCall.Name + " is not allowed in this context",
			}, nil
		}
		return nil, nil
	}
}

// AllowList returns a BeforeToolCall hook that blocks every tool whose name
// is NOT in the allow list. Useful for subagents that should only see a
// restricted tool set.
func AllowList(names ...string) runtime.BeforeToolCall {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(ctx context.Context, c runtime.BeforeToolCallContext) (*runtime.BeforeToolCallResult, error) {
		if _, ok := set[c.ToolCall.Name]; !ok {
			return &runtime.BeforeToolCallResult{
				Block:  true,
				Reason: "tool " + c.ToolCall.Name + " is not in the allowed set",
			}, nil
		}
		return nil, nil
	}
}

// Redact returns an AfterToolCall hook that replaces any occurrence of the
// given substrings in the tool result's text content. The replacement string
// defaults to "[REDACTED]" when empty.
func Redact(replacement string, substrings ...string) runtime.AfterToolCall {
	if replacement == "" {
		replacement = "[REDACTED]"
	}
	return func(ctx context.Context, c runtime.AfterToolCallContext) (*runtime.AfterToolCallResult, error) {
		if c.Result == nil || len(c.Result.Content) == 0 || len(substrings) == 0 {
			return nil, nil
		}
		changed := false
		newContent := make([]llm.Content, 0, len(c.Result.Content))
		for _, block := range c.Result.Content {
			if tc, ok := block.(llm.TextContent); ok {
				t := tc.Text
				for _, s := range substrings {
					if s != "" && strings.Contains(t, s) {
						t = strings.ReplaceAll(t, s, replacement)
						changed = true
					}
				}
				tc.Text = t
				newContent = append(newContent, tc)
				continue
			}
			newContent = append(newContent, block)
		}
		if !changed {
			return nil, nil
		}
		return &runtime.AfterToolCallResult{Content: newContent}, nil
	}
}
