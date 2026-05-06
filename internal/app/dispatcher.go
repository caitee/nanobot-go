// Package app wires the new runtime.Agent to nanobot's existing surfaces:
// channels, TUI, cron, heartbeat, subagents. The dispatcher in this file is
// the one object every legacy integration talks to.
//
// Responsibilities:
//
//   - consume bus.InboundMessage (channels, cron, TUI)
//   - route "/command" inputs to the command handler
//   - otherwise submit the message to the runtime.Agent, cancelling or
//     steering any in-flight run for the same session as appropriate
//   - fan runtime.Event out to subscribers (the CLI TUI subscribes directly;
//     channels consume the OutboundMessage published after agent_end)
//   - after agent_end, publish an OutboundMessage with the final assistant
//     text + reasoning for channels to consume
package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"nanobot-go/internal/bus"
	"nanobot-go/internal/llm"
	"nanobot-go/internal/runtime"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tool"
)

// Dispatcher is the glue between bus-driven inputs and a runtime.Agent.
// It owns per-session cancellation, command routing, and event translation.
//
// One Dispatcher per App. Agent instances are created per inbound turn so
// each session can have its own transcript snapshot; the cost of
// runtime.New is modest and this keeps concurrent sessions isolated.
type Dispatcher struct {
	bus          bus.MessageBus
	sessionStore session.SessionStore
	toolRegistry tool.Registry
	streamFn     llm.StreamFn
	model        llm.Model
	commands     map[string]CommandHandler

	enableReasoning bool
	reasoningStates sync.Map // sessionKey -> bool

	// running tracks the active agent per session so we can abort/steer.
	mu      sync.Mutex
	running map[string]*activeRun

	// runtime event fan-out: every listener sees every Agent's events,
	// filtered by SessionID on the subscriber side.
	rtMu             sync.RWMutex
	runtimeListeners map[int]func(runtime.Event)
	nextRTID         int

	// startTime is set by Run; exposed through Status.
	startTime time.Time

	// systemPrompt is computed once at construction and passed to every
	// spawned Agent. Callers can tweak it via SetSystemPrompt before Run.
	systemPrompt string

	// transformContext is chained into every Agent so memory / runtime
	// metadata gets injected without the command surface caring.
	transformContext runtime.TransformContext

	// subagent provides the spawn tool's backing implementation. May be nil.
	subagents SubagentSpawner
}

type activeRun struct {
	agent  *runtime.Agent
	cancel context.CancelFunc
}

// CommandHandler matches the legacy handler signature to keep command
// implementations portable.
type CommandHandler func(ctx context.Context, d *Dispatcher, args string, inbound bus.InboundMessage) (string, error)

// DispatcherOptions bundles what New needs.
type DispatcherOptions struct {
	Bus              bus.MessageBus
	SessionStore     session.SessionStore
	ToolRegistry     tool.Registry
	StreamFn         llm.StreamFn
	Model            llm.Model
	EnableReasoning  bool
	SystemPrompt     string
	TransformContext runtime.TransformContext
	Subagents        SubagentSpawner
}

// SubagentSpawner is the surface the spawn tool needs. Defined here so
// Dispatcher can hand it to agents without importing the concrete type.
type SubagentSpawner interface {
	Spawn(ctx context.Context, task, label, originChannel, originChatID, sessionKey string) string
	CancelBySession(sessionKey string) int
	GetRunningCount() int
}

// NewDispatcher builds a Dispatcher with the default command set.
func NewDispatcher(opts DispatcherOptions) *Dispatcher {
	d := &Dispatcher{
		bus:              opts.Bus,
		sessionStore:     opts.SessionStore,
		toolRegistry:     opts.ToolRegistry,
		streamFn:         opts.StreamFn,
		model:            opts.Model,
		enableReasoning:  opts.EnableReasoning,
		systemPrompt:     opts.SystemPrompt,
		transformContext: opts.TransformContext,
		subagents:        opts.Subagents,
		commands:         map[string]CommandHandler{},
		running:          map[string]*activeRun{},
		runtimeListeners: map[int]func(runtime.Event){},
	}
	RegisterDefaultCommands(d)
	return d
}

// RegisterCommand adds or replaces a slash command handler.
func (d *Dispatcher) RegisterCommand(name string, h CommandHandler) {
	d.commands[name] = h
}

// SetSystemPrompt updates the prompt passed to newly created agents.
func (d *Dispatcher) SetSystemPrompt(p string) { d.systemPrompt = p }

// SetTransformContext updates the transform-context hook used by new agents.
func (d *Dispatcher) SetTransformContext(fn runtime.TransformContext) { d.transformContext = fn }

// ReasoningEnabled reports the effective reasoning flag for a session.
func (d *Dispatcher) ReasoningEnabled(sessionKey string) bool {
	if v, ok := d.reasoningStates.Load(sessionKey); ok {
		return v.(bool)
	}
	return d.enableReasoning
}

// SetReasoning overrides the reasoning flag for a session.
func (d *Dispatcher) SetReasoning(sessionKey string, v bool) {
	d.reasoningStates.Store(sessionKey, v)
}

// Subagents returns the spawner (possibly nil) attached to this dispatcher.
func (d *Dispatcher) Subagents() SubagentSpawner { return d.subagents }

// SubscribeRuntimeEvents registers a listener for every runtime.Event emitted
// by any Agent the Dispatcher spawns. The returned function unsubscribes.
// Listeners are invoked synchronously during event emission, so they must not
// block for long periods — typical consumers forward the event into their own
// channel / bubbletea program and return.
func (d *Dispatcher) SubscribeRuntimeEvents(fn func(runtime.Event)) func() {
	d.rtMu.Lock()
	id := d.nextRTID
	d.nextRTID++
	d.runtimeListeners[id] = fn
	d.rtMu.Unlock()

	return func() {
		d.rtMu.Lock()
		delete(d.runtimeListeners, id)
		d.rtMu.Unlock()
	}
}

func (d *Dispatcher) fanoutRuntimeEvent(e runtime.Event) {
	d.rtMu.RLock()
	fns := make([]func(runtime.Event), 0, len(d.runtimeListeners))
	for _, fn := range d.runtimeListeners {
		fns = append(fns, fn)
	}
	d.rtMu.RUnlock()
	for _, fn := range fns {
		fn(e)
	}
}

// Model returns the default model the dispatcher will use.
func (d *Dispatcher) Model() llm.Model { return d.model }

// Bus returns the dispatcher's message bus. Used by CLI / channels that need
// to publish inbound or consume outbound without holding the whole App.
func (d *Dispatcher) Bus() bus.MessageBus { return d.bus }

// StartTime reports when Run was first invoked.
func (d *Dispatcher) StartTime() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.startTime
}

// ActiveCount returns the number of in-flight agents.
func (d *Dispatcher) ActiveCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.running)
}

// Session returns the session for a session key (creates on demand).
func (d *Dispatcher) Session(key string) *session.Session {
	return d.sessionStore.GetOrCreate(key)
}

// ResetSession clears the transcript for a session.
func (d *Dispatcher) ResetSession(key string) {
	sess := d.sessionStore.GetOrCreate(key)
	sess.Messages = nil
	sess.LastConsolidated = 0
	_ = d.sessionStore.Save(sess)
}

// AbortSession cancels the in-flight agent for a session. Returns true
// if a cancellation happened.
func (d *Dispatcher) AbortSession(key string) bool {
	d.mu.Lock()
	run, ok := d.running[key]
	d.mu.Unlock()
	if !ok {
		return false
	}
	run.cancel()
	return true
}

// Run consumes inbound messages until ctx is cancelled. It blocks.
func (d *Dispatcher) Run(ctx context.Context) error {
	d.mu.Lock()
	d.startTime = time.Now()
	d.mu.Unlock()

	inbound := d.bus.ConsumeInbound()
	slog.Info("dispatcher started, waiting for inbound messages")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-inbound:
			if !ok {
				slog.Info("dispatcher: inbound channel closed")
				return nil
			}
			d.handleInbound(ctx, msg)
		}
	}
}

// ProcessDirect runs a one-shot prompt without going through the inbound
// channel. Returns the final OutboundMessage. Used by cron, tests, and the
// -m single-shot CLI mode.
func (d *Dispatcher) ProcessDirect(ctx context.Context, content, sessionKey, channel, chatID string) (*bus.OutboundMessage, error) {
	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "user",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
	}
	return d.runTurn(ctx, msg)
}

// handleInbound dispatches commands or runs a turn and publishes the result.
func (d *Dispatcher) handleInbound(ctx context.Context, inbound bus.InboundMessage) {
	if strings.HasPrefix(inbound.Content, "/") {
		if out, handled := d.tryCommand(ctx, inbound); handled {
			if out != nil {
				d.bus.PublishOutbound(*out)
			}
			return
		}
	}

	// Cancel any prior run for this session before starting a new turn.
	d.AbortSession(inbound.SessionKey)

	go func() {
		out, err := d.runTurn(ctx, inbound)
		if err != nil {
			slog.Error("dispatcher: runTurn failed", "error", err, "session", inbound.SessionKey)
			return
		}
		if out != nil {
			d.bus.PublishOutbound(*out)
		}
	}()
}

func (d *Dispatcher) tryCommand(ctx context.Context, inbound bus.InboundMessage) (*bus.OutboundMessage, bool) {
	parts := splitCommand(inbound.Content)
	cmd := strings.TrimPrefix(parts[0], "/")
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}
	handler, ok := d.commands[cmd]
	if !ok {
		return nil, false
	}
	resp, err := handler(ctx, d, args, inbound)
	if err != nil {
		slog.Error("dispatcher: command error", "cmd", cmd, "error", err)
		return nil, true
	}
	if resp == "" {
		return nil, true
	}
	return &bus.OutboundMessage{
		Channel: inbound.Channel,
		ChatID:  inbound.ChatID,
		Content: resp,
		ReplyTo: inbound.SenderID,
	}, true
}

// runTurn creates an Agent, primes it with session history, wires the event
// translator, drives the prompt to completion, and returns the outbound
// payload for channels.
func (d *Dispatcher) runTurn(parent context.Context, inbound bus.InboundMessage) (*bus.OutboundMessage, error) {
	sess := d.sessionStore.GetOrCreate(inbound.SessionKey)

	// Append the user message to the persistent transcript. We do it here
	// (instead of inside the agent event listener) so subsequent turns in
	// the same session can read this message before the agent finishes.
	sess.Messages = append(sess.Messages, session.Message{Role: "user", Content: inbound.Content})
	_ = d.sessionStore.Save(sess)

	// Build the AgentMessage history from session history.
	history := messagesFromSession(sess)

	a, err := runtime.New(runtime.Options{
		SystemPrompt:     d.systemPrompt,
		Model:            d.model,
		ThinkingLevel:    d.thinkingLevelFor(inbound.SessionKey),
		Tools:            d.toolRegistry.All(),
		InitialHistory:   history[:len(history)-1], // exclude the just-appended user msg; it's the prompt
		StreamFn:         d.streamFn,
		TransformContext: d.transformContext,
		SessionID:        inbound.SessionKey,
	})
	if err != nil {
		return nil, fmt.Errorf("runtime.New: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)
	d.mu.Lock()
	d.running[inbound.SessionKey] = &activeRun{agent: a, cancel: cancel}
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		if r, ok := d.running[inbound.SessionKey]; ok && r.agent == a {
			delete(d.running, inbound.SessionKey)
		}
		d.mu.Unlock()
		cancel()
	}()

	// Subscribe to runtime events: a final collector grabs the assistant's
	// final text/reasoning at agent_end so we can publish one OutboundMessage
	// for channels, and a fan-out lets modern subscribers (the TUI) observe
	// the raw stream.
	final := newFinalCollector()
	unsubFinal := a.Subscribe(final.handle)
	defer unsubFinal()

	unsubFanout := a.Subscribe(func(e runtime.Event) {
		if e.SessionID == "" {
			e.SessionID = inbound.SessionKey
		}
		d.fanoutRuntimeEvent(e)
	})
	defer unsubFanout()

	// The prompt is just the user message we already appended.
	promptMsg := history[len(history)-1]
	if err := a.Prompt(ctx, promptMsg); err != nil {
		return nil, err
	}

	// Persist the assistant's final content into the session transcript.
	finalText, finalReasoning := final.Result()
	if finalText != "" {
		sess.Messages = append(sess.Messages, session.Message{Role: "assistant", Content: finalText})
		_ = d.sessionStore.Save(sess)
	}

	if finalText == "" && finalReasoning == "" {
		return nil, nil
	}
	return &bus.OutboundMessage{
		Channel:   inbound.Channel,
		ChatID:    inbound.ChatID,
		Content:   finalText,
		ReplyTo:   inbound.SenderID,
		Reasoning: finalReasoning,
		Metadata: map[string]any{
			bus.OutboundMetadataAgentEventFinal: true,
		},
	}, nil
}

// thinkingLevelFor returns the reasoning level string for a session.
func (d *Dispatcher) thinkingLevelFor(sessionKey string) string {
	if d.ReasoningEnabled(sessionKey) {
		return "medium"
	}
	return ""
}

// splitCommand splits "/name arg1 arg2" into ["/name", "arg1 arg2"].
func splitCommand(content string) []string {
	idx := strings.IndexByte(content, ' ')
	if idx == -1 {
		return []string{content}
	}
	return []string{content[:idx], strings.TrimSpace(content[idx+1:])}
}
