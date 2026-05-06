# Nanobot Go

A lightweight personal AI assistant framework rewritten in Go, inspired by the original [Python nanobot](https://github.com/HKUDS/nanobot).

## Features

- **Multi-Channel Support**: Telegram, Discord, Slack, WhatsApp, Feishu, DingTalk, QQ, Email, Matrix, WeCom, MoChat
- **Multiple LLM Providers**: OpenAI, Azure OpenAI, Anthropic Claude, OpenRouter
- **Tool System**: Extensible tool registry with built-in tools (shell, filesystem, web, message, cron, spawn, MCP)
- **Session Management**: JSONL-based persistent session storage
- **Cron Scheduling**: Schedule tasks with at/every/cron expressions
- **CLI & Gateway**: Full CLI with Cobra, gateway server for channel orchestration

## Architecture

Nanobot-go follows a four-layer agent design inspired by [pi-mono](https://github.com/OpenPipe/pi-mono): a pure-function `runtime` loop, a streaming `llm` provider abstraction, a `tool` registry with hook points, and an `app` container that wires everything to channels, cron, sessions, and subagents. See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for the full design.

```
nanobot-go/
├── cmd/
│   ├── nanobot/         # CLI entry (TUI + single-shot + onboard)
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
git clone https://github.com/your-repo/nanobot-go.git
cd nanobot-go
make build
```

### Configuration

Run onboard to create the default config:

```bash
./nanobot onboard
```

Or manually create `~/.nanobot/config.json`:

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

### Usage

After building with `make build`, the `nanobot` binary will be created in the current directory:

```bash
# Show status
./nanobot status

# Interactive chat
./nanobot agent

# Single message
./nanobot agent -m "Hello!"

# Start gateway
./nanobot gateway

# Initialize config
./nanobot onboard
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `nanobot onboard` | Initialize configuration (creates default config) |
| `nanobot onboard --wizard` | Interactive configuration wizard |
| `nanobot agent` | Run the agent |
| `nanobot gateway` | Start gateway server |
| `nanobot channels` | Manage channels |
| `nanobot status` | Show status |

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
