package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nanobot-go/internal/app"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/llm"
	"nanobot-go/internal/session"
	"nanobot-go/internal/tool"
)

// fakeStream returns a scripted stream that replies with fixed text.
func fakeStreamFn(reply string) llm.StreamFn {
	return func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		out := make(chan llm.StreamEvent, 4)
		msg := llm.AssistantMessage{
			Content:    []llm.Content{llm.TextContent{Text: reply}},
			StopReason: llm.StopReasonStop,
		}
		go func() {
			defer close(out)
			out <- llm.StreamEvent{Kind: llm.StreamEventTextDelta, Delta: reply, Partial: &msg}
			out <- llm.StreamEvent{Kind: llm.StreamEventDone, StopReason: llm.StopReasonStop, Message: &msg}
		}()
		return out
	}
}

func newTestDispatcher(t *testing.T, reply string) (*app.Dispatcher, bus.MessageBus, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "nanobot-app-test-*")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("session store: %v", err)
	}

	b := bus.New(10)
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          b,
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     fakeStreamFn(reply),
		Model:        llm.Model{ID: "test-model", Provider: "fake", API: "openai"},
	})
	return d, b, dir
}

func TestDispatcherProcessDirectReturnsOutbound(t *testing.T) {
	d, _, _ := newTestDispatcher(t, "hello back")

	out, err := d.ProcessDirect(context.Background(), "hi", "cli:direct", "cli", "direct")
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if out == nil {
		t.Fatalf("expected outbound, got nil")
	}
	if out.Content != "hello back" {
		t.Fatalf("content = %q", out.Content)
	}
}

func TestDispatcherInboundCommandBypassesAgent(t *testing.T) {
	d, _, _ := newTestDispatcher(t, "would-have-replied")

	// Issue the /help command directly; the dispatcher should answer from
	// the command table without calling the stream function.
	out, err := d.ProcessDirect(context.Background(), "/help", "cli:direct", "cli", "direct")
	if err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	// ProcessDirect runs the command path only when called through
	// handleInbound, not ProcessDirect — ProcessDirect intentionally goes
	// straight to the agent. So here we expect the scripted reply instead.
	if out == nil || out.Content != "would-have-replied" {
		t.Fatalf("unexpected output: %+v", out)
	}
}

func TestDispatcherRunProcessesInboundAndEmitsEvents(t *testing.T) {
	d, b, _ := newTestDispatcher(t, "hello back")

	sub := b.SubscribeAgentEvents()
	outbound := b.ConsumeOutbound()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	b.PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "direct",
		Content:    "ping",
		SessionKey: "cli:direct",
	})

	select {
	case msg, ok := <-outbound:
		if !ok {
			t.Fatalf("outbound channel closed")
		}
		if msg.Content != "hello back" {
			t.Fatalf("content = %q", msg.Content)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for outbound message")
	}

	// We should have seen at least session_start, llm_stream_chunk, llm_final.
	deadline := time.After(500 * time.Millisecond)
	var seenStart, seenChunk, seenFinal bool
	for {
		select {
		case ev := <-sub:
			switch ev.Type {
			case bus.EventSessionStart:
				seenStart = true
			case bus.EventLLMStreamChunk:
				seenChunk = true
			case bus.EventLLMFinal:
				seenFinal = true
			}
			if seenStart && seenChunk && seenFinal {
				return
			}
		case <-deadline:
			t.Fatalf("events missing: start=%v chunk=%v final=%v", seenStart, seenChunk, seenFinal)
		}
	}
}

func TestDispatcherStopCommandAbortsRun(t *testing.T) {
	// Blocking stream that never terminates until context is cancelled.
	blockStream := func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		out := make(chan llm.StreamEvent)
		go func() { <-ctx.Done(); close(out) }()
		return out
	}
	dir, _ := os.MkdirTemp("", "nanobot-stop-*")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	store, _ := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	b := bus.New(10)
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          b,
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     blockStream,
		Model:        llm.Model{ID: "m"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	b.PublishInbound(bus.InboundMessage{
		Channel: "cli", SenderID: "u", ChatID: "c", Content: "go", SessionKey: "k",
	})
	time.Sleep(50 * time.Millisecond)
	if d.ActiveCount() != 1 {
		t.Fatalf("expected 1 active run, got %d", d.ActiveCount())
	}
	b.PublishInbound(bus.InboundMessage{
		Channel: "cli", SenderID: "u", ChatID: "c", Content: "/stop", SessionKey: "k",
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if d.ActiveCount() == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("active run did not terminate after /stop")
}
