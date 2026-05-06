package runtime

import "sync"

// PendingMessageQueue buffers steering or follow-up messages. It is safe to
// enqueue from any goroutine.
type PendingMessageQueue struct {
	mu       sync.Mutex
	mode     QueueMode
	messages []AgentMessage
}

func newQueue(mode QueueMode) *PendingMessageQueue {
	if mode == "" {
		mode = QueueDefaultMode
	}
	return &PendingMessageQueue{mode: mode}
}

// Mode returns the queue's current drain mode.
func (q *PendingMessageQueue) Mode() QueueMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.mode
}

// SetMode updates the drain mode.
func (q *PendingMessageQueue) SetMode(m QueueMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.mode = m
}

// Enqueue appends a message.
func (q *PendingMessageQueue) Enqueue(m AgentMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = append(q.messages, m)
}

// HasItems reports whether the queue is non-empty.
func (q *PendingMessageQueue) HasItems() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages) > 0
}

// Drain returns pending messages according to the queue mode:
//   - QueueAll         — returns everything, clears the buffer
//   - QueueOneAtAtTime — returns only the oldest message, keeps the rest
func (q *PendingMessageQueue) Drain() []AgentMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil
	}
	if q.mode == QueueAll {
		out := q.messages
		q.messages = nil
		return out
	}
	first := q.messages[0]
	q.messages = q.messages[1:]
	return []AgentMessage{first}
}

// Clear discards all pending messages.
func (q *PendingMessageQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.messages = nil
}
