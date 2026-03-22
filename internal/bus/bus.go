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
	ConsumeInbound() <-chan InboundMessage
	ConsumeOutbound() <-chan OutboundMessage
	Close()
}

// messageBus implements MessageBus using buffered channels
type messageBus struct {
	inbound  chan InboundMessage
	outbound chan OutboundMessage
	wg       sync.WaitGroup
	closed   atomic.Bool
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
