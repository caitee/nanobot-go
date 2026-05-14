package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"ori/internal/llm"
	"ori/internal/tool"
)

// Agent is a stateful wrapper around the low-level loop. It owns the
// transcript, event listeners, steering / follow-up queues, and the cancel
// controls for the active run.
type Agent struct {
	mu         sync.Mutex
	state      *AgentState
	listeners  map[int]func(Event)
	nextListID int

	steering *PendingMessageQueue
	followUp *PendingMessageQueue

	streamFn         llm.StreamFn
	convertToLLM     ConvertToLLM
	transformContext TransformContext
	getAPIKey        GetAPIKey
	shouldStopAfter  ShouldStopAfter
	beforeToolCall   BeforeToolCall
	afterToolCall    AfterToolCall
	toolExecution    tool.ExecutionMode
	sessionID        string
	temperature      float64
	maxTokens        int

	activeCancel context.CancelFunc
	activeDone   chan struct{}
}

// New builds an Agent from Options. StreamFn and ConvertToLLM are required.
func New(opts Options) (*Agent, error) {
	if opts.StreamFn == nil {
		return nil, errors.New("runtime.New: StreamFn is required")
	}
	convert := opts.ConvertToLLM
	if convert == nil {
		convert = DefaultConvertToLLM
	}

	steeringMode := opts.SteeringMode
	if steeringMode == "" {
		steeringMode = QueueOneAtAtTime
	}
	followUpMode := opts.FollowUpMode
	if followUpMode == "" {
		followUpMode = QueueOneAtAtTime
	}

	a := &Agent{
		state:            newState(opts),
		listeners:        map[int]func(Event){},
		steering:         newQueue(steeringMode),
		followUp:         newQueue(followUpMode),
		streamFn:         opts.StreamFn,
		convertToLLM:     convert,
		transformContext: opts.TransformContext,
		getAPIKey:        opts.GetAPIKey,
		shouldStopAfter:  opts.ShouldStopAfter,
		beforeToolCall:   opts.BeforeToolCall,
		afterToolCall:    opts.AfterToolCall,
		toolExecution:    opts.ToolExecution,
		sessionID:        opts.SessionID,
		temperature:      opts.Temperature,
		maxTokens:        opts.MaxTokens,
	}
	return a, nil
}

// State returns the underlying AgentState. Callers can observe or mutate the
// settable fields through it; the returned pointer is shared, not a copy.
func (a *Agent) State() *AgentState { return a.state }

// Snapshot returns an immutable view of the state.
func (a *Agent) Snapshot() AgentStateSnapshot { return a.state.Snapshot() }

// Subscribe registers an event listener and returns an unsubscribe function.
// Listeners are invoked in registration order, synchronously during event
// emission. Listeners must not block for long periods.
func (a *Agent) Subscribe(fn func(Event)) func() {
	a.mu.Lock()
	id := a.nextListID
	a.nextListID++
	a.listeners[id] = fn
	a.mu.Unlock()

	return func() {
		a.mu.Lock()
		delete(a.listeners, id)
		a.mu.Unlock()
	}
}

func (a *Agent) emit(e Event) {
	a.mu.Lock()
	listeners := make([]func(Event), 0, len(a.listeners))
	for _, fn := range a.listeners {
		listeners = append(listeners, fn)
	}
	a.mu.Unlock()
	for _, fn := range listeners {
		fn(e)
	}
	a.reduce(e)
}

// reduce updates AgentState in response to lifecycle events.
func (a *Agent) reduce(e Event) {
	switch e.Kind {
	case EventMessageStart:
		// Streaming assistant messages arrive here too; we only set
		// streamingMessage for message_update events.
	case EventMessageUpdate:
		if data, ok := e.MessageUpdate(); ok {
			a.state.setStreamingMessage(data.Partial)
		}
	case EventMessageEnd:
		if data, ok := e.MessageEnd(); ok {
			a.state.appendMessage(data.Message)
			a.state.setStreamingMessage(nil)
		}
	case EventToolExecutionStart:
		if data, ok := e.ToolStart(); ok {
			a.state.addPending(data.ToolCallID)
		}
	case EventToolExecutionEnd:
		if data, ok := e.ToolEnd(); ok {
			a.state.removePending(data.ToolCallID)
		}
	case EventAgentEnd:
		a.state.setStreamingMessage(nil)
	}
}

// Steer injects a message that runs after the current turn finishes.
func (a *Agent) Steer(m AgentMessage) { a.steering.Enqueue(m) }

// FollowUp injects a message that runs only after the agent would otherwise stop.
func (a *Agent) FollowUp(m AgentMessage) { a.followUp.Enqueue(m) }

// ClearQueues drops all steering and follow-up messages.
func (a *Agent) ClearQueues() {
	a.steering.Clear()
	a.followUp.Clear()
}

// HasQueued reports whether either queue has pending messages.
func (a *Agent) HasQueued() bool {
	return a.steering.HasItems() || a.followUp.HasItems()
}

// Abort cancels the active run, if any. It returns immediately; use
// WaitForIdle to observe completion.
func (a *Agent) Abort() {
	a.mu.Lock()
	cancel := a.activeCancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WaitForIdle returns a channel that closes when the active run finishes.
// If there is no active run, the returned channel is already closed.
func (a *Agent) WaitForIdle() <-chan struct{} {
	a.mu.Lock()
	done := a.activeDone
	a.mu.Unlock()
	if done == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return done
}

// Prompt starts a new turn cycle with the provided input. If input is a
// string, it is lifted to a UserMessage; if it implements AgentMessage it is
// used directly. Returns when the agent stops.
func (a *Agent) Prompt(ctx context.Context, input any) error {
	msgs, err := a.normalizePrompt(input)
	if err != nil {
		return err
	}
	return a.run(ctx, func(ctx context.Context, cfg loopConfig) ([]AgentMessage, error) {
		return runAgentLoop(ctx, msgs, a.state.Messages(), cfg)
	})
}

// Continue resumes the loop from the current transcript. The last message
// must be a user or tool-result message.
func (a *Agent) Continue(ctx context.Context) error {
	return a.run(ctx, func(ctx context.Context, cfg loopConfig) ([]AgentMessage, error) {
		return runAgentLoopContinue(ctx, a.state.Messages(), cfg)
	})
}

// Reset clears transcript, runtime state, and queued messages.
func (a *Agent) Reset() {
	a.state.SetMessages(nil)
	a.state.setStreaming(false)
	a.state.setError("")
	a.ClearQueues()
}

// run wraps a loop entry point with activeRun lifecycle bookkeeping.
func (a *Agent) run(
	parent context.Context,
	exec func(ctx context.Context, cfg loopConfig) ([]AgentMessage, error),
) error {
	a.mu.Lock()
	if a.activeCancel != nil {
		a.mu.Unlock()
		return errors.New("agent is already processing a prompt; use Steer / FollowUp or wait for completion")
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	a.activeCancel = cancel
	a.activeDone = done
	a.mu.Unlock()

	a.state.setStreaming(true)
	a.state.setError("")

	tools := a.state.Tools()
	toolIndex := make(map[string]tool.AgentTool, len(tools))
	for _, t := range tools {
		toolIndex[t.Name()] = t
	}
	cfg := loopConfig{
		model:            a.state.Model(),
		thinkingLevel:    a.state.ThinkingLevel(),
		temperature:      a.temperature,
		maxTokens:        a.maxTokens,
		sessionID:        a.sessionID,
		tools:            tools,
		toolIndex:        toolIndex,
		toolExecution:    a.toolExecution,
		streamFn:         a.streamFn,
		convertToLLM:     a.convertToLLM,
		transformContext: a.transformContext,
		getAPIKey:        a.getAPIKey,
		shouldStopAfter:  a.shouldStopAfter,
		beforeToolCall:   a.beforeToolCall,
		afterToolCall:    a.afterToolCall,
		sink:             a.emit,
	}

	// Steering / follow-up pollers close over queues. The steering queue is
	// polled between turns; follow-up is polled after the stop-reason allows.
	cfg.getSteeringMessages = func() []AgentMessage { return a.steering.Drain() }
	cfg.getFollowUpMessages = func() []AgentMessage { return a.followUp.Drain() }

	err := func() error {
		_, runErr := exec(ctx, cfg)
		if runErr != nil {
			// Synthesize a terminal assistant message on hard failure.
			stop := llm.StopReasonError
			if ctx.Err() != nil {
				stop = llm.StopReasonAborted
			}
			msg := llm.AssistantMessage{
				Content:      nil,
				API:          cfg.model.API,
				Provider:     cfg.model.Provider,
				Model:        cfg.model.ID,
				StopReason:   stop,
				ErrorMessage: runErr.Error(),
				Timestamp:    time.Now(),
			}
			wrapped := WrapLLM(msg)
			a.state.appendMessage(wrapped)
			a.state.setError(runErr.Error())
		}
		return runErr
	}()

	a.mu.Lock()
	a.activeCancel = nil
	a.activeDone = nil
	a.mu.Unlock()
	a.state.setStreaming(false)
	close(done)

	return err
}

func (a *Agent) normalizePrompt(input any) ([]AgentMessage, error) {
	switch v := input.(type) {
	case nil:
		return nil, errors.New("Prompt: nil input")
	case string:
		msg := llm.UserMessage{
			Content:   []llm.Content{llm.TextContent{Text: v}},
			Timestamp: time.Now(),
		}
		return []AgentMessage{WrapLLM(msg)}, nil
	case llm.Message:
		return []AgentMessage{WrapLLM(v)}, nil
	case AgentMessage:
		return []AgentMessage{v}, nil
	case []AgentMessage:
		return v, nil
	}
	return nil, errors.New("Prompt: unsupported input type")
}

// DefaultConvertToLLM is the fallback ConvertToLLM: it unwraps any llm.Message
// and drops everything else.
func DefaultConvertToLLM(msgs []AgentMessage) ([]llm.Message, error) {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		if underlying, ok := Unwrap(m); ok {
			out = append(out, underlying)
		}
	}
	return out, nil
}
