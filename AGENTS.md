# Development Rules

## Project Shape

Ori is a Go personal AI assistant framework. It is organized as a layered agent runtime:

- `cmd/ori` and `cmd/gateway` are process entrypoints. Keep them thin: flags, config loading, TUI/HTTP wiring, and calls into `internal/app`.
- `internal/app` owns application wiring: config, sessions, registries, channels, cron, dispatcher, subagents, and event translation for legacy channel consumers.
- `internal/runtime` owns the provider-agnostic agent loop, state, queues, typed runtime events, and hooks.
- `internal/llm` is the canonical streaming provider abstraction: `Model`, `Context`, `Message`, `StreamEvent`, `StreamFn`, and registry.
- `internal/tool` is the canonical tool contract and registry surface: `tool.AgentTool`, schemas, execution modes, results, and the legacy adapter.
- `internal/providers` and `internal/tools` are legacy compatibility layers. Prefer native `internal/llm` and `internal/tool` work unless adapting existing implementations.
- `internal/bus`, `internal/plugin`, and `internal/errors` are shared primitives. Keep them independent of runtime and app concerns.
- `internal/channels`, `internal/cron`, `internal/session`, `internal/memory`, `internal/skills`, and `internal/config` provide integrations around the runtime.

Use `docs/ARCHITECTURE.md` as the source of truth for dependency direction and migration boundaries.

## Conversational Style

- Keep repository changes narrowly scoped.
- Prefer direct technical language over narrative explanations.
- State verification performed, especially when tests were skipped because the change was documentation-only.

## Code Quality

- All exported APIs must have explicit Go types and useful package-appropriate comments.
- New features and bug fixes require unit tests. Put tests near the package being changed and prefer table tests where they clarify behavior.
- Keep changes aligned with Go `1.24.2` from `go.mod`.
- Use structured errors from `internal/errors` for inspectable subsystem failures instead of string matching.
- Avoid package-level mutable state unless it already exists as part of a registry, service, or test fake.
- Keep provider-specific and tool-specific behavior out of `internal/runtime`; use stream adapters, tool adapters, registries, and hooks instead.

## Provider Work

- New provider behavior should be expressed as an `internal/llm.StreamFn`.
- Register providers through `llm.Registry.Register(api, streamFn, sourceID)`.
- Use `llm.FromLegacy` only as a transition path for existing `providers.LLMProvider` implementations.
- Keep provider API quirks in provider or bridge code, not in `internal/runtime` or `internal/app`.
- Add focused tests for stream event ordering, tool-call streaming, errors, and default-model behavior when those areas change.

## Tool Work

- New tools should implement `tool.AgentTool`.
- Use `tool.FromLegacy` only when wrapping an existing `internal/tools.Tool`.
- Prefer registering stock tools via `internal/app/defaults.go` plugin helpers so lifecycle and config wiring stay centralized.
- Use `ExecutionMode`, `PrepareArguments`, streaming `UpdateFn`, and `tool.Result.Terminate` instead of expanding legacy tool interfaces.
- Keep filesystem, shell, web, cron, spawn, and MCP behavior in tool packages or adapters, not in the runtime loop.

## Runtime And App Boundaries

- `internal/runtime` should depend only on `internal/llm`, `internal/tool`, and the standard library.
- Runtime extension points are hooks: `TransformContext`, `BeforeToolCall`, `AfterToolCall`, and `ShouldStopAfter`.
- `internal/app.Dispatcher` owns inbound routing, command handling, session history loading, and steering active runs.
- `internal/app.SubagentManager` owns child agents and tool restrictions; avoid recursive spawn behavior.
- Channels communicate through `internal/bus` and should not call runtime internals directly.
- `cmd/*` should not contain business logic.
- Document any new cross-layer dependency before introducing it.

## Commands

- Run `make fmt` after editing Go files.
- Run `make check` after code changes. It runs `fmt-check`, `go vet ./...`, and `go test -race ./...`.
- For docs-only changes, `make check` is optional; say clearly that no Go verification was needed.
- Use targeted `go test ./path` while iterating, then `make check` before finishing code changes.
- Do not change generated binaries (`ori`, `gateway`) unless the task explicitly asks for build artifacts.

## Compatibility And Migration

- Prefer extending `internal/llm` and `internal/tool` for new provider and tool work.
- Treat `internal/providers` and `internal/tools` as legacy compatibility layers; only add code there when adapting or preserving existing implementations.
- Do not expand legacy interfaces when the same behavior can be expressed through `internal/llm.StreamFn` or `tool.AgentTool`.
- When migrating a legacy provider or tool, remove bridge-only logic after native parity is covered by tests.
- Keep legacy event translation in `internal/app` until channel adapters no longer need `bus.AgentEvent`.
