package runtime

import (
	"sync"

	"nanobot-go/internal/llm"
	"nanobot-go/internal/tool"
)

// AgentState is the public, observable state of an Agent. Assigning Tools or
// Messages through the Agent copies the slice so external mutation is safe.
type AgentState struct {
	mu sync.RWMutex

	systemPrompt  string
	model         llm.Model
	thinkingLevel string
	tools         []tool.AgentTool
	messages      []AgentMessage

	isStreaming      bool
	streamingMessage AgentMessage
	pendingToolCalls map[string]struct{}
	errorMessage     string
}

// AgentStateSnapshot is a pass-by-value view suitable for UI rendering.
type AgentStateSnapshot struct {
	SystemPrompt     string
	Model            llm.Model
	ThinkingLevel    string
	Tools            []tool.AgentTool
	Messages         []AgentMessage
	IsStreaming      bool
	StreamingMessage AgentMessage
	PendingToolCalls []string
	ErrorMessage     string
}

func newState(opts Options) *AgentState {
	return &AgentState{
		systemPrompt:     opts.SystemPrompt,
		model:            opts.Model,
		thinkingLevel:    opts.ThinkingLevel,
		tools:            cloneTools(opts.Tools),
		messages:         cloneMessages(opts.InitialHistory),
		pendingToolCalls: map[string]struct{}{},
	}
}

func (s *AgentState) Snapshot() AgentStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pending := make([]string, 0, len(s.pendingToolCalls))
	for id := range s.pendingToolCalls {
		pending = append(pending, id)
	}
	return AgentStateSnapshot{
		SystemPrompt:     s.systemPrompt,
		Model:            s.model,
		ThinkingLevel:    s.thinkingLevel,
		Tools:            cloneTools(s.tools),
		Messages:         cloneMessages(s.messages),
		IsStreaming:      s.isStreaming,
		StreamingMessage: s.streamingMessage,
		PendingToolCalls: pending,
		ErrorMessage:     s.errorMessage,
	}
}

// Messages returns a copy of the current transcript.
func (s *AgentState) Messages() []AgentMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneMessages(s.messages)
}

// Tools returns a copy of the current tool set.
func (s *AgentState) Tools() []tool.AgentTool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneTools(s.tools)
}

// SetTools replaces the tool set (copies the provided slice).
func (s *AgentState) SetTools(ts []tool.AgentTool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = cloneTools(ts)
}

// SetMessages replaces the transcript (copies the provided slice).
func (s *AgentState) SetMessages(ms []AgentMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = cloneMessages(ms)
}

// SystemPrompt returns the current system prompt.
func (s *AgentState) SystemPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.systemPrompt
}

// SetSystemPrompt updates the system prompt used for future turns.
func (s *AgentState) SetSystemPrompt(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemPrompt = p
}

// Model returns the current model.
func (s *AgentState) Model() llm.Model {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model
}

// SetModel updates the model used for future turns.
func (s *AgentState) SetModel(m llm.Model) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = m
}

// ThinkingLevel returns the current reasoning level.
func (s *AgentState) ThinkingLevel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.thinkingLevel
}

// SetThinkingLevel updates the reasoning level used for future turns.
func (s *AgentState) SetThinkingLevel(level string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thinkingLevel = level
}

// appendMessage atomically appends one message to the transcript.
func (s *AgentState) appendMessage(m AgentMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, m)
}

// setStreaming toggles the isStreaming flag.
func (s *AgentState) setStreaming(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.isStreaming = v
	if !v {
		s.streamingMessage = nil
		s.pendingToolCalls = map[string]struct{}{}
	}
}

// setStreamingMessage records the partial assistant message for UIs.
func (s *AgentState) setStreamingMessage(m AgentMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.streamingMessage = m
}

// addPending / removePending track executing tool call IDs.
func (s *AgentState) addPending(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingToolCalls[id] = struct{}{}
}

func (s *AgentState) removePending(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingToolCalls, id)
}

// setError records the most recent run's error message.
func (s *AgentState) setError(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorMessage = msg
}

func cloneTools(in []tool.AgentTool) []tool.AgentTool {
	if in == nil {
		return nil
	}
	out := make([]tool.AgentTool, len(in))
	copy(out, in)
	return out
}

func cloneMessages(in []AgentMessage) []AgentMessage {
	if in == nil {
		return nil
	}
	out := make([]AgentMessage, len(in))
	copy(out, in)
	return out
}
