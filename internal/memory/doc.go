// Package memory implements the two-layer memory model used by nanobot:
//
//   - MEMORY.md   — a long-term, curated scratchpad of facts the agent has
//                   chosen to remember. It is written verbatim into the system
//                   prompt on every call via InjectSystemPrompt.
//   - HISTORY.md  — an append-only, grep-searchable log of consolidated
//                   conversations. The agent never reads it directly; a tool
//                   or an out-of-band consolidator writes to it.
//
// The old internal/agent/memory.go combined this store with a full
// LLM-driven consolidator. That policy lives at the application layer;
// this package keeps only the persistent I/O and the prompt-injection helpers
// so the runtime can plug it in as a simple TransformContext hook.
package memory
