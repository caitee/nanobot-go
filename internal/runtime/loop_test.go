package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ori/internal/llm"
	"ori/internal/tool"
)

// ---------- fakes ----------

// fakeStream scripts a sequence of stream events; each Prompt/Continue drains
// one script in order. If the scripts slice runs out, a terminal error event
// is produced so tests fail loudly rather than hanging.
type fakeStream struct {
	mu      sync.Mutex
	scripts [][]llm.StreamEvent
	calls   int32
}

func (f *fakeStream) add(script ...llm.StreamEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, script)
}

func (f *fakeStream) streamFn() llm.StreamFn {
	return func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		atomic.AddInt32(&f.calls, 1)
		out := make(chan llm.StreamEvent, 16)
		f.mu.Lock()
		var events []llm.StreamEvent
		if len(f.scripts) > 0 {
			events = f.scripts[0]
			f.scripts = f.scripts[1:]
		}
		f.mu.Unlock()

		go func() {
			defer close(out)
			if events == nil {
				out <- llm.StreamEvent{
					Kind:         llm.StreamEventError,
					StopReason:   llm.StopReasonError,
					ErrorMessage: "fakeStream: no scripted events",
				}
				return
			}
			for _, ev := range events {
				select {
				case <-ctx.Done():
					return
				case out <- ev:
				}
			}
		}()
		return out
	}
}

// fakeTool implements tool.AgentTool. It records every call and produces the
// preset result. If fail != nil the tool returns that error.
type fakeTool struct {
	name       string
	params     map[string]any
	result     *tool.Result
	fail       error
	execMode   tool.ExecutionMode
	onExecute  func(args map[string]any, update tool.UpdateFn)
	calls      int32
	capturedMu sync.Mutex
	captured   []map[string]any
}

func (f *fakeTool) Name() string                      { return f.name }
func (f *fakeTool) Label() string                     { return f.name }
func (f *fakeTool) Description() string               { return "fake tool " + f.name }
func (f *fakeTool) Parameters() map[string]any        { return f.params }
func (f *fakeTool) ExecutionMode() tool.ExecutionMode { return f.execMode }
func (f *fakeTool) PrepareArguments(raw map[string]any) (map[string]any, error) {
	return raw, nil
}

func (f *fakeTool) Execute(
	ctx context.Context, callID string, args map[string]any, update tool.UpdateFn,
) (*tool.Result, error) {
	atomic.AddInt32(&f.calls, 1)
	f.capturedMu.Lock()
	f.captured = append(f.captured, args)
	f.capturedMu.Unlock()
	if f.onExecute != nil {
		f.onExecute(args, update)
	}
	if f.fail != nil {
		return nil, f.fail
	}
	return f.result, nil
}

// helpers ---------------------------------------------------------------

func collectorSink() (func(Event), func() []Event) {
	var mu sync.Mutex
	var events []Event
	return func(e Event) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		}, func() []Event {
			mu.Lock()
			defer mu.Unlock()
			out := make([]Event, len(events))
			copy(out, events)
			return out
		}
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func lastEventOfKind(evs []Event, k EventKind) (Event, bool) {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == k {
			return evs[i], true
		}
	}
	return Event{}, false
}

func countKind(evs []Event, k EventKind) int {
	n := 0
	for _, e := range evs {
		if e.Kind == k {
			n++
		}
	}
	return n
}

func assistantDone(text string) []llm.StreamEvent {
	msg := llm.AssistantMessage{
		Content:    []llm.Content{llm.TextContent{Text: text}},
		StopReason: llm.StopReasonStop,
	}
	return []llm.StreamEvent{
		{Kind: llm.StreamEventStart, Partial: &msg},
		{Kind: llm.StreamEventTextDelta, Delta: text, Partial: &msg},
		{Kind: llm.StreamEventDone, StopReason: llm.StopReasonStop, Message: &msg},
	}
}

func assistantToolUse(calls ...llm.ToolCallContent) []llm.StreamEvent {
	content := make([]llm.Content, 0, len(calls))
	for _, c := range calls {
		content = append(content, c)
	}
	msg := llm.AssistantMessage{
		Content:    content,
		StopReason: llm.StopReasonToolUse,
	}
	events := []llm.StreamEvent{
		{Kind: llm.StreamEventStart, Partial: &msg},
	}
	for _, c := range calls {
		cc := c
		events = append(events, llm.StreamEvent{
			Kind:     llm.StreamEventToolCallEnd,
			ToolCall: &cc,
			Partial:  &msg,
		})
	}
	events = append(events, llm.StreamEvent{
		Kind:       llm.StreamEventDone,
		StopReason: llm.StopReasonToolUse,
		Message:    &msg,
	})
	return events
}

func newAgent(t *testing.T, opts Options) *Agent {
	t.Helper()
	a, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

// ---------- tests ----------

func TestAgentPromptPlainText(t *testing.T) {
	fs := &fakeStream{}
	fs.add(assistantDone("hello there")...)

	sink, events := collectorSink()

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
	})
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "ping"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	got := events()
	if countKind(got, EventAgentStart) != 1 {
		t.Fatalf("agent_start count = %d", countKind(got, EventAgentStart))
	}
	if countKind(got, EventAgentEnd) != 1 {
		t.Fatalf("agent_end count = %d; events=%v", countKind(got, EventAgentEnd), kinds(got))
	}
	if countKind(got, EventTurnEnd) != 1 {
		t.Fatalf("turn_end count = %d", countKind(got, EventTurnEnd))
	}

	msgs := a.State().Messages()
	if len(msgs) != 2 {
		t.Fatalf("transcript len = %d; want 2 (user + assistant)", len(msgs))
	}
	if msgs[0].AgentRole() != string(llm.RoleUser) {
		t.Fatalf("msg[0] role = %s", msgs[0].AgentRole())
	}
	if msgs[1].AgentRole() != string(llm.RoleAssistant) {
		t.Fatalf("msg[1] role = %s", msgs[1].AgentRole())
	}
}

func TestAgentSingleToolCall(t *testing.T) {
	ft := &fakeTool{
		name:   "echo",
		params: map[string]any{"type": "object"},
		result: &tool.Result{Content: []llm.Content{llm.TextContent{Text: "echoed"}}},
	}

	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{
		ID: "tc-1", Name: "echo", Arguments: map[string]any{"x": 1},
	})...)
	fs.add(assistantDone("done")...)

	sink, events := collectorSink()

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
	})
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	if atomic.LoadInt32(&ft.calls) != 1 {
		t.Fatalf("tool call count = %d", ft.calls)
	}

	got := events()
	if countKind(got, EventToolExecutionStart) != 1 || countKind(got, EventToolExecutionEnd) != 1 {
		t.Fatalf("tool_execution_start/end mismatch: %v", kinds(got))
	}
	if countKind(got, EventTurnEnd) != 2 {
		t.Fatalf("turn_end count = %d; want 2", countKind(got, EventTurnEnd))
	}
}

func TestAgentParallelTools(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32

	mkTool := func(name string) *fakeTool {
		return &fakeTool{
			name:     name,
			execMode: tool.ExecutionParallel,
			result:   &tool.Result{Content: []llm.Content{llm.TextContent{Text: name + ":ok"}}},
			onExecute: func(args map[string]any, update tool.UpdateFn) {
				c := atomic.AddInt32(&concurrent, 1)
				for {
					m := atomic.LoadInt32(&maxConcurrent)
					if c <= m || atomic.CompareAndSwapInt32(&maxConcurrent, m, c) {
						break
					}
				}
				time.Sleep(30 * time.Millisecond)
				atomic.AddInt32(&concurrent, -1)
			},
		}
	}
	t1 := mkTool("a")
	t2 := mkTool("b")
	t3 := mkTool("c")

	fs := &fakeStream{}
	fs.add(assistantToolUse(
		llm.ToolCallContent{ID: "1", Name: "a", Arguments: map[string]any{}},
		llm.ToolCallContent{ID: "2", Name: "b", Arguments: map[string]any{}},
		llm.ToolCallContent{ID: "3", Name: "c", Arguments: map[string]any{}},
	)...)
	fs.add(assistantDone("done")...)

	a := newAgent(t, Options{
		StreamFn:      fs.streamFn(),
		Tools:         []tool.AgentTool{t1, t2, t3},
		ToolExecution: tool.ExecutionParallel,
	})

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	if got := atomic.LoadInt32(&maxConcurrent); got < 2 {
		t.Fatalf("expected concurrent execution, maxConcurrent=%d", got)
	}
}

func TestAgentBeforeToolCallBlock(t *testing.T) {
	ft := &fakeTool{name: "danger", result: &tool.Result{}}

	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{
		ID: "tc", Name: "danger", Arguments: map[string]any{},
	})...)
	fs.add(assistantDone("done")...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
		BeforeToolCall: func(ctx context.Context, c BeforeToolCallContext) (*BeforeToolCallResult, error) {
			return &BeforeToolCallResult{Block: true, Reason: "nope"}, nil
		},
	})
	sink, events := collectorSink()
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if atomic.LoadInt32(&ft.calls) != 0 {
		t.Fatalf("tool must not have executed: calls=%d", ft.calls)
	}

	end, ok := lastEventOfKind(events(), EventToolExecutionEnd)
	if !ok {
		t.Fatalf("missing tool_execution_end")
	}
	td, ok := end.ToolEnd()
	if !ok || !td.IsError {
		t.Fatalf("expected error tool result: %+v", td)
	}
}

func TestAgentAfterToolCallOverride(t *testing.T) {
	ft := &fakeTool{
		name:   "t",
		result: &tool.Result{Content: []llm.Content{llm.TextContent{Text: "original"}}},
	}

	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{ID: "1", Name: "t", Arguments: map[string]any{}})...)
	fs.add(assistantDone("done")...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
		AfterToolCall: func(ctx context.Context, c AfterToolCallContext) (*AfterToolCallResult, error) {
			return &AfterToolCallResult{
				Content: []llm.Content{llm.TextContent{Text: "overridden"}},
			}, nil
		},
	})
	sink, events := collectorSink()
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	end, _ := lastEventOfKind(events(), EventToolExecutionEnd)
	td, _ := end.ToolEnd()
	content, ok := td.Result.([]llm.Content)
	if !ok || len(content) == 0 {
		t.Fatalf("unexpected result payload: %T", td.Result)
	}
	if text := content[0].(llm.TextContent).Text; text != "overridden" {
		t.Fatalf("afterToolCall override not applied: got %q", text)
	}
}

func TestAgentShouldStopAfterTurn(t *testing.T) {
	ft := &fakeTool{name: "t", result: &tool.Result{Content: []llm.Content{llm.TextContent{Text: "ok"}}}}

	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{ID: "1", Name: "t", Arguments: map[string]any{}})...)
	// A second turn would be started if shouldStop returned false. We don't
	// script a second response so the test will fail-fast if that happens.

	stopAfterFirst := int32(0)
	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
		ShouldStopAfter: func(ctx context.Context, c ShouldStopContext) (bool, error) {
			atomic.StoreInt32(&stopAfterFirst, 1)
			return true, nil
		},
	})
	sink, events := collectorSink()
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if atomic.LoadInt32(&stopAfterFirst) != 1 {
		t.Fatalf("shouldStopAfter not called")
	}
	if got := countKind(events(), EventTurnEnd); got != 1 {
		t.Fatalf("expected single turn, got %d turn_end events", got)
	}
}

func TestAgentSteeringInjection(t *testing.T) {
	ft := &fakeTool{name: "t", result: &tool.Result{Content: []llm.Content{llm.TextContent{Text: "ok"}}}}

	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{ID: "1", Name: "t", Arguments: map[string]any{}})...)
	fs.add(assistantDone("acknowledged")...)

	a := newAgent(t, Options{
		StreamFn: fs.streamFn(),
		Tools:    []tool.AgentTool{ft},
	})

	// Enqueue a steering message before starting; it'll be drained after the
	// first turn's tool batch and fed into the second turn.
	steering := WrapLLM(llm.UserMessage{
		Content:   []llm.Content{llm.TextContent{Text: "steer me"}},
		Timestamp: time.Now(),
	})
	a.Steer(steering)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	// After two turns there should be: user + assistant + tool + user(steering) + assistant
	msgs := a.State().Messages()
	if len(msgs) != 5 {
		t.Fatalf("transcript len = %d; want 5: %+v", len(msgs), msgs)
	}
	if msgs[3].AgentRole() != string(llm.RoleUser) {
		t.Fatalf("steering message missing: %s", msgs[3].AgentRole())
	}
}

func TestAgentAbort(t *testing.T) {
	// fakeStream blocks forever until context is cancelled.
	blockStream := func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		out := make(chan llm.StreamEvent)
		go func() {
			<-ctx.Done()
			close(out)
		}()
		return out
	}
	a := newAgent(t, Options{StreamFn: blockStream})

	done := make(chan error, 1)
	go func() { done <- a.Prompt(context.Background(), "go") }()

	time.Sleep(20 * time.Millisecond)
	a.Abort()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Abort did not terminate run")
	}

	if msg := a.Snapshot().ErrorMessage; msg == "" {
		t.Fatalf("expected error message after abort")
	}
}

func TestAgentToolNotFound(t *testing.T) {
	fs := &fakeStream{}
	fs.add(assistantToolUse(llm.ToolCallContent{ID: "1", Name: "missing"})...)
	fs.add(assistantDone("done")...)

	a := newAgent(t, Options{StreamFn: fs.streamFn()})
	sink, events := collectorSink()
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "go"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	end, _ := lastEventOfKind(events(), EventToolExecutionEnd)
	td, _ := end.ToolEnd()
	if !td.IsError {
		t.Fatalf("expected error for missing tool")
	}
}

// ensureToolsVisibleToStreamFn verifies that the tool definitions actually
// reach the StreamFn via llm.Context.Tools.
func TestAgentStreamFnReceivesTools(t *testing.T) {
	received := make(chan []llm.Tool, 1)

	streamFn := func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		received <- c.Tools
		msg := llm.AssistantMessage{
			Content:    []llm.Content{llm.TextContent{Text: "ok"}},
			StopReason: llm.StopReasonStop,
		}
		out := make(chan llm.StreamEvent, 2)
		out <- llm.StreamEvent{Kind: llm.StreamEventDone, StopReason: llm.StopReasonStop, Message: &msg}
		close(out)
		return out
	}

	ft := &fakeTool{name: "t", params: map[string]any{"type": "object"}}
	a := newAgent(t, Options{StreamFn: streamFn, Tools: []tool.AgentTool{ft}})

	if err := a.Prompt(context.Background(), "ping"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	select {
	case tools := <-received:
		if len(tools) != 1 || tools[0].Name != "t" {
			t.Fatalf("tool defs missing in stream context: %+v", tools)
		}
	case <-time.After(time.Second):
		t.Fatalf("stream function not called")
	}
}

// Sanity check that AgentEnd payload carries new-messages only (not the full
// transcript) — important for channels/memory listeners that persist them.
func TestAgentEndCarriesNewMessages(t *testing.T) {
	fs := &fakeStream{}
	fs.add(assistantDone("hi")...)

	a := newAgent(t, Options{StreamFn: fs.streamFn()})
	sink, events := collectorSink()
	a.Subscribe(sink)

	if err := a.Prompt(context.Background(), "ping"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	end, _ := lastEventOfKind(events(), EventAgentEnd)
	data, ok := end.AgentEnd()
	if !ok {
		t.Fatalf("agent_end data type mismatch")
	}
	// new messages: user + assistant = 2
	if got := len(data.Messages); got != 2 {
		t.Fatalf("AgentEnd.Messages len = %d; want 2", got)
	}
}

// dummy writer to silence goimports warnings if fmt is unused in some paths.
var _ = fmt.Sprintf
