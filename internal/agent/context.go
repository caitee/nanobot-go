package agent

import (
	"fmt"
	"nanobot-go/internal/session"
)

type ContextBuilder struct{}

func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{}
}

func (cb *ContextBuilder) Build(sess *session.Session) []string {
	var ctx []string
	ctx = append(ctx, "You are a helpful AI assistant.")
	ctx = append(ctx, fmt.Sprintf("Session has %d messages", len(sess.Messages)))
	return ctx
}
