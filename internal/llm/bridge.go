package llm

import (
	"context"
	"time"

	oldprov "nanobot-go/internal/providers"
)

// LegacyProvider is the transitional interface mirroring internal/providers.LLMProvider.
// It exists so the new llm package can wrap a legacy provider without importing
// the old package throughout the codebase.
type LegacyProvider interface {
	Chat(ctx context.Context, messages []oldprov.Message, tools []oldprov.ToolDef, opts oldprov.ChatOptions) (*oldprov.LLMResponse, error)
	StreamGenerate(ctx context.Context, messages []oldprov.Message, tools []oldprov.ToolDef, opts oldprov.ChatOptions) <-chan oldprov.StreamResponse
	GetDefaultModel() string
}

// FromLegacy wraps a legacy LLMProvider as a new Provider. Every Stream()
// call spawns a goroutine that pumps the legacy StreamResponse channel and
// translates events into the unified StreamEvent format.
//
// This is a migration shim. Once every call site switches to the new
// abstraction, the legacy package can be deleted and this file alongside it.
func FromLegacy(p LegacyProvider) Provider {
	return &legacyProviderAdapter{legacy: p}
}

type legacyProviderAdapter struct {
	legacy LegacyProvider
}

func (a *legacyProviderAdapter) Stream(ctx context.Context, model Model, c Context, opts StreamOptions) EventStream {
	out := make(chan StreamEvent, 100)

	go func() {
		defer close(out)

		legacyMsgs := toLegacyMessages(c)
		legacyTools := toLegacyTools(c.Tools)
		legacyOpts := oldprov.ChatOptions{
			Temperature:     opts.Temperature,
			MaxTokens:       opts.MaxTokens,
			Model:           firstNonEmpty(opts.ModelOverride, model.ID),
			ReasoningEffort: opts.Reasoning,
		}
		if legacyOpts.MaxTokens == 0 {
			legacyOpts.MaxTokens = 4096
		}

		stream := a.legacy.StreamGenerate(ctx, legacyMsgs, legacyTools, legacyOpts)

		var partial = AssistantMessage{
			Provider:  model.Provider,
			API:       model.API,
			Model:     model.ID,
			Timestamp: time.Now(),
		}
		var textBuf, thinkingBuf string

		emit := func(ev StreamEvent) {
			select {
			case <-ctx.Done():
			case out <- ev:
			}
		}

		// Each emit takes a fresh pointer so consumers can safely hold it.
		freshPartial := func() *AssistantMessage {
			p := partial
			return &p
		}

		// Start event gives subscribers a handle to the partial message.
		emit(StreamEvent{Kind: StreamEventStart, Partial: freshPartial()})

		for chunk := range stream {
			if chunk.Error != nil {
				final := partial
				final.StopReason = StopReasonError
				final.ErrorMessage = chunk.Error.Error()
				errCopy := final
				emit(StreamEvent{
					Kind:         StreamEventError,
					StopReason:   StopReasonError,
					ErrorMessage: chunk.Error.Error(),
					Message:      &final,
					Partial:      &errCopy,
				})
				return
			}

			if chunk.Chunk != "" {
				if chunk.IsReasoning {
					thinkingBuf += chunk.Chunk
					partial.Content = partialContentWithTextAndThinking(textBuf, thinkingBuf)
					emit(StreamEvent{
						Kind:    StreamEventThinkingDelta,
						Delta:   chunk.Chunk,
						Partial: freshPartial(),
					})
				} else {
					textBuf += chunk.Chunk
					partial.Content = partialContentWithTextAndThinking(textBuf, thinkingBuf)
					emit(StreamEvent{
						Kind:    StreamEventTextDelta,
						Delta:   chunk.Chunk,
						Partial: freshPartial(),
					})
				}
			}

			if chunk.Done {
				// Reconstruct the final assistant message with text, thinking,
				// and any tool calls reported by the legacy provider.
				var finalContent []Content
				if thinkingBuf != "" || chunk.ReasoningContent != "" {
					th := chunk.ReasoningContent
					if th == "" {
						th = thinkingBuf
					}
					finalContent = append(finalContent, ThinkingContent{Thinking: th})
				}
				text := chunk.Content
				if text == "" {
					text = textBuf
				}
				if text != "" {
					finalContent = append(finalContent, TextContent{Text: text})
				}
				for _, tc := range chunk.ToolCalls {
					finalContent = append(finalContent, ToolCallContent{
						ID:        tc.ID,
						Name:      tc.Name,
						Arguments: tc.Arguments,
					})
				}

				stop := mapFinishReason(chunk.FinishReason, len(chunk.ToolCalls) > 0)

				final := AssistantMessage{
					Content:   finalContent,
					API:       model.API,
					Provider:  model.Provider,
					Model:     model.ID,
					Usage: Usage{
						Input:  chunk.Usage.PromptTokens,
						Output: chunk.Usage.CompletionTokens,
					},
					StopReason: stop,
					Timestamp:  time.Now(),
				}
				doneMsg := final
				donePartial := final
				emit(StreamEvent{
					Kind:       StreamEventDone,
					StopReason: stop,
					Message:    &doneMsg,
					Partial:    &donePartial,
				})
				return
			}
		}

		// Channel closed without Done — synthesize an error.
		final := partial
		final.StopReason = StopReasonError
		final.ErrorMessage = "legacy provider closed stream without Done"
		errMsg := final
		errPartial := final
		emit(StreamEvent{
			Kind:         StreamEventError,
			StopReason:   StopReasonError,
			ErrorMessage: final.ErrorMessage,
			Message:      &errMsg,
			Partial:      &errPartial,
		})
	}()

	return out
}

// partialContentWithTextAndThinking builds a synthetic partial content slice
// holding the accumulated thinking + text so subscribers can render either.
func partialContentWithTextAndThinking(text, thinking string) []Content {
	var out []Content
	if thinking != "" {
		out = append(out, ThinkingContent{Thinking: thinking})
	}
	if text != "" {
		out = append(out, TextContent{Text: text})
	}
	return out
}

// toLegacyMessages flattens Context back into the legacy Message shape.
func toLegacyMessages(c Context) []oldprov.Message {
	msgs := make([]oldprov.Message, 0, len(c.Messages)+1)
	if c.SystemPrompt != "" {
		msgs = append(msgs, oldprov.Message{
			Role:    "system",
			Content: c.SystemPrompt,
		})
	}
	for _, m := range c.Messages {
		msgs = append(msgs, messageToLegacy(m))
	}
	return msgs
}

// messageToLegacy converts a single llm.Message to the legacy wire shape.
func messageToLegacy(m Message) oldprov.Message {
	switch mm := m.(type) {
	case UserMessage:
		return oldprov.Message{
			Role:    "user",
			Content: legacyContentFromBlocks(mm.Content),
		}
	case AssistantMessage:
		out := oldprov.Message{
			Role: "assistant",
		}
		var text string
		for _, block := range mm.Content {
			switch b := block.(type) {
			case TextContent:
				text += b.Text
			case ToolCallContent:
				out.ToolCalls = append(out.ToolCalls, oldprov.ToolCall{
					ID:        b.ID,
					Name:      b.Name,
					Arguments: b.Arguments,
				})
			}
		}
		out.Content = text
		return out
	case ToolResultMessage:
		return oldprov.Message{
			Role:       "tool",
			Content:    legacyContentFromBlocks(mm.Content),
			ToolCallID: mm.ToolCallID,
			Name:       mm.ToolName,
		}
	}
	return oldprov.Message{}
}

// legacyContentFromBlocks flattens content blocks into legacy Content.
// If all blocks are text, returns a string; otherwise returns []ContentBlock.
func legacyContentFromBlocks(blocks []Content) any {
	onlyText := true
	var buf string
	for _, b := range blocks {
		if t, ok := b.(TextContent); ok {
			buf += t.Text
			continue
		}
		onlyText = false
		break
	}
	if onlyText {
		return buf
	}
	out := make([]oldprov.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch bb := b.(type) {
		case TextContent:
			out = append(out, oldprov.ContentBlock{Type: "text", Text: bb.Text})
		case ImageContent:
			out = append(out, oldprov.ContentBlock{Type: "image", ImageURL: "data:" + bb.MimeType + ";base64," + bb.Data})
		}
	}
	return out
}

func toLegacyTools(ts []Tool) []oldprov.ToolDef {
	if len(ts) == 0 {
		return nil
	}
	out := make([]oldprov.ToolDef, len(ts))
	for i, t := range ts {
		out[i] = oldprov.ToolDef{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func mapFinishReason(reason string, hadToolCalls bool) StopReason {
	switch reason {
	case "", "stop", "end_turn":
		if hadToolCalls {
			return StopReasonToolUse
		}
		return StopReasonStop
	case "length", "max_tokens":
		return StopReasonLength
	case "tool_use", "tool_calls":
		return StopReasonToolUse
	case "error":
		return StopReasonError
	}
	if hadToolCalls {
		return StopReasonToolUse
	}
	return StopReasonStop
}
