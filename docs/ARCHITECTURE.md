# Nanobot-go Architecture

Nanobot-go is a four-layer agent runtime inspired by [pi-mono](https://github.com/OpenPipe/pi-mono). The layers are ordered from lowest to highest dependency: `llm` and `tool` are zero-dependency primitives, `runtime` composes them into an agent loop, and `app` wires everything to I/O (channels, cron, sessions, CLI/gateway).

```
┌─────────────────────────────────────────────────────────────┐
│ cmd/nanobot (CLI + TUI)           cmd/gateway (HTTP + channels) │
└──────────────┬──────────────────────────┬──────────────────┘
               │                          │
               ▼                          ▼
┌─────────────────────────────────────────────────────────────┐
│ internal/app                                                 │
│   Dispatcher (inbound routing + command table + sessions)    │
│   SubagentManager (spawns child runtime.Agent)               │
│   Event translation (runtime.Event → bus.AgentEvent legacy)  │
└──────────────┬──────────────────────────┬──────────────────┘
               │                          │
               ▼                          ▼
┌──────────────────────────┐   ┌──────────────────────────────┐
│ internal/runtime         │   │ internal/memory              │
│   Agent (state + queues) │   │   MEMORY.md + HISTORY.md     │
│   runAgentLoop (pure fn) │   │   TransformContext hook      │
│   Hooks (before/after,   │   └──────────────────────────────┘
│   transform, shouldStop) │
│   Events (typed stream)  │
└───────┬──────────────┬───┘
        │              │
        ▼              ▼
┌───────────────┐  ┌───────────────────────────────────────────┐
│ internal/llm  │  │ internal/tool                              │
│   StreamFn    │  │   AgentTool interface                      │
│   StreamEvent │  │   Registry (concurrent-safe)               │
│   Registry    │  │   Schema validation / casting              │
│   Bridge to   │  │   Legacy adapter (wraps internal/tools/*)  │
│   providers   │  └───────────────────────────────────────────┘
└───────┬───────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────┐
│ internal/providers (legacy impls)   internal/tools (legacy impls)│
│   OpenAI / Anthropic / MiniMax /    shell / fs / web / mcp /    │
│   Azure / OpenRouter                cron / spawn / message      │
└───────────────────────────────────────────────────────────────┘
```

## Four Layers

### 1. `internal/llm` — streaming provider abstraction

Pi-mono equivalent: `packages/ai/src/types.ts`, `stream.ts`, `api-registry.ts`.

- `Model{ID, Provider, API, Reasoning, MaxTokens, ContextWindow, Cost}` — what to call
- `Context{SystemPrompt, Messages, Tools}` — what to send
- `Message` is a sealed interface (`UserMessage` | `AssistantMessage` | `ToolResultMessage`), backed by `Content` blocks (`TextContent` | `ImageContent` | `ThinkingContent` | `ToolCallContent`)
- `StreamEvent{Kind, ...}` with `Kind` ∈ `start | text_start | text_delta | text_end | thinking_* | toolcall_* | done | error` — one `text_*` or `toolcall_*` region per content block within a single stream
- `StreamFn = func(ctx, model, ctx, opts) <-chan StreamEvent` — the one function every provider must satisfy
- `Registry` maps API name → `StreamFn`, supports `UnregisterSource(sourceID)` so plugins can hot-swap
- `bridge.go` wraps legacy `providers.LLMProvider` as a `StreamFn` (transitional; will be removed once every provider is rewritten natively against `StreamFn`)

This layer has **no runtime dependencies** — it's called from `runtime` and implemented by `providers`.

### 2. `internal/tool` — tool registry + hook surface

Pi-mono equivalent: `packages/agent/src/types.ts:AgentTool` and `agent-loop.ts` execution paths.

```go
type AgentTool interface {
    Name() string
    Label() string                         // human-friendly display
    Description() string
    Parameters() map[string]any            // JSON schema
    ExecutionMode() ExecutionMode          // "" | sequential | parallel
    PrepareArguments(raw map[string]any) (map[string]any, error)
    Execute(ctx, callID, args, update UpdateFn) (*Result, error)
}
```

- `schema.go` — JSON-schema cast + validate (migrated verbatim from the legacy `tools.BaseTool`)
- `registry.go` — concurrent-safe `Register / Unregister / Get / List / Definitions`
- `adapter.go` — `FromLegacy(tools.Tool) AgentTool` lets M5-era tools keep working; `UnwrapLegacy()` exposes the underlying legacy value when the app needs to call concrete-type methods (e.g., `SpawnTool.SetSpawner`)
- `Result.Terminate = true` causes the agent loop to stop after this tool returns (pi-mono parity)

### 3. `internal/runtime` — the agent loop

Pi-mono equivalent: `packages/agent/src/agent.ts` (Agent object) + `agent-loop.ts` (low-level loop).

**Key split**: state/lifecycle vs. pure loop.

- `Agent` (`agent.go`) — owns `AgentState`, listeners, `PendingMessageQueue` (for steering and follow-up), and the active run's cancel func. Methods: `Prompt / Continue / Steer / FollowUp / Abort / WaitForIdle / Subscribe / Reset`.
- `runAgentLoop` (`loop.go`) — pure function. Takes config (including hooks and stream fn), emits `Event` values on an `EventSink`. The loop structure is:

  ```
  agent_start
    ├─ for each turn until stop:
    │    ├─ turn_start
    │    ├─ transformContext(messages)           // hook
    │    ├─ convertToLLM(messages)               // hook
    │    ├─ stream LLM → emit message_update deltas
    │    ├─ message_end (assistant)
    │    ├─ for each tool call (seq or parallel):
    │    │    ├─ beforeToolCall hook (may block / override)
    │    │    ├─ tool_execution_start
    │    │    ├─ tool.Execute (can emit tool_execution_update)
    │    │    ├─ afterToolCall hook (may rewrite result)
    │    │    └─ tool_execution_end
    │    ├─ message_start + message_end (tool_result)
    │    ├─ turn_end
    │    ├─ drain steering queue (one-at-a-time or all)
    │    └─ shouldStopAfter hook
    └─ drain followUp queue → loop again if non-empty
  agent_end
  ```

- `events.go` — `Event` value + typed accessors (`.TurnEnd()`, `.ToolEnd()`, …) in the same style as the legacy `bus.AgentEvent`
- `queue.go` — `PendingMessageQueue` with two drain modes (`QueueModeAll` vs `QueueModeOneAtATime`), matching pi-mono's `drainPendingMessages`
- `hooks/hooks.go` — reusable hook factories: `ChainBefore`, `ChainAfter`, `Logging`, `AllowList`, `DenyList`, `Redact`
- `context.go` — `SystemPromptBuilder` (workspace + skills + runtime header) and `RuntimeContextTransform` (injects current time / channel / chat into the system prompt as a `TransformContext` hook)

`runtime` depends only on `llm`, `tool`, and the standard library.

### 4. `internal/app` — wiring

- `App` holds shared infra: `bus.MessageBus`, `session.SessionStore`, `tool.Registry`, `llm.Registry`, legacy `providers.Registry`, `channels.Manager`, `cron.CronService`, `plugin.Registry`.
- `Dispatcher` consumes `bus.InboundMessage`, routes commands (`/help`, `/stop`, `/status`, `/new`, `/reasoning`) through a command table, and spawns a fresh `runtime.Agent` per turn with the session's history loaded into `InitialMessages`. One active run per session; `/stop` aborts it; new inbound while active is steered into the running agent.
- `SubagentManager` spawns **child** `runtime.Agent` instances with a restricted tool set (enforced by an `AllowList` hook) and publishes the final assistant text back to the parent session as an `InboundMessage` from channel `system`.
- `eventTranslator` (legacy) converts `runtime.Event` → `bus.AgentEvent` so existing channel adapters can keep consuming `bus.SubscribeAgentEvents()`. The **CLI TUI** subscribes directly to `runtime.Event` via `Dispatcher.SubscribeRuntimeEvents`.

## Data flow: one user turn

```
 User types  →  bus.PublishInbound
                   │
                   ▼
            Dispatcher.handleInbound
                   │
              ┌────┴────┐
              │         │
       command path    runtime.Agent.Prompt
       (/help, /stop)  (new run or Steer into existing)
                             │
                             ▼
                       runAgentLoop
                             │
                        (stream events)
                             │
                   ┌─────────┼─────────┐
                   ▼         ▼         ▼
              eventTranslator   TUI subscription   SubagentManager
                   │            (direct)              │
                   ▼                                  ▼
             bus.AgentEvent                  runtime.Agent (child)
                   │
                   ▼
            channels / bus.PublishOutbound
```

## Memory

`internal/memory` implements two-layer memory as a `TransformContext` hook:

- **MEMORY.md** — long-term distilled summary, prepended to the system prompt
- **HISTORY.md** — append-only session log, trimmed and inserted as the most recent messages
- Consolidation runs when session length exceeds threshold (old messages → MEMORY, recent kept as-is)

This is a plain Go implementation of pi-mono's "transform context before LLM call" pattern.

## Extending

- **Add a provider**: implement `llm.StreamFn`, register via `llm.Registry.Register(api, streamFn, sourceID)`. Legacy-style providers can still go through `providers.Registry` and be auto-bridged.
- **Add a tool**: implement `tool.AgentTool`, register via `app.ToolRegistry.Register`. If you have a legacy `tools.Tool`, wrap with `tool.FromLegacy`.
- **Add a hook**: write a `runtime.BeforeToolCall` / `AfterToolCall` / `TransformContext` / `ShouldStopAfter` function, pass it via `runtime.Options`. Chain multiple with `hooks.ChainBefore` / `hooks.ChainAfter`.
- **Add a channel**: implement `channels.Channel`, register in `channels.Manager`. It will receive outbound messages and can publish inbound ones via `bus.PublishInbound`.

## Migration boundaries

- New provider work should land in `internal/llm` first. `internal/providers` remains a compatibility layer for implementations that have not yet been rewritten to `llm.StreamFn`.
- New tool work should land in `internal/tool` first. `internal/tools` should only grow when wrapping or adapting an existing legacy tool.
- `internal/runtime` should not absorb provider-specific or tool-specific branching. Express those differences through stream adapters, tool adapters, and hooks.
- When migrating legacy code, remove bridge-only logic once the native `internal/llm` or `internal/tool` implementation reaches parity instead of keeping both paths indefinitely.

## Why this structure

- **Testability**: `runAgentLoop` is a pure function — tests construct an `EventSink`, a fake `StreamFn`, fake tools, and assert on the event stream. No process state, no I/O.
- **Extensibility**: hooks are the only extension points that touch the loop. Adding memory, logging, rate limiting, or tool filtering is a single function, not a patch to the loop.
- **Provider parity**: every LLM looks the same to the runtime — a `StreamFn` emitting `StreamEvent`. Provider quirks (MiniMax's tool-call JSON merging, Anthropic's `content_block_start` cadence) live in provider code, not the loop.
- **Session safety**: each turn spawns a fresh `runtime.Agent` with the session's history. Concurrent sessions never share mutable agent state.
