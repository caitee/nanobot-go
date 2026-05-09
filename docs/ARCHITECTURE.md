# Ori-go Architecture

Ori-go is a five-layer agent runtime inspired by [pi-mono](https://github.com/OpenPipe/pi-mono). The layers are ordered from lowest to highest dependency:

```
entry → app → runtime → llm / tool / plugin / bus
```

- `llm`, `tool`, `plugin`, `bus` are zero-dependency primitives
- `runtime` composes `llm` and `tool` into an agent loop
- `app` wires everything to I/O (channels, cron, sessions, plugin lifecycle)
- `entry` (`cmd/ori`, `cmd/gateway`) is the process boundary

```
┌──────────────────────────────────────────────────────────────────┐
│ entry                                                            │
│   cmd/ori  (CLI + TUI)     cmd/gateway  (HTTP + channels)   │
└──────────────┬─────────────────────────────┬────────────────────┘
               │                             │
               ▼                             ▼
┌──────────────────────────────────────────────────────────────────┐
│ internal/app                                                     │
│   App (shared infra: bus, sessions, registries, cron, channels)  │
│   Dispatcher (inbound routing + command table + sessions)        │
│   SubagentManager (spawns child runtime.Agent)                   │
│   eventTranslator (runtime.Event → bus.AgentEvent legacy)        │
└──────────────┬─────────────────────────────┬────────────────────┘
               │                             │
               ▼                             ▼
┌──────────────────────────────────────────────────────────────────┐
│ internal/runtime                                                 │
│   Agent (state + queues + lifecycle)                             │
│   runAgentLoop (pure fn: hooks + stream + tool execution)        │
│   Hooks (before/after, transform, shouldStop)                    │
│   Events (typed stream)                                          │
└───────┬──────────────┬──────────────────────────────────────────┘
        │              │
        ▼              ▼
┌───────────────┐  ┌───────────────────────────────────────────────┐
│ internal/llm  │  │ internal/tool                                  │
│   StreamFn    │  │   AgentTool interface                          │
│   StreamEvent │  │   Registry (concurrent-safe)                   │
│   Registry    │  │   Schema validation / casting                  │
│   Bridge to   │  │   Legacy adapter (wraps internal/tools/*)      │
│   providers   │  └───────────────────────────────────────────────┘
└───────┬───────┘
        │
        ▼
┌───────────────────────────────────────────────────────────────────┐
│ internal/providers (legacy impls)   internal/tools (legacy impls) │
│   OpenAI / Anthropic / MiniMax /    shell / fs / web / mcp /      │
│   Azure / OpenRouter                cron / spawn / message        │
└───────────────────────────────────────────────────────────────────┘

Shared primitives (no runtime dependency):
┌──────────────────────────────────────────────────────────────────┐
│ internal/bus     MessageBus (inbound/outbound pub-sub)           │
│ internal/plugin  Plugin interface + Registry (lifecycle mgmt)    │
│ internal/errors  Structured error types (Category/Code/Severity) │
└──────────────────────────────────────────────────────────────────┘
```

## Five Layers

### 0. `entry` — process boundary

`cmd/ori` and `cmd/gateway` are the only packages that contain `main()`. They:

- Parse flags and load `config.Config`
- Construct `app.App` and call `app.Start`
- Wire the TUI (ori) or HTTP server (gateway) to `app.Dispatcher`
- Subscribe to `runtime.Event` directly via `Dispatcher.SubscribeRuntimeEvents` for the TUI

Entry packages have **no business logic** — they are thin wiring only.

### 1. `internal/llm` — streaming provider abstraction

Pi-mono equivalent: `packages/ai/src/types.ts`, `stream.ts`, `api-registry.ts`.

- `Model{ID, Provider, API, Reasoning, MaxTokens, ContextWindow, Cost}` — what to call
- `Context{SystemPrompt, Messages, Tools}` — what to send
- `Message` is a sealed interface (`UserMessage` | `AssistantMessage` | `ToolResultMessage`), backed by `Content` blocks (`TextContent` | `ImageContent` | `ThinkingContent` | `ToolCallContent`)
- `StreamEvent{Kind, ...}` with `Kind` ∈ `start | text_start | text_delta | text_end | thinking_* | toolcall_* | done | error` — one `text_*` or `toolcall_*` region per content block within a single stream
- `StreamFn = func(ctx, model, ctx, opts) <-chan StreamEvent` — the one function every provider must satisfy
- `Registry` maps API name → `StreamFn`, supports `UnregisterSource(sourceID)` so plugins can hot-swap
- `bridge.go` wraps legacy `providers.LLMProvider` as a `StreamFn` (transitional; will be removed once every provider is rewritten natively against `StreamFn`)

This layer has **no runtime dependencies** — it is called from `runtime` and implemented by `providers`.

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

## Shared Primitives

### `internal/bus` — message pub-sub

`MessageBus` is the inbound/outbound pub-sub surface used by channels, cron, the CLI, and the dispatcher. It is **not** used for runtime agent events — those flow through `Dispatcher.SubscribeRuntimeEvents`.

```go
type MessageBus interface {
    PublishInbound(msg InboundMessage)
    PublishOutbound(msg OutboundMessage)
    ConsumeInbound() <-chan InboundMessage
    ConsumeOutbound() <-chan OutboundMessage
    Close()
}
```

`bus` has no dependencies on `runtime`, `app`, or any other internal package.

### `internal/plugin` — plugin registry

`plugin.Registry` manages the lifecycle of all plugins (providers, channels, tools registered as plugins). It is owned by `app.App` and initialized before the dispatcher starts.

```go
type Plugin interface {
    Name() string
    Type() Type                              // "provider" | "channel" | "tool"
    Init(ctx context.Context, app AppContext) error
    Close() error
}

// Optional interfaces a plugin may implement:
type MetadataProvider interface { GetMetadata() Metadata }
type Lifecycle interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

`Metadata` carries `Name`, `Type`, `Source`, `Version`, `Description`, `Author`, `Dependencies`, and `Removable`. The registry stores metadata separately so it can be queried without calling into the plugin itself.

Key registry operations:
- `Register(p Plugin) error` — registers and stores metadata (calls `MetadataProvider` if implemented)
- `InitAll(ctx, AppContext) error` — initializes plugins in registration order
- `CloseAll() error` — closes plugins in reverse registration order
- `Start(ctx, name) / Stop(ctx, name)` — per-plugin lifecycle control (requires `Lifecycle`)
- `Unload(name) error` — closes and removes a plugin; only allowed when `Metadata.Removable = true`
- `GetByType(t Type) []Plugin` — returns all plugins of a given type in registration order
- `ListMetadata() []Metadata` — returns metadata for all registered plugins

`plugin` has no dependencies on `runtime`, `llm`, or `tool`.

### `internal/errors` — structured error types

All subsystems use `internal/errors` to produce and inspect structured errors. This replaces ad-hoc `fmt.Errorf` strings with typed, inspectable values.

```go
type Error struct {
    Category    Category    // "provider" | "tool" | "runtime" | "config" | "plugin"
    Severity    Severity    // "info" | "warning" | "error"
    Code        Code        // e.g. "provider.api_key_missing"
    Message     string
    Cause       error
    Context     map[string]any
    Recoverable bool
}
```

Defined error codes:

| Code | Category | Meaning |
|---|---|---|
| `provider.api_key_missing` | provider | API key not configured |
| `provider.request_failed` | provider | LLM call failed |
| `tool.execution_timeout` | tool | Tool exceeded time limit |
| `tool.not_found` | tool | Tool name not in registry |
| `runtime.context_overflow` | runtime | Message history too long |
| `runtime.internal_error` | runtime | Unexpected runtime failure |
| `config.invalid` | config | Config value fails validation |
| `config.missing_value` | config | Required config key absent |
| `plugin.load_failed` | plugin | Plugin init returned error |
| `plugin.execution_error` | plugin | Plugin runtime error |

Helper predicates (`IsContextOverflow`, `IsAPIKeyMissing`, `IsToolExecutionTimeout`) use `errors.As` to unwrap structured errors from any depth in the chain.

`runtime/errors.go` provides mapping helpers (`mapProviderError`, `mapRuntimeError`, `mapGetAPIKeyError`) that convert raw errors from providers and the loop into structured `*errors.Error` values before they surface as `runtime.Event` error payloads.

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
- **Add a plugin**: implement `plugin.Plugin` (and optionally `MetadataProvider` / `Lifecycle`), register via `app.PluginRegistry.Register`. The plugin receives an `AppContext` on `Init` to access registries and shared infra.

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
- **Structured errors**: all subsystems produce `*errors.Error` values with a `Category`, `Code`, and `Recoverable` flag. Callers can inspect errors without string matching, and the loop surfaces them as typed `runtime.Event` payloads rather than opaque strings.
- **Plugin isolation**: `plugin.Registry` owns lifecycle (init order, close order, hot-unload) without coupling to any specific subsystem. Plugins receive `AppContext` — a narrow interface — rather than a concrete `*App`, keeping the dependency direction clean.
