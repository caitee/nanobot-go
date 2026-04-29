package bus

import (
	"sync"
	"sync/atomic"
	"time"
)

// MessageBus is the pub/sub message bus interface
type MessageBus interface {
	PublishInbound(msg InboundMessage)
	PublishOutbound(msg OutboundMessage)
	PublishAgentEvent(event AgentEvent)
	ConsumeInbound() <-chan InboundMessage
	ConsumeOutbound() <-chan OutboundMessage
	SubscribeAgentEvents() <-chan AgentEvent
	Close()
}

// messageBus implements MessageBus using buffered channels
type messageBus struct {
	inbound   chan InboundMessage
	outbound  chan OutboundMessage
	agentSubs []chan AgentEvent
	subMu     sync.RWMutex
	wg        sync.WaitGroup
	closed    atomic.Bool
	closeOnce sync.Once
}

// New creates a new MessageBus with specified buffer size
func New(bufferSize int) MessageBus {
	return &messageBus{
		inbound:  make(chan InboundMessage, bufferSize),
		outbound: make(chan OutboundMessage, bufferSize),
	}
}

func (b *messageBus) PublishInbound(msg InboundMessage) {
	if b.closed.Load() {
		return
	}
	select {
	case b.inbound <- msg:
	case <-time.After(time.Second):
		// Log dropped message in production
	}
}

func (b *messageBus) PublishOutbound(msg OutboundMessage) {
	if b.closed.Load() {
		return
	}
	select {
	case b.outbound <- msg:
	case <-time.After(time.Second):
		// Log dropped message in production
	}
}

func (b *messageBus) ConsumeInbound() <-chan InboundMessage {
	return b.inbound
}

func (b *messageBus) ConsumeOutbound() <-chan OutboundMessage {
	return b.outbound
}

func (b *messageBus) PublishAgentEvent(event AgentEvent) {
	if b.closed.Load() {
		return
	}
	b.subMu.RLock()
	defer b.subMu.RUnlock()
	for _, ch := range b.agentSubs {
		select {
		case ch <- event:
		case <-time.After(100 * time.Millisecond):
			// Log dropped event - this should not happen in normal operation
			// If events are being dropped, the subscriber is too slow
		}
	}
}

func (b *messageBus) SubscribeAgentEvents() <-chan AgentEvent {
	ch := make(chan AgentEvent, 500) // Increased buffer for high-frequency streaming events
	b.subMu.Lock()
	b.agentSubs = append(b.agentSubs, ch)
	b.subMu.Unlock()
	return ch
}

func (b *messageBus) Close() {
	b.closeOnce.Do(func() {
		if b.closed.Swap(true) {
			return
		}
		close(b.inbound)
		close(b.outbound)
		b.wg.Wait()
	})
}
