// Package tool defines the agent-runtime tool abstraction.
//
// AgentTool is richer than the legacy tools.Tool interface:
//
//   - Label()            — human-readable UI label
//   - ExecutionMode()    — "" | "sequential" | "parallel" override per tool
//   - PrepareArguments() — optional compatibility shim run before validation
//   - Execute(ctx, id, args, update) — streaming-capable execution
//
// ToolResult carries content blocks (text / image) and structured details,
// plus a Terminate hint that can early-exit the agent loop.
//
// Registry keeps tools addressable by name and enforces schema validation.
// Built-in tools live under internal/tool/builtin/*.
package tool
