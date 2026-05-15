package main

import (
	"sync"
	"time"

	appcore "ori/internal/app"
	"ori/internal/bus"
	"ori/internal/runtime"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// interactiveModel is the bubbletea model backing `ori agent` interactive
// mode. It subscribes directly to runtime.Event via the Dispatcher, plus the
// outbound channel for assistant final text (the dispatcher publishes an
// OutboundMessage after agent_end so every channel, including the TUI, gets
// the same payload).
type interactiveModel struct {
	textInput  textinput.Model
	dispatcher *appcore.Dispatcher
	sessionKey string
	chatID     string
	banner     string

	waiting          bool
	quitting         bool
	done             chan struct{}
	mu               sync.Mutex
	spinnerIdx       int
	responseReceived bool
	program          *tea.Program
	printAboveFn     func(string)

	runtimeEvents chan runtime.Event
	outboundCh    <-chan bus.OutboundMessage
	unsubRuntime  func()

	active          bool
	currentRound    *thinkingRound // round in progress; completed rounds are flushed to View above on TurnStart
	streamText      string         // full text received from stream
	displayedText   string         // text currently displayed (typewriter)
	typewriterQueue []rune         // queue of runes waiting to be displayed
	flushedText     string         // cumulative stream text already flushed to View above (for dedup on finalize)
	status          string         // current agent status

	transcript       transcript
	nextTranscriptID int

	viewport               viewport.Model
	renderer               transcriptRenderer
	focus                  focusArea
	hasNewTranscriptOutput bool
	transcriptViewportText string

	// Live-render cache for displayedText. renderLiveContent runs glamour,
	// which is linear in the input size and gets called on every frame (every
	// spinner tick, every typewriter tick). We memoise by exact string match:
	// if displayedText hasn't changed since the last render, return the same
	// output. Terminal-width changes invalidate the cache.
	lastRenderedText   string
	lastRenderedOutput string
	lastRenderedWidth  int

	// View-level cache. viewVersion is bumped by any Update branch that
	// changes visible state; View() also keys on cheap direct render inputs
	// such as spinner frame, terminal width, live text, and input output.
	// Returning the cached string lets bubbletea's own diff detect no-ops.
	viewVersion      uint64
	cachedViewKey    viewCacheKey
	cachedViewOutput string
	cachedTextInput  string

	// Slash-command completion state. The query is tracked so selection resets
	// as soon as the input changes.
	slashCompletionQuery       string
	slashCompletionSelected    int
	slashCompletionWindowStart int

	panel *managementPanel
}

// viewCacheKey is the tuple we key the View cache on. Equality across calls
// means nothing affecting the output has changed.
type viewCacheKey struct {
	version            uint64
	spinnerIdx         int
	width              int
	textInput          string
	active             bool
	waiting            bool
	quitting           bool
	status             string
	displayedText      string
	typewriterQueueLen int
	viewportContent    string
	viewportWidth      int
	viewportHeight     int
	viewportYOffset    int
	focus              focusArea
	hasNewOutput       bool
}

// thinkingRound represents one round of thinking + tool calls.
type thinkingRound struct {
	reasoning string
	toolCalls []toolCallEntry
}

type toolCallEntry struct {
	id             string
	name           string
	args           string
	argsMap        map[string]any
	status         string // "pending" | "running" | "done" | "error"
	partial        string
	result         string
	durationMs     int64
	startTime      time.Time
	lastUpdate     time.Time
	expanded       bool
	displayArgs    truncatedField // cached truncation of args for render
	displayPartial truncatedField
	displayResult  truncatedField // cached truncation of result for render
}

// Messages flowing through tea.Update. Runtime events and outbound messages
// are pushed by a background pump goroutine via program.Send; the typewriter
// and spinner tickers fire on their own cadence as pure tea.Cmd chains.
type spinnerTickMsg struct{}
type typewriterTickMsg struct{}

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
func newInteractiveModel(dispatcher *appcore.Dispatcher, messageBus bus.MessageBus, sessionKey, chatID, banner string) *interactiveModel {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.Prompt = "> "
	vp := viewport.New(getTerminalWidth(), transcriptViewportHeight())

	// Buffered so the runtime emission path doesn't stall on our select loop
	// during bursty streams.
	eventCh := make(chan runtime.Event, 512)
	m := &interactiveModel{
		textInput:     ti,
		dispatcher:    dispatcher,
		sessionKey:    sessionKey,
		chatID:        chatID,
		banner:        banner,
		done:          make(chan struct{}),
		runtimeEvents: eventCh,
		outboundCh:    messageBus.ConsumeOutbound(),
		viewport:      vp,
		renderer:      transcriptRenderer{},
		focus:         focusInput,
	}
	m.subscribeRuntimeEvents(sessionKey)
	return m
}

func (m *interactiveModel) subscribeRuntimeEvents(sessionKey string) {
	if m.dispatcher == nil {
		return
	}
	if m.unsubRuntime != nil {
		m.unsubRuntime()
		m.unsubRuntime = nil
	}
	if m.runtimeEvents == nil {
		m.runtimeEvents = make(chan runtime.Event, 512)
	}
	m.unsubRuntime = m.dispatcher.SubscribeRuntimeEvents(func(e runtime.Event) {
		if e.SessionID != sessionKey {
			return
		}
		select {
		case m.runtimeEvents <- e:
		default:
			// Event buffer full — drop to avoid blocking the emitter.
			// The UI can always fall back to the outbound final message.
		}
	})
}

// SetProgram installs the bubbletea program reference and starts the pump
// goroutine that forwards runtime events and outbound messages into the tea
// loop via program.Send.
func (m *interactiveModel) SetProgram(p *tea.Program) {
	m.program = p
	go m.pump()
}

// pump is the single consumer of runtimeEvents / outboundCh. It forwards each
// value to the tea program so Update can remain a pure state transition.
func (m *interactiveModel) pump() {
	for {
		select {
		case ev := <-m.runtimeEvents:
			if m.program != nil {
				m.program.Send(runtimeEventMsg{ev: ev})
			}
		case resp, ok := <-m.outboundCh:
			if !ok {
				return
			}
			if m.program != nil {
				m.program.Send(responseMsg{
					content:         resp.Content,
					reasoning:       resp.Reasoning,
					agentEventFinal: outboundFromAgentEventFinal(resp),
				})
			}
		case <-m.done:
			return
		}
	}
}

// Init starts all background tickers.
func (m *interactiveModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.tickSpinner(), m.tickTypewriter())
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
