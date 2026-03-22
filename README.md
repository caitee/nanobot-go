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

```
nanobot-go/
├── cmd/
│   ├── nanobot/         # CLI entry point
│   └── gateway/         # Gateway server
├── internal/
│   ├── agent/           # Core agent loop
│   ├── bus/             # Message bus (pub/sub)
│   ├── channels/        # Channel implementations
│   ├── providers/       # LLM providers
│   ├── tools/           # Tool system
│   ├── session/         # Session management
│   ├── cron/            # Cron scheduling
│   └── config/          # Configuration
└── Makefile             # Build automation
```

## Getting Started

### Prerequisites

- Go 1.25+

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

```bash
# Build
make build

# Test
make test

# Clean
make clean
```

## License

MIT
