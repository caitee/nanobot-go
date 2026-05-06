// Package runtime hosts the pi-mono style agent runtime.
//
// The runtime is split into three layers:
//
//   - Agent (agent.go)      — stateful wrapper: owns transcript, emits events,
//     manages steering/follow-up queues, exposes
//     Prompt / Continue / Steer / FollowUp / Abort.
//   - Loop  (loop.go)       — pure functions runAgentLoop / runAgentLoopContinue
//     that drive turns, tool batches, and hook callouts.
//   - Types (types.go)      — AgentMessage / AgentEvent / Hooks / Options and
//     supporting payloads shared across layers.
//
// Supporting files:
//
//   - state.go   — AgentState and snapshot helpers
//   - queue.go   — PendingMessageQueue (steering / follow-up)
//   - events.go  — EventSink and typed event payloads
//
// All external integrations (channels, cron, memory, UI) plug in via
// Agent.Subscribe() or via hook function fields on Options.
package runtime
