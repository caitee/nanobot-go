# Development Rules

## Conversational Style

- Keep repository changes narrowly scoped.
- Prefer direct technical language over narrative explanations.

## Code Quality

- All exported APIs must have explicit Go types.
- New features and bug fixes require unit tests.
- Prefer extending `internal/llm` and `internal/tool` for new provider and tool work.
- Treat `internal/providers` and `internal/tools` as legacy compatibility layers; only add code there when adapting existing implementations into the new abstractions.
- Do not expand legacy interfaces when the same behavior can be expressed through `internal/llm.StreamFn` or `tool.AgentTool`.
- Keep provider-specific and tool-specific behavior out of `internal/runtime`; use hooks, registries, and adapters instead.

## Commands

- Run `make check` after code changes.
- Use `make fmt` when Go files need formatting.
- Keep CI and local Go versions aligned with `go.mod`.

## Architecture Boundaries

- `internal/llm` is the canonical streaming provider abstraction.
- `internal/tool` is the canonical tool contract and registry surface.
- `internal/runtime` owns the agent loop and hooks, but should stay provider-agnostic and tool-agnostic.
- `internal/app` wires runtime to sessions, channels, cron, subagents, and CLI or gateway entrypoints.
- Document any new cross-layer dependency before introducing it.
