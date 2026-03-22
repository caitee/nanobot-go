package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nanobot-go/internal/providers"
	"nanobot-go/internal/session"
)

const (
	// MaxConsolidationRounds is the maximum number of consolidation rounds per call
	MaxConsolidationRounds = 5
	// MaxFailuresBeforeRawArchive is the number of consecutive failures before raw archiving
	MaxFailuresBeforeRawArchive = 3
	// SaveMemoryToolName is the tool name for memory saving
	SaveMemoryToolName = "save_memory"
)

// SaveMemoryToolDefinition is the tool definition for the save_memory tool
var SaveMemoryToolDefinition = providers.ToolDef{
	Name:        SaveMemoryToolName,
	Description: "Save the memory consolidation result to persistent storage.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"history_entry": map[string]any{
				"type":        "string",
				"description": "A paragraph summarizing key events/decisions/topics. Start with [YYYY-MM-DD HH:MM]. Include detail useful for grep search.",
			},
			"memory_update": map[string]any{
				"type":        "string",
				"description": "Full updated long-term memory as markdown. Include all existing facts plus new ones. Return unchanged if nothing new.",
			},
		},
		"required": []any{"history_entry", "memory_update"},
	},
}

// MemoryStore manages two-layer memory: MEMORY.md (long-term facts) + HISTORY.md (grep-searchable log)
type MemoryStore struct {
	memoryDir           string
	memoryFile          string
	historyFile         string
	consecutiveFailures int
}

// NewMemoryStore creates a new MemoryStore for the given workspace
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	historyFile := filepath.Join(memoryDir, "HISTORY.md")

	// Ensure memory directory exists
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		slog.Error("failed to create memory directory", "error", err)
	}

	return &MemoryStore{
		memoryDir:   memoryDir,
		memoryFile:  memoryFile,
		historyFile: historyFile,
	}
}

// ReadLongTerm reads the long-term memory from MEMORY.md
func (ms *MemoryStore) ReadLongTerm() string {
	data, err := os.ReadFile(ms.memoryFile)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("failed to read memory file", "error", err)
		}
		return ""
	}
	return string(data)
}

// WriteLongTerm writes the long-term memory to MEMORY.md
func (ms *MemoryStore) WriteLongTerm(content string) error {
	if err := os.WriteFile(ms.memoryFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write memory file: %w", err)
	}
	return nil
}

// AppendHistory appends a history entry to HISTORY.md
func (ms *MemoryStore) AppendHistory(entry string) error {
	f, err := os.OpenFile(ms.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open history file: %w", err)
	}
	defer f.Close()

	// Ensure entry ends with newline
	if len(entry) > 0 && entry[len(entry)-1] != '\n' {
		entry += "\n"
	}
	if _, err := f.WriteString(entry + "\n\n"); err != nil {
		return fmt.Errorf("failed to write history entry: %w", err)
	}
	return nil
}

// GetMemoryContext returns the long-term memory formatted as a context string
func (ms *MemoryStore) GetMemoryContext() string {
	longTerm := ms.ReadLongTerm()
	if longTerm == "" {
		return ""
	}
	return "## Long-term Memory\n" + longTerm
}

// formatMessages formats messages for consolidation prompt
func formatMessages(messages []session.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var lines []string
	for _, msg := range messages {
		content := extractMessageContent(msg.Content)
		if content == "" {
			continue
		}

		// Use current time as timestamp since messages don't store timestamps individually
		timestamp := time.Now().Format("2006-01-02 15:04")

		// Extract tools_used if present in content (as JSON)
		toolsUsed := extractToolsUsed(msg.Content)

		toolsStr := ""
		if len(toolsUsed) > 0 {
			toolsStr = fmt.Sprintf(" [tools: %s]", strings.Join(toolsUsed, ", "))
		}

		lines = append(lines, fmt.Sprintf("[%s] %s%s: %s", timestamp, msg.Role, toolsStr, truncateContent(content, 500)))
	}
	return strings.Join(lines, "\n")
}

// extractMessageContent extracts string content from message.Content
func extractMessageContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []byte:
		return string(c)
	default:
		// Try to serialize to JSON and extract
		data, err := json.Marshal(c)
		if err != nil {
			return fmt.Sprintf("%v", c)
		}
		return string(data)
	}
}

// extractToolsUsed tries to extract tool names from message content
func extractToolsUsed(content any) []string {
	// This is a heuristic - in practice, tools_used would come from message metadata
	// For now, we look for patterns like "Called tool X" or "tool_calls" in structured content
	switch c := content.(type) {
	case string:
		return nil
	case []any:
		// Look for tool-related entries
		var tools []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if name, ok := m["name"].(string); ok {
					tools = append(tools, name)
				}
			}
		}
		return tools
	}
	return nil
}

// truncateContent truncates content to maxLen characters
func truncateContent(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen] + "..."
}

// Consolidate consolidates the provided message chunk into MEMORY.md + HISTORY.md
func (ms *MemoryStore) Consolidate(ctx context.Context, messages []session.Message, provider providers.LLMProvider, model string) bool {
	if len(messages) == 0 {
		return true
	}

	currentMemory := ms.ReadLongTerm()
	formattedMessages := formatMessages(messages)

	prompt := fmt.Sprintf(`Process this conversation and call the save_memory tool with your consolidation.

## Current Long-term Memory
%s

## Conversation to Process
%s`, func() string {
		if currentMemory == "" {
			return "(empty)"
		}
		return currentMemory
	}(), formattedMessages)

	chatMessages := []providers.Message{
		{Role: "system", Content: "You are a memory consolidation agent. Call the save_memory tool with your consolidation of the conversation."},
		{Role: "user", Content: prompt},
	}

	tools := []providers.ToolDef{SaveMemoryToolDefinition}

	// First attempt with forced tool_choice
	resp, err := provider.ChatWithRetry(ctx, chatMessages, tools, providers.ChatOptions{
		Model: model,
	})
	if err != nil {
		slog.Warn("memory consolidation failed", "error", err)
		return ms.failOrRawArchive(messages)
	}

	// Check if forced tool_choice was unsupported (common issue with some providers)
	if resp.FinishReason == "error" && isToolChoiceUnsupported(resp.Content) {
		slog.Info("Forced tool_choice unsupported, retrying with auto")
		resp, err = provider.ChatWithRetry(ctx, chatMessages, tools, providers.ChatOptions{
			Model: model,
		})
		if err != nil {
			slog.Warn("memory consolidation retry failed", "error", err)
			return ms.failOrRawArchive(messages)
		}
	}

	// Check if we got tool calls
	if len(resp.ToolCalls) == 0 {
		slog.Warn("memory consolidation: LLM did not call save_memory",
			"finish_reason", resp.FinishReason,
			"content_len", len(resp.Content))
		return ms.failOrRawArchive(messages)
	}

	// Parse tool arguments
	args := normalizeSaveMemoryArgs(resp.ToolCalls[0].Arguments)
	if args == nil {
		slog.Warn("memory consolidation: unexpected save_memory arguments")
		return ms.failOrRawArchive(messages)
	}

	historyEntry, ok := args["history_entry"].(string)
	if !ok {
		slog.Warn("memory consolidation: history_entry missing or not a string")
		return ms.failOrRawArchive(messages)
	}

	memoryUpdate, ok := args["memory_update"].(string)
	if !ok {
		slog.Warn("memory consolidation: memory_update missing or not a string")
		return ms.failOrRawArchive(messages)
	}

	if historyEntry == "" || memoryUpdate == "" {
		slog.Warn("memory consolidation: history_entry or memory_update is empty")
		return ms.failOrRawArchive(messages)
	}

	// Ensure text values
	historyEntry = ensureText(historyEntry)
	memoryUpdate = ensureText(memoryUpdate)

	// Append history entry
	if err := ms.AppendHistory(historyEntry); err != nil {
		slog.Error("failed to append history", "error", err)
		return ms.failOrRawArchive(messages)
	}

	// Update long-term memory if changed
	if memoryUpdate != currentMemory {
		if err := ms.WriteLongTerm(memoryUpdate); err != nil {
			slog.Error("failed to write long-term memory", "error", err)
			return ms.failOrRawArchive(messages)
		}
	}

	ms.consecutiveFailures = 0
	slog.Info("memory consolidation done", "messages", len(messages))
	return true
}

func isToolChoiceUnsupported(content string) bool {
	markers := []string{"tool_choice", "toolchoice", "does not support", `should be ["none", "auto"]`}
	lower := strings.ToLower(content)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func ensureText(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	// If it's not a string, serialize to JSON
	data, _ := json.Marshal(value)
	return string(data)
}

func normalizeSaveMemoryArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}

	// Already a map with the expected fields
	if _, ok := args["history_entry"]; ok {
		return args
	}

	return nil
}

func (ms *MemoryStore) failOrRawArchive(messages []session.Message) bool {
	ms.consecutiveFailures++
	if ms.consecutiveFailures < MaxFailuresBeforeRawArchive {
		return false
	}
	ms.rawArchive(messages)
	ms.consecutiveFailures = 0
	return true
}

func (ms *MemoryStore) rawArchive(messages []session.Message) {
	ts := time.Now().Format("2006-01-02 15:04")
	formatted := formatMessages(messages)
	entry := fmt.Sprintf("[%s] [RAW] %d messages\n%s", ts, len(messages), formatted)
	if err := ms.AppendHistory(entry); err != nil {
		slog.Error("failed to raw archive messages", "error", err)
	}
	slog.Warn("memory consolidation degraded: raw-archived messages", "messages", len(messages))
}

// BuildMessagesFunc is a function type for building messages
type BuildMessagesFunc func(history []map[string]any, currentMessage string, channel, chatID *string) []providers.Message

// GetToolDefinitionsFunc is a function type for getting tool definitions
type GetToolDefinitionsFunc func() []providers.ToolDef

// MemoryConsolidator owns consolidation policy, locking, and session offset updates
type MemoryConsolidator struct {
	store               *MemoryStore
	provider            providers.LLMProvider
	model               string
	sessions            session.SessionStore
	contextWindowTokens int
	buildMessages       BuildMessagesFunc
	getToolDefinitions  GetToolDefinitionsFunc
	locks               sync.Map // map[string]*sync.Mutex
}

// NewMemoryConsolidator creates a new MemoryConsolidator
func NewMemoryConsolidator(
	workspace string,
	provider providers.LLMProvider,
	model string,
	sessions session.SessionStore,
	contextWindowTokens int,
	buildMessages BuildMessagesFunc,
	getToolDefinitions GetToolDefinitionsFunc,
) *MemoryConsolidator {
	return &MemoryConsolidator{
		store:               NewMemoryStore(workspace),
		provider:            provider,
		model:               model,
		sessions:            sessions,
		contextWindowTokens: contextWindowTokens,
		buildMessages:       buildMessages,
		getToolDefinitions:  getToolDefinitions,
	}
}

// getLock returns the consolidation lock for a session
func (mc *MemoryConsolidator) getLock(sessionKey string) *sync.Mutex {
	lockInterface, _ := mc.locks.LoadOrStore(sessionKey, &sync.Mutex{})
	return lockInterface.(*sync.Mutex)
}

// ConsolidateMessages archives a selected message chunk into persistent memory
func (mc *MemoryConsolidator) ConsolidateMessages(ctx context.Context, messages []session.Message) bool {
	return mc.store.Consolidate(ctx, messages, mc.provider, mc.model)
}

// PickConsolidationBoundary picks a user-turn boundary that removes enough old prompt tokens
// Returns (endIndex, removedTokens) or (0, 0) if no boundary found
func (mc *MemoryConsolidator) PickConsolidationBoundary(sess *session.Session, tokensToRemove int) (int, int) {
	start := sess.LastConsolidated
	if start >= len(sess.Messages) || tokensToRemove <= 0 {
		return 0, 0
	}

	removedTokens := 0
	lastBoundaryEnd := 0

	for idx := start; idx < len(sess.Messages); idx++ {
		msg := sess.Messages[idx]
		if idx > start && msg.Role == "user" {
			lastBoundaryEnd = idx
			if removedTokens >= tokensToRemove {
				return lastBoundaryEnd, removedTokens
			}
		}
		removedTokens += estimateMessageTokens(msg)
	}

	if lastBoundaryEnd > 0 {
		return lastBoundaryEnd, removedTokens
	}
	return 0, 0
}

// estimateMessageTokens estimates the token count for a single message
func estimateMessageTokens(msg session.Message) int {
	content := extractMessageContent(msg.Content)
	if content == "" {
		return 4
	}
	return estimateContentTokens(content) + 4 // +4 for message framing overhead
}

// estimateContentTokens estimates token count for text content using improved char-based estimation.
// Chinese characters require more tokens than English in cl100k_base encoding.
func estimateContentTokens(content string) int {
	if content == "" {
		return 0
	}

	// Count Chinese and non-Chinese characters separately
	// Chinese characters (Unicode range 0x4E00-0x9FFF) typically need ~2x more tokens
	chineseChars := 0
	nonChineseChars := 0

	for _, r := range content {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseChars++
		} else {
			nonChineseChars++
		}
	}

	// Rough ratios for cl100k_base:
	// - Non-Chinese (ASCII): ~4 chars per token
	// - Chinese: ~2 chars per token (conservative, Chinese chars are often 2-4 tokens each)
	chineseTokens := chineseChars / 2
	nonChineseTokens := nonChineseChars / 4

	return max(0, chineseTokens+nonChineseTokens)
}

// EstimateSessionPromptTokens estimates current prompt size for the normal session history view
// Returns (estimatedTokens, source)
func (mc *MemoryConsolidator) EstimateSessionPromptTokens(sess *session.Session) (int, string) {
	history := getHistory(sess, 0)

	// Build messages to estimate
	channel, chatID := parseSessionKey(sess.Key)
	messages := mc.buildMessages(history, "[token-probe]", channel, chatID)

	return estimatePromptTokensChain(mc.provider, mc.model, messages, mc.getToolDefinitions())
}

// estimatePromptTokensChain estimates tokens using provider counter first, then fallback
func estimatePromptTokensChain(provider providers.LLMProvider, model string, messages []providers.Message, tools []providers.ToolDef) (int, string) {
	// Try provider's counter first if available
	if pc, ok := provider.(interface {
		EstimatePromptTokens(messages []providers.Message, tools []providers.ToolDef, model string) (int, string)
	}); ok {
		tokens, source := pc.EstimatePromptTokens(messages, tools, model)
		if tokens > 0 {
			return tokens, source
		}
	}

	// Fallback to character-based approximation
	return estimatePromptTokens(messages, tools), "char approximation"
}

// estimatePromptTokens estimates tokens using improved character-based approximation.
// Uses separate counting for Chinese and non-Chinese characters for better accuracy.
func estimatePromptTokens(messages []providers.Message, tools []providers.ToolDef) int {
	var parts []string

	// Add messages
	for _, msg := range messages {
		parts = append(parts, msg.Role)
		switch c := msg.Content.(type) {
		case string:
			parts = append(parts, c)
		default:
			data, _ := json.Marshal(c)
			parts = append(parts, string(data))
		}
	}

	// Add tools if present
	if len(tools) > 0 {
		toolsData, _ := json.Marshal(tools)
		parts = append(parts, string(toolsData))
	}

	// Calculate total tokens using improved estimation
	total := 0
	for _, part := range parts {
		total += estimateContentTokens(part)
	}

	// Per-message overhead: ~4 tokens per message for framing
	perMessageOverhead := len(messages) * 4
	return max(0, total+perMessageOverhead)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ArchiveMessages archives messages with guaranteed persistence (retries until raw-dump fallback)
func (mc *MemoryConsolidator) ArchiveMessages(ctx context.Context, messages []session.Message) bool {
	if len(messages) == 0 {
		return true
	}

	for i := 0; i < MaxFailuresBeforeRawArchive; i++ {
		if mc.ConsolidateMessages(ctx, messages) {
			return true
		}
	}
	return true
}

// MaybeConsolidateByTokens loops: archive old messages until prompt fits within half the context window
func (mc *MemoryConsolidator) MaybeConsolidateByTokens(ctx context.Context, sess *session.Session) {
	if len(sess.Messages) == 0 || mc.contextWindowTokens <= 0 {
		return
	}

	lock := mc.getLock(sess.Key)
	lock.Lock()
	defer lock.Unlock()

	target := mc.contextWindowTokens / 2
	estimated, source := mc.EstimateSessionPromptTokens(sess)
	if estimated <= 0 {
		return
	}

	if estimated < mc.contextWindowTokens {
		slog.Debug("token consolidation idle",
			"session", sess.Key, "estimated", estimated, "limit", mc.contextWindowTokens, "source", source)
		return
	}

	for round := 0; round < MaxConsolidationRounds; round++ {
		if estimated <= target {
			return
		}

		tokensToRemove := estimated - target
		if tokensToRemove < 1 {
			tokensToRemove = 1
		}

		endIdx, _ := mc.PickConsolidationBoundary(sess, tokensToRemove)
		if endIdx == 0 {
			slog.Debug("token consolidation: no safe boundary for %s (round %d)",
				sess.Key, round)
			return
		}

		chunk := sess.Messages[sess.LastConsolidated:endIdx]
		if len(chunk) == 0 {
			return
		}

		slog.Info("token consolidation round",
			"round", round, "session", sess.Key,
			"estimated", estimated, "limit", mc.contextWindowTokens,
			"source", source, "chunk_msgs", len(chunk))

		if !mc.ConsolidateMessages(ctx, chunk) {
			return
		}

		sess.LastConsolidated = endIdx
		if err := mc.sessions.Save(sess); err != nil {
			slog.Error("failed to save session after consolidation", "error", err)
			return
		}

		estimated, source = mc.EstimateSessionPromptTokens(sess)
		if estimated <= 0 {
			return
		}
	}
}

// getHistory returns unconsolidated messages for LLM input
func getHistory(sess *session.Session, maxMessages int) []map[string]any {
	unconsolidated := sess.Messages
	if sess.LastConsolidated > 0 && sess.LastConsolidated < len(sess.Messages) {
		unconsolidated = sess.Messages[sess.LastConsolidated:]
	}

	// Apply max_messages limit
	if maxMessages > 0 && len(unconsolidated) > maxMessages {
		unconsolidated = unconsolidated[len(unconsolidated)-maxMessages:]
	}

	// Convert to map format
	result := make([]map[string]any, len(unconsolidated))
	for i, msg := range unconsolidated {
		result[i] = map[string]any{
			"role":    msg.Role,
			"content": msg.Content,
		}
	}

	return result
}

// parseSessionKey parses a session key into channel and chatID
func parseSessionKey(key string) (*string, *string) {
	for i := 0; i < len(key); i++ {
		if key[i] == ':' && i < len(key)-1 {
			channel := key[:i]
			chatID := key[i+1:]
			return &channel, &chatID
		}
	}
	return nil, nil
}
