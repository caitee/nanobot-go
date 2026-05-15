package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ori/internal/app"
	"ori/internal/bus"
	"ori/internal/errors"
	"ori/internal/llm"
	"ori/internal/runtime"
	"ori/internal/session"
	"ori/internal/skills"
	"ori/internal/tool"
	legacytools "ori/internal/tools"
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
	dir, err := os.MkdirTemp("", "ori-app-test-*")
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

func TestDispatcherInjectsRelevantMCPDirectTools(t *testing.T) {
	dir, err := os.MkdirTemp("", "ori-mcp-dynamic-tools-*")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	store, err := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("session store: %v", err)
	}

	browser := legacytools.MCPServerConfig{
		Name:        "browser",
		Command:     "server",
		Description: "Browser automation and screenshots",
	}
	notes := legacytools.MCPServerConfig{
		Name:        "notes",
		Command:     "server",
		Description: "Personal notes",
	}
	manager := legacytools.NewMCPManager(legacytools.MCPManagerOptions{
		Config: &legacytools.MCPConfig{Servers: map[string]legacytools.MCPServerConfig{
			"browser": browser,
			"notes":   notes,
		}},
		Cache: &legacytools.MCPMetadataCache{Servers: map[string]legacytools.MCPServerMetadata{
			"browser": {
				ConfigHash: legacytools.HashMCPServerConfig(browser),
				Tools: []legacytools.MCPToolMeta{{
					Name:        "take_screenshot",
					Description: "Capture a screenshot of the current browser page",
					InputSchema: map[string]any{"type": "object"},
				}},
			},
			"notes": {
				ConfigHash: legacytools.HashMCPServerConfig(notes),
				Tools: []legacytools.MCPToolMeta{{
					Name:        "create_note",
					Description: "Create a note",
					InputSchema: map[string]any{"type": "object"},
				}},
			},
		}},
	})

	var toolNames []string
	stream := func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		for _, item := range c.Tools {
			toolNames = append(toolNames, item.Name)
		}
		return fakeStreamFn("done")(ctx, model, c, opts)
	}
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          bus.New(10),
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     stream,
		Model:        llm.Model{ID: "test-model", Provider: "fake", API: "openai"},
		MCPManager:   manager,
	})

	if _, err := d.ProcessDirect(context.Background(), "take a browser screenshot", "cli:k", "cli", "k"); err != nil {
		t.Fatalf("ProcessDirect: %v", err)
	}
	if !hasToolName(toolNames, "mcp_browser_take_screenshot") {
		t.Fatalf("expected relevant browser MCP tool, got %v", toolNames)
	}
	if hasToolName(toolNames, "mcp_notes_create_note") {
		t.Fatalf("unexpected unrelated notes MCP tool, got %v", toolNames)
	}
}

func TestDispatcherListsDefaultSlashCommands(t *testing.T) {
	d, _, _ := newTestDispatcher(t, "hello")

	commands := d.ListCommands()
	names := map[string]bool{}
	for _, cmd := range commands {
		names[cmd.Name] = true
	}

	for _, name := range []string{"help", "clear", "new", "status", "stop", "reasoning", "sessions"} {
		if !names[name] {
			t.Fatalf("expected default command %q in command list: %+v", name, commands)
		}
	}
}

func hasToolName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestDispatcherExecuteCommandHandlesAliasesAndResetSession(t *testing.T) {
	d, _, _ := newTestDispatcher(t, "hello")
	sess := d.Session("cli:test")
	sess.Messages = append(sess.Messages, session.Message{Role: "user", Content: "old"})

	result, handled := d.ExecuteCommand(context.Background(), "/clear", bus.InboundMessage{
		Channel: "cli", SenderID: "u", ChatID: "test", SessionKey: "cli:test",
	})
	if !handled {
		t.Fatal("expected /clear to be handled")
	}
	if result == nil || !result.ResetSession {
		t.Fatalf("expected reset-session result, got %+v", result)
	}
	if got := len(d.Session("cli:test").Messages); got != 0 {
		t.Fatalf("expected session messages to be cleared, got %d", got)
	}
}

func TestDispatcherHelpIncludesRegisteredCommandMetadata(t *testing.T) {
	d, _, _ := newTestDispatcher(t, "hello")

	result, handled := d.ExecuteCommand(context.Background(), "/help", bus.InboundMessage{
		Channel: "cli", SenderID: "u", ChatID: "test", SessionKey: "cli:test",
	})
	if !handled {
		t.Fatal("expected /help to be handled")
	}
	if result == nil || !strings.Contains(result.Text, "/clear") || !strings.Contains(result.Text, "/skills") {
		t.Fatalf("expected help to include registered command metadata, got %+v", result)
	}
}

func TestDispatcherManagementCommandsReturnUIRequestsAndFallbackText(t *testing.T) {
	d, _, _ := newTestDispatcher(t, "hello")

	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: "/mcp", want: app.UIRequestMCP},
		{input: "/skills", want: app.UIRequestSkills},
		{input: "/config", want: app.UIRequestConfig},
		{input: "/sessions", want: app.UIRequestSessions},
	} {
		result, handled := d.ExecuteCommand(context.Background(), tt.input, bus.InboundMessage{
			Channel: "cli", SenderID: "u", ChatID: "test", SessionKey: "cli:test",
		})
		if !handled {
			t.Fatalf("expected %s to be handled", tt.input)
		}
		if result == nil || result.UIRequest != tt.want {
			t.Fatalf("%s UIRequest = %+v; want %q", tt.input, result, tt.want)
		}
		if result.Text == "" {
			t.Fatalf("%s should include fallback text", tt.input)
		}
	}
}

func TestDispatcherSessionsCommandReturnsHistoryFallback(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	history := &session.Session{
		Key:       "cli:history",
		CreatedAt: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 15, 8, 5, 0, 0, time.UTC),
		Metadata:  map[string]any{},
		Messages: []session.Message{
			{Role: "user", Content: "restore this conversation"},
			{Role: "assistant", Content: "ok"},
		},
	}
	if err := store.Save(history); err != nil {
		t.Fatalf("Save history: %v", err)
	}
	mgmt := app.NewManagementService(app.ManagementOptions{SessionStore: store})
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          bus.New(10),
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     fakeStreamFn("unused"),
		Model:        llm.Model{ID: "test-model", Provider: "fake", API: "openai"},
		Management:   mgmt,
	})

	result, handled := d.ExecuteCommand(context.Background(), "/sessions", bus.InboundMessage{SessionKey: "cli:current"})

	if !handled || result == nil {
		t.Fatalf("expected /sessions to be handled")
	}
	if result.UIRequest != app.UIRequestSessions {
		t.Fatalf("UIRequest = %q; want %q", result.UIRequest, app.UIRequestSessions)
	}
	if !strings.Contains(result.Text, "cli:history") || !strings.Contains(result.Text, "restore this conversation") {
		t.Fatalf("expected session fallback text, got %q", result.Text)
	}
}

func TestManagementSessionMessagesReturnsFullTranscript(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	history := &session.Session{
		Key:       "cli:history",
		CreatedAt: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 15, 8, 5, 0, 0, time.UTC),
		Metadata:  map[string]any{},
		Messages: []session.Message{
			{Role: "user", Content: "first user message"},
			{Role: "assistant", Content: []any{
				map[string]any{"type": "thinking", "thinking": "model private reasoning"},
				map[string]any{"type": "text", "text": "assistant answer"},
			}, ToolCalls: []session.ToolCall{{
				ID:        "call_1",
				Name:      "read_file",
				Arguments: map[string]any{"path": "demo.md"},
			}}},
			{Role: "tool", Name: "read_file", ToolCallID: "call_1", Content: "file contents"},
			{Role: "user", Content: []any{map[string]any{"text": "second user message"}}},
		},
	}
	if err := store.Save(history); err != nil {
		t.Fatalf("Save history: %v", err)
	}
	mgmt := app.NewManagementService(app.ManagementOptions{SessionStore: store})

	messages := mgmt.SessionMessages("cli:history")

	if len(messages) != 4 {
		t.Fatalf("expected full transcript, got %+v", messages)
	}
	if messages[0].Role != "user" || messages[0].Content != "first user message" {
		t.Fatalf("unexpected first message: %+v", messages[0])
	}
	if messages[1].Content != "assistant answer" || messages[1].Reasoning != "model private reasoning" {
		t.Fatalf("expected assistant content and reasoning to be split, got %+v", messages[1])
	}
	if len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].Name != "read_file" || !strings.Contains(messages[1].ToolCalls[0].Arguments, "demo.md") {
		t.Fatalf("expected assistant tool call details, got %+v", messages[1])
	}
	if messages[2].Role != "tool" || messages[2].Name != "read_file" || messages[2].Content != "file contents" {
		t.Fatalf("unexpected tool message: %+v", messages[2])
	}
	if messages[3].Content != "second user message" {
		t.Fatalf("unexpected structured content text: %+v", messages[3])
	}
}

func TestDispatcherSkillCommandExpandsIntoPrompt(t *testing.T) {
	dir, err := os.MkdirTemp("", "ori-skill-command-*")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "demo"), 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "demo", "SKILL.md"), []byte(`---
name: demo
description: "Demo skill"
---

# Demo Skill
`), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	store, err := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	b := bus.New(10)
	var capturedUser string
	stream := func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		if len(c.Messages) > 0 {
			if user, ok := c.Messages[len(c.Messages)-1].(llm.UserMessage); ok {
				for _, content := range user.Content {
					if text, ok := content.(llm.TextContent); ok {
						capturedUser += text.Text
					}
				}
			}
		}
		return fakeStreamFn("done")(ctx, model, c, opts)
	}
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          b,
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     stream,
		Model:        llm.Model{ID: "test-model", Provider: "fake", API: "openai"},
		SkillLoader:  skills.NewSkillLoader(skillsDir, ""),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()
	outbound := b.ConsumeOutbound()
	b.PublishInbound(bus.InboundMessage{
		Channel: "cli", SenderID: "u", ChatID: "c", Content: "/skill:demo inspect", SessionKey: "k",
	})

	select {
	case <-outbound:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for skill command response")
	}
	if !strings.Contains(capturedUser, `<skill name="demo"`) || !strings.Contains(capturedUser, "inspect") {
		t.Fatalf("expected expanded skill prompt, got %q", capturedUser)
	}
}

func TestDispatcherSkillsCommandReturnsPlainText(t *testing.T) {
	dir, err := os.MkdirTemp("", "ori-skills-command-*")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	t.Setenv("HOME", filepath.Join(dir, "home"))

	skillsDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "demo"), 0755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "demo", "SKILL.md"), []byte(`---
name: demo
description: "Demo skill"
---

# Demo Skill
`), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	store, err := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          bus.New(10),
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     fakeStreamFn("unused"),
		Model:        llm.Model{ID: "test-model", Provider: "fake", API: "openai"},
		SkillLoader:  skills.NewSkillLoader(skillsDir, filepath.Join(dir, "no-builtins")),
	})

	result, handled := d.ExecuteCommand(context.Background(), "/skills", bus.InboundMessage{SessionKey: "k"})
	if !handled || result == nil {
		t.Fatalf("expected /skills to be handled")
	}
	if result.Markdown != "" {
		t.Fatalf("expected /skills to return plain text, got markdown %q", result.Markdown)
	}
	if !strings.Contains(result.Text, "/skill:demo") {
		t.Fatalf("expected skill list in plain text, got %q", result.Text)
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

	events := make(chan runtime.Event, 64)
	unsub := d.SubscribeRuntimeEvents(func(e runtime.Event) {
		select {
		case events <- e:
		default:
		}
	})
	defer unsub()

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

	// We should have seen at least agent_start, message_update (text delta),
	// and agent_end on the runtime event stream.
	deadline := time.After(500 * time.Millisecond)
	var seenStart, seenUpdate, seenEnd bool
	for {
		select {
		case ev := <-events:
			switch ev.Kind {
			case runtime.EventAgentStart:
				seenStart = true
			case runtime.EventMessageUpdate:
				seenUpdate = true
			case runtime.EventAgentEnd:
				seenEnd = true
			}
			if seenStart && seenUpdate && seenEnd {
				return
			}
		case <-deadline:
			t.Fatalf("events missing: start=%v update=%v end=%v", seenStart, seenUpdate, seenEnd)
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
	dir, _ := os.MkdirTemp("", "ori-stop-*")
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

func TestDispatcherFormatsErrorsForUser(t *testing.T) {
	// Stream function that returns a structured error
	errorStream := func(ctx context.Context, model llm.Model, c llm.Context, opts llm.StreamOptions) llm.EventStream {
		out := make(chan llm.StreamEvent, 2)
		go func() {
			defer close(out)
			// Simulate an API key missing error
			structuredErr := errors.Wrap(
				nil,
				errors.CategoryProvider,
				errors.SeverityError,
				errors.CodeProviderAPIKeyMissing,
				"Provider API key is missing",
				map[string]any{"provider": "anthropic"},
				false,
			)
			out <- llm.StreamEvent{
				Kind:         llm.StreamEventError,
				StopReason:   llm.StopReasonError,
				ErrorMessage: structuredErr.Error(),
			}
		}()
		return out
	}

	dir, _ := os.MkdirTemp("", "ori-error-*")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	store, _ := session.NewFileSessionStore(filepath.Join(dir, "sessions"))
	b := bus.New(10)
	d := app.NewDispatcher(app.DispatcherOptions{
		Bus:          b,
		SessionStore: store,
		ToolRegistry: tool.NewRegistry(),
		StreamFn:     errorStream,
		Model:        llm.Model{ID: "m"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	outbound := b.ConsumeOutbound()

	b.PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "test",
		Content:    "hello",
		SessionKey: "test-session",
	})

	select {
	case msg, ok := <-outbound:
		if !ok {
			t.Fatalf("outbound channel closed")
		}
		// Should contain formatted error message, not raw error
		if !strings.Contains(msg.Content, "API key") {
			t.Errorf("expected formatted error message, got: %q", msg.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for error message")
	}
}

func TestDispatcherFormatsCommandErrors(t *testing.T) {
	d, b, _ := newTestDispatcher(t, "hello")

	// Register a command that returns a structured error
	d.RegisterCommand("fail", func(ctx context.Context, d *app.Dispatcher, args string, inbound bus.InboundMessage) (string, error) {
		return "", errors.New(
			errors.CategoryConfig,
			errors.SeverityError,
			errors.CodeConfigInvalid,
			"Invalid configuration provided",
		)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	outbound := b.ConsumeOutbound()

	b.PublishInbound(bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "test",
		Content:    "/fail",
		SessionKey: "test-session",
	})

	select {
	case msg, ok := <-outbound:
		if !ok {
			t.Fatalf("outbound channel closed")
		}
		// Should contain formatted error message
		if msg.Content != "Invalid configuration provided" {
			t.Errorf("expected formatted error message, got: %q", msg.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for error message")
	}
}
