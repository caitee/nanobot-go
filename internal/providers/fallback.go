package providers

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// FallbackProvider wraps a provider and falls back to alternative models on error.
type FallbackProvider struct {
	primary     LLMProvider
	fallbacks   []LLMProvider
	maxRetries  int
}

// NewFallbackProvider creates a new FallbackProvider.
func NewFallbackProvider(primary LLMProvider, fallbacks []LLMProvider) *FallbackProvider {
	return &FallbackProvider{
		primary:    primary,
		fallbacks:  fallbacks,
		maxRetries: 2,
	}
}

// Chat tries the primary provider, then falls back to alternatives on error.
func (p *FallbackProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
	// Try primary first
	resp, err := p.primary.Chat(ctx, messages, tools, opts)
	if err == nil {
		return resp, nil
	}

	slog.Warn("FallbackProvider: primary failed, trying fallbacks", "error", err, "model", opts.Model)

	// Try fallbacks
	for i, fb := range p.fallbacks {
		slog.Info("FallbackProvider: trying fallback", "index", i, "model", fb.GetDefaultModel())
		resp, err := fb.Chat(ctx, messages, tools, opts)
		if err == nil {
			slog.Info("FallbackProvider: fallback succeeded", "index", i)
			return resp, nil
		}
		slog.Warn("FallbackProvider: fallback failed", "index", i, "error", err)
	}

	// All failed
	return nil, fmt.Errorf("FallbackProvider: primary and all fallbacks failed")
}

// ChatWithRetry implements retry with exponential backoff.
func (p *FallbackProvider) ChatWithRetry(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) (*LLMResponse, error) {
	return ChatWithRetryConfig(ctx, p, messages, tools, opts, RetryConfig{
		MaxAttempts: p.maxRetries + 1,
		BaseDelay:   time.Second,
		MaxDelay:    10 * time.Second,
	})
}

// RetryConfig configures retry behavior.
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// isRetryableError returns true for transient errors that should be retried.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for common retryable error patterns
	retryable := []string{"429", "500", "502", "503", "504", "timeout", "connection reset", "temporary"}
	for _, pattern := range retryable {
		if contains(errStr, pattern) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ChatWithRetryConfig retries Chat with exponential backoff using RetryConfig.
func ChatWithRetryConfig(ctx context.Context, provider LLMProvider, messages []Message, tools []ToolDef, opts ChatOptions, cfg RetryConfig) (*LLMResponse, error) {
	var lastErr error
	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := cfg.BaseDelay * time.Duration(1<<uint(attempt-1))
			if delay > cfg.MaxDelay {
				delay = cfg.MaxDelay
			}
			slog.Info("retrying after backoff", "attempt", attempt, "delay", delay)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := provider.Chat(ctx, messages, tools, opts)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryableError(err) {
			slog.Debug("non-retryable error, giving up", "error", err)
			break
		}
		slog.Warn("retryable error", "attempt", attempt, "error", err)
	}
	return nil, lastErr
}

// StreamGenerate streams from the primary provider.
func (p *FallbackProvider) StreamGenerate(ctx context.Context, messages []Message, tools []ToolDef, opts ChatOptions) <-chan StreamResponse {
	return p.primary.StreamGenerate(ctx, messages, tools, opts)
}

// GetDefaultModel returns the primary provider's default model.
func (p *FallbackProvider) GetDefaultModel() string {
	return p.primary.GetDefaultModel()
}
