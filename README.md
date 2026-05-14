# Ori Go

A lightweight personal AI assistant framework rewritten in Go, inspired by the original [Python nanobot](https://github.com/HKUDS/nanobot).

## Features

- **Multi-Channel Support**: Telegram, Discord, Slack, WhatsApp, Feishu, DingTalk, QQ, Email, Matrix, WeCom, MoChat
- **Multiple LLM Providers**: OpenAI, Azure OpenAI, Anthropic Claude, OpenRouter
- **Tool System**: Extensible tool registry with built-in tools (shell, filesystem, web, message, cron, spawn, MCP)
- **Session Management**: JSONL-based persistent session storage
- **Cron Scheduling**: Schedule tasks with at/every/cron expressions
- **CLI & Gateway**: Full CLI with Cobra, gateway server for channel orchestration

## Architecture

Ori-go follows a four-layer agent design inspired by [pi-mono](https://github.com/OpenPipe/pi-mono): a pure-function `runtime` loop, a streaming `llm` provider abstraction, a `tool` registry with hook points, and an `app` container that wires everything to channels, cron, sessions, and subagents. See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full design.

```
ori-go/
├── cmd/
│   ├── ori/         # CLI entry (TUI + single-shot + onboard)
│   └── gateway/         # Gateway server (channels + health)
├── internal/
│   ├── runtime/         # Agent + loop + events + hooks (pi-mono core)
│   ├── llm/             # Streaming provider abstraction + registry
│   ├── tool/            # AgentTool interface + schema + registry + legacy adapter
│   ├── memory/          # MEMORY.md / HISTORY.md two-layer store
│   ├── app/             # Dispatcher, subagents, event translation
│   ├── bus/             # Inbound/outbound message bus + legacy AgentEvent
│   ├── channels/        # Telegram/Discord/Slack/... adapters
│   ├── providers/       # Legacy provider impls (bridged into internal/llm)
│   ├── tools/           # Legacy tool impls (adapted into internal/tool)
│   ├── session/         # JSONL session store
│   ├── cron/            # Cron service (injects via Dispatcher)
│   ├── plugin/          # Plugin registration
│   ├── skills/          # Always-on skill prompts
│   └── config/          # Configuration loading
└── Makefile             # build / test / lint
```

## Getting Started

### Prerequisites

- Go 1.24.2+

### Installation

```bash
git clone https://github.com/your-repo/ori-go.git
cd ori-go
make build
```

### Configuration

Run onboard to create the default config:

```bash
./ori onboard
```

Or manually create `~/.ori/config.json`:

```json
{
  "Agents": {
    "Model": "claude-opus-4-5",
    "Provider": "auto",
    "MaxTokens": 8192,
    "ContextWindowTokens": 65536,
    "Temperature": 0.1,
    "MaxToolIterations": 40
  },
  "Providers": {
    "OpenAI": {
      "api_key": "your-api-key"
    }
  },
  "Gateway": {
    "Host": "0.0.0.0",
    "Port": 18790
  }
}
```

### MCP Servers

Ori can load MCP server definitions from these files, in order:

- `~/.config/mcp/mcp.json`
- `~/.ori/mcp.json`
- `<workspace>/.mcp.json`
- `<workspace>/.ori/mcp.json`

Later files override earlier files. You can also put the same shape under
`tools.mcp` in `~/.ori/config.json`.

```json
{
  "settings": {
    "idleTimeout": 10,
    "directTools": false
  },
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["-y", "chrome-devtools-mcp@latest"],
      "lifecycle": "lazy",
      "directTools": ["take_screenshot"]
    },
    "remote": {
      "url": "http://localhost:3845/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TOKEN}" },
      "lifecycle": "keep-alive"
    }
  }
}
```

The built-in `mcp` tool is the low-token proxy for status, listing, searching,
describing, connecting, and calling MCP tools/resources/prompts. Servers are
lazy by default; `eager` and `keep-alive` connect during app startup. Metadata
is cached in `~/.ori/mcp-cache.json` and invalidated by a server config hash.

Direct MCP tools are optional. Set global `settings.directTools` or per-server
`directTools` to `true` or a list of remote tool names. Direct tools are built
from cached metadata, so newly added servers are first available through the
`mcp` proxy and direct tools appear after metadata has been refreshed.

See [docs/MCP.md](docs/MCP.md) for the full design, configuration, usage, and
troubleshooting guide. Current MCP support does not include OAuth flows,
host-specific config import, an `/mcp` management UI, MCP UI/AppBridge, or
sampling.

### Usage

After building with `make build`, the `ori` binary will be created in the current directory:

```bash
# Show status
./ori status

# Interactive chat
./ori agent

# Single message
./ori agent -m "Hello!"

# Start gateway
./ori gateway

# Initialize config
./ori onboard
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `ori onboard` | Initialize configuration (creates default config) |
| `ori onboard --wizard` | Interactive configuration wizard |
| `ori agent` | Run the agent |
| `ori gateway` | Start gateway server |
| `ori channels` | Manage channels |
| `ori status` | Show status |

## Development

See [`AGENTS.md`](AGENTS.md) for repository rules and migration boundaries.

```bash
# Build
make build

# Format Go files
make fmt

# Run repository checks
make check

# Test (with race detector)
make test

# Lint (go vet)
make lint

# Clean
make clean
```

## License

MIT
