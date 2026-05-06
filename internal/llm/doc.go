// Package llm defines the provider-agnostic LLM abstraction layer used by the
// agent runtime. It mirrors the pi-mono packages/ai design:
//
//   - Model      — metadata describing a concrete model (provider, id, cost, ...)
//   - Context    — systemPrompt + messages + tools passed to each call
//   - Message    — user / assistant / toolResult variants with content blocks
//   - StreamEvent — a unified stream-event union emitted by every provider
//   - StreamFn   — entry point: (ctx, model, Context, Options) -> EventStream
//   - Registry   — dynamic registration / unregistration of providers by api name
//
// Concrete providers live in internal/llm/providers and adapt their native
// streaming APIs into the common StreamEvent sequence.
package llm
