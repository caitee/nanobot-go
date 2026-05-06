package main

import (
	"sync"

	appcore "nanobot-go/internal/app"
	"nanobot-go/internal/bus"
	"nanobot-go/internal/runtime"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// interactiveModel is the bubbletea model backing `nanobot agent` interactive
// mode. It subscribes directly to runtime.Event via the Dispatcher, plus the
// outbound channel for assistant final text (the dispatcher publishes an
// OutboundMessage after agent_end so every channel, including the TUI, gets
// the same payload).
type interactiveModel struct {
	textInput  textinput.Model
	dispatcher *appcore.Dispatcher
	sessionKey string
	chatID     string

	waiting          bool
	quitting         bool
	done             chan struct{}
	mu               sync.Mutex
	spinnerIdx       int
	responseReceived bool
	program          *tea.Program

	runtimeEvents chan runtime.Event
	outboundCh    <-chan bus.OutboundMessage
	unsubRuntime  func()

	active          bool
	rounds          []thinkingRound // completed rounds
	currentRound    *thinkingRound  // round in progress
	streamText      string          // full text received from stream
	displayedText   string          // text currently displayed (typewriter)
	typewriterQueue []rune          // queue of runes waiting to be displayed
	status          string          // current agent status
}

// thinkingRound represents one round of thinking + tool calls.
type thinkingRound struct {
	reasoning string
	toolCalls []toolCallEntry
}

type toolCallEntry struct {
	id         string
	name       string
	args       string
	status     string // "pending" | "running" | "done" | "error"
	result     string
	durationMs int64
	expanded   bool
}

// Messages flowing through tea.Update. One notification arrives per runtime
// event or outbound message; the typewriter and spinner tickers fire on
// their own cadence.
type spinnerTickMsg struct{}
type typewriterTickMsg struct{}
type pollTickMsg struct{}

type runtimeEventMsg struct {
	ev runtime.Event
}

type responseMsg struct {
	content         string
	reasoning       string
	agentEventFinal bool
	fallback        bool
}

// newInteractiveModel constructs the TUI model and subscribes to the
// dispatcher's runtime event stream for sessionKey.
func newInteractiveModel(dispatcher *appcore.Dispatcher, messageBus bus.MessageBus, sessionKey, chatID string) *interactiveModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.Prompt = "> "

	// Buffered so the runtime emission path doesn't stall on our select loop
	// during bursty streams.
	eventCh := make(chan runtime.Event, 512)
	m := &interactiveModel{
		textInput:     ti,
		dispatcher:    dispatcher,
		sessionKey:    sessionKey,
		chatID:        chatID,
		done:          make(chan struct{}),
		runtimeEvents: eventCh,
		outboundCh:    messageBus.ConsumeOutbound(),
	}
	m.unsubRuntime = dispatcher.SubscribeRuntimeEvents(func(e runtime.Event) {
		if e.SessionID != sessionKey {
			return
		}
		select {
		case eventCh <- e:
		default:
			// Event buffer full — drop to avoid blocking the emitter.
			// The UI can always fall back to the outbound final message.
		}
	})
	return m
}

// SetProgram installs the bubbletea program reference used for printAbove.
func (m *interactiveModel) SetProgram(p *tea.Program) { m.program = p }

// Init starts all background tickers.
func (m *interactiveModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.tickSpinner(), m.pollEvents(), m.tickTypewriter())
}

// shutdown releases the runtime subscription. Called on quit.
func (m *interactiveModel) shutdown() {
	if m.unsubRuntime != nil {
		m.unsubRuntime()
		m.unsubRuntime = nil
	}
	select {
	case <-m.done:
	default:
		close(m.done)
	}
}
