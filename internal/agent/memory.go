package agent

import (
	"nanobot-go/internal/session"
)

type MemoryConsolidator struct {
	threshold int
}

func NewMemoryConsolidator(threshold int) *MemoryConsolidator {
	return &MemoryConsolidator{threshold: threshold}
}

func (mc *MemoryConsolidator) ShouldConsolidate(sess *session.Session) bool {
	return len(sess.Messages) > mc.threshold
}

func (mc *MemoryConsolidator) Consolidate(sess *session.Session) {
	// Keep last N messages, summarize the rest
	if len(sess.Messages) > mc.threshold {
		keep := sess.Messages[len(sess.Messages)-mc.threshold:]
		sess.Messages = keep
		sess.LastConsolidated = len(sess.Messages)
	}
}
