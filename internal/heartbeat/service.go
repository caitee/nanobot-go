package heartbeat

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HeartbeatTool defines the tool schema for LLM heartbeat decision
var HeartbeatTool = map[string]any{
	"type": "function",
	"function": map[string]any{
		"name":        "heartbeat",
		"description": "Report heartbeat decision after reviewing tasks.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"enum":        []any{"skip", "run"},
					"description": "skip = nothing to do, run = has active tasks",
				},
				"tasks": map[string]any{
					"type":        "string",
					"description": "Natural-language summary of active tasks (required for run)",
				},
			},
			"required": []any{"action"},
		},
	},
}

// OnExecuteFunc is the callback when heartbeat decides to run tasks
type OnExecuteFunc func(ctx context.Context, tasks string) (string, error)

// OnNotifyFunc is the callback when heartbeat delivers a response
type OnNotifyFunc func(ctx context.Context, response string) error

// Service handles periodic heartbeat checks for agent tasks
type Service struct {
	workspace    string
	interval     time.Duration
	enabled      bool
	running      bool
	stopCh       chan struct{}
	doneCh       chan struct{}
	onExecute    OnExecuteFunc
	onNotify     OnNotifyFunc
	mu           sync.RWMutex
	ticker       *time.Ticker
}

// Config holds heartbeat service configuration
type Config struct {
	IntervalSeconds int  `mapstructure:"interval_seconds"`
	Enabled         bool `mapstructure:"enabled"`
}

// DefaultConfig returns default heartbeat configuration
func DefaultConfig() *Config {
	return &Config{
		IntervalSeconds: 30 * 60, // 30 minutes
		Enabled:         true,
	}
}

// NewService creates a new heartbeat service
func NewService(workspace string, intervalSeconds int, enabled bool) *Service {
	if intervalSeconds <= 0 {
		intervalSeconds = 30 * 60
	}
	return &Service{
		workspace:    workspace,
		interval:     time.Duration(intervalSeconds) * time.Second,
		enabled:      enabled,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// SetCallbacks sets the execute and notify callbacks
func (s *Service) SetCallbacks(onExecute OnExecuteFunc, onNotify OnNotifyFunc) {
	s.onExecute = onExecute
	s.onNotify = onNotify
}

// heartbeatFile returns the path to HEARTBEAT.md
func (s *Service) heartbeatFile() string {
	return filepath.Join(s.workspace, "HEARTBEAT.md")
}

// readHeartbeatFile reads the HEARTBEAT.md content
func (s *Service) readHeartbeatFile() (string, error) {
	content, err := os.ReadFile(s.heartbeatFile())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(content), nil
}

// currentTimeStr returns current time as string for prompts
func currentTimeStr() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

// HeartbeatDecision represents the LLM's decision
type HeartbeatDecision struct {
	Action string `json:"action"`
	Tasks  string `json:"tasks"`
}

// decide asks the LLM to decide whether to skip or run tasks
func (s *Service) decide(ctx context.Context, content string, provider HeartbeatProvider) (string, string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: "You are a heartbeat agent. Call the heartbeat tool to report your decision."},
		{Role: "user", Content: "Current Time: " + currentTimeStr() + "\n\nReview the following HEARTBEAT.md and decide whether there are active tasks.\n\n" + content},
	}

	tools := []map[string]any{HeartbeatTool}

	resp, err := provider.ChatWithRetry(ctx, messages, tools)
	if err != nil {
		return "skip", "", err
	}

	// Parse tool call response
	if len(resp.ToolCalls) == 0 {
		return "skip", "", nil
	}

	// Get the first tool call arguments
	argsJSON, ok := resp.ToolCalls[0].Arguments.(map[string]any)
	if !ok {
		// Try to parse from JSON string
		if argsStr, ok := resp.ToolCalls[0].Arguments.(string); ok {
			var decision HeartbeatDecision
			if err := json.Unmarshal([]byte(argsStr), &decision); err == nil {
				return decision.Action, decision.Tasks, nil
			}
		}
		return "skip", "", nil
	}

	action, _ := argsJSON["action"].(string)
	tasks, _ := argsJSON["tasks"].(string)
	if action == "" {
		action = "skip"
	}
	return action, tasks, nil
}

// Start begins the heartbeat service
func (s *Service) Start(ctx context.Context, provider HeartbeatProvider) error {
	s.mu.Lock()
	if !s.enabled {
		slog.Info("heartbeat disabled")
		s.mu.Unlock()
		return nil
	}
	if s.running {
		slog.Warn("heartbeat already running")
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	slog.Info("heartbeat started", "interval", s.interval)

	go s.runLoop(ctx, provider)
	return nil
}

// Stop stops the heartbeat service
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
	if s.ticker != nil {
		s.ticker.Stop()
	}
}

// runLoop is the main heartbeat loop
func (s *Service) runLoop(ctx context.Context, provider HeartbeatProvider) {
	defer close(s.doneCh)

	s.ticker = time.NewTicker(s.interval)
	defer s.ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-s.ticker.C:
			s.tick(ctx, provider)
		}
	}
}

// tick executes a single heartbeat check
func (s *Service) tick(ctx context.Context, provider HeartbeatProvider) {
	content, err := s.readHeartbeatFile()
	if err != nil {
		slog.Error("heartbeat: error reading HEARTBEAT.md", "error", err)
		return
	}
	if content == "" {
		slog.Debug("heartbeat: HEARTBEAT.md missing or empty")
		return
	}

	slog.Info("heartbeat: checking for tasks...")

	action, tasks, err := s.decide(ctx, content, provider)
	if err != nil {
		slog.Error("heartbeat: decision error", "error", err)
		return
	}

	if action != "run" {
		slog.Info("heartbeat: OK (nothing to report)")
		return
	}

	slog.Info("heartbeat: tasks found, executing...")
	if s.onExecute != nil {
		response, err := s.onExecute(ctx, tasks)
		if err != nil {
			slog.Error("heartbeat: execute error", "error", err)
			return
		}

		if response != "" && s.onNotify != nil {
			shouldNotify, err := evaluateResponse(ctx, response, tasks, provider)
			if err != nil {
				slog.Error("heartbeat: evaluate error", "error", err)
				return
			}
			if shouldNotify {
				slog.Info("heartbeat: completed, delivering response")
				if err := s.onNotify(ctx, response); err != nil {
					slog.Error("heartbeat: notify error", "error", err)
				}
			} else {
				slog.Info("heartbeat: silenced by post-run evaluation")
			}
		}
	}
}

// TriggerNow manually triggers a heartbeat check
func (s *Service) TriggerNow(ctx context.Context, provider HeartbeatProvider) (string, error) {
	content, err := s.readHeartbeatFile()
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", nil
	}

	action, tasks, err := s.decide(ctx, content, provider)
	if err != nil {
		return "", err
	}
	if action != "run" || s.onExecute == nil {
		return "", nil
	}
	return s.onExecute(ctx, tasks)
}

// HeartbeatProvider is the interface for LLM providers used by heartbeat
type HeartbeatProvider interface {
	Chat(ctx context.Context, messages []ChatMessage, tools []map[string]any) (*HeartbeatResponse, error)
	ChatWithRetry(ctx context.Context, messages []ChatMessage, tools []map[string]any) (*HeartbeatResponse, error)
}

// ChatMessage represents a chat message for heartbeat
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// HeartbeatResponse represents an LLM response
type HeartbeatResponse struct {
	Content   string
	ToolCalls []HeartbeatToolCall
}

// HeartbeatToolCall represents a tool call from LLM
type HeartbeatToolCall struct {
	ID        string
	Name      string
	Arguments any // map[string]any or string (JSON)
}

// EvaluateNotificationTool is the tool schema for post-run evaluation
var EvaluateNotificationTool = map[string]any{
	"type": "function",
	"function": map[string]any{
		"name":        "evaluate_notification",
		"description": "Decide whether the user should be notified about this background task result.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"should_notify": map[string]any{
					"type":        "boolean",
					"description": "true = result contains actionable/important info the user should see; false = routine or empty, safe to suppress",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "One-sentence reason for the decision",
				},
			},
			"required": []any{"should_notify"},
		},
	},
}

const evaluateNotifySystemPrompt = `You are a notification gate for a background agent. You will be given the original task and the agent's response. Call the evaluate_notification tool to decide whether the user should be notified.

Notify when the response contains actionable information, errors, completed deliverables, or anything the user explicitly asked to be reminded about.

Suppress when the response is a routine status check with nothing new, a confirmation that everything is normal, or essentially empty.`

// evaluateResponse decides if a background-task result should be delivered to the user.
// Uses a lightweight tool-call LLM request. Falls back to true (notify) on any failure
// so that important messages are never silently dropped.
func evaluateResponse(ctx context.Context, response, tasks string, provider HeartbeatProvider) (bool, error) {
	messages := []ChatMessage{
		{Role: "system", Content: evaluateNotifySystemPrompt},
		{Role: "user", Content: "## Original task\n" + tasks + "\n\n## Agent response\n" + response},
	}

	tools := []map[string]any{EvaluateNotificationTool}

	resp, err := provider.ChatWithRetry(ctx, messages, tools)
	if err != nil {
		slog.Warn("evaluateResponse: ChatWithRetry failed, defaulting to notify", "error", err)
		return true, nil
	}

	if len(resp.ToolCalls) == 0 {
		slog.Warn("evaluateResponse: no tool call returned, defaulting to notify")
		return true, nil
	}

	// Parse should_notify from tool call arguments
	argsJSON, ok := resp.ToolCalls[0].Arguments.(map[string]any)
	if !ok {
		// Try to parse from JSON string
		if argsStr, ok := resp.ToolCalls[0].Arguments.(string); ok {
			var evalResult struct {
				ShouldNotify bool   `json:"should_notify"`
				Reason       string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(argsStr), &evalResult); err == nil {
				slog.Info("evaluateResponse result", "should_notify", evalResult.ShouldNotify, "reason", evalResult.Reason)
				return evalResult.ShouldNotify, nil
			}
		}
		slog.Warn("evaluateResponse: could not parse tool call arguments, defaulting to notify")
		return true, nil
	}

	shouldNotify, ok := argsJSON["should_notify"].(bool)
	if !ok {
		slog.Warn("evaluateResponse: should_notify not found in args, defaulting to notify")
		return true, nil
	}
	reason, _ := argsJSON["reason"].(string)
	slog.Info("evaluateResponse result", "should_notify", shouldNotify, "reason", reason)
	return shouldNotify, nil
}
