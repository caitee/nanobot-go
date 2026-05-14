# Ori MCP 设计与接入指南

本文说明 Ori 当前 MCP 能力的设计逻辑、配置方式、使用方法和接入新 MCP server 的推荐流程。

## 当前状态

Ori 的 MCP 能力是默认工具插件，不是 `internal/runtime` 的特殊分支。启动时 `internal/app` 注册 `tool.mcp` 插件，插件加载 MCP 配置、创建 `MCPManager`，再向 `tool.Registry` 注册一个低 token 的 `mcp` proxy tool，以及可选的 direct MCP tools。

核心边界如下：

```text
cmd/ori, cmd/gateway
  -> internal/app/defaults.go
      -> tool.mcp plugin
          -> internal/tools/MCPManager
              -> official MCP Go SDK client sessions
          -> internal/tool.Registry
              -> mcp proxy tool
              -> optional mcp_<server>_<tool> direct tools
  -> internal/runtime
      -> only sees ordinary tool.AgentTool values
```

这意味着 MCP 的协议、生命周期、缓存和工具名规则都在工具/插件层完成；runtime 只负责执行普通 `tool.AgentTool`。

## 设计逻辑

### Proxy Tool

`mcp` 是默认暴露给模型的统一入口，适合发现和调用 MCP 能力。它支持：

- `status`: 查看 server 是否配置、是否连接、缓存了多少 tools/resources/prompts。
- `connect`: 主动连接一个或全部 server，并刷新元数据缓存。
- `list`: 列出 tools/resources/prompts。
- `search`: 按 server、tool 名称和描述搜索工具。
- `describe`: 查看某个工具的 schema。
- `call`: 调用某个 MCP tool。
- `tools`, `resources`, `prompts`: 兼容旧 action 形态。

默认建议先 `search` 或 `describe`，再 `call`，这样模型不需要一次性把所有远端工具 schema 放进上下文。

### Direct Tools

direct tools 是可选能力。开启后，Ori 会把缓存中的 MCP tool 注册成独立工具，例如：

```text
mcp_minimax_web_search
mcp_chrome_devtools_take_screenshot
```

命名规则：

- 前缀为 `mcp_`。
- server 名和 tool 名会转成 snake case。
- 工具名最长 64 字符。
- 冲突或过长时追加 hash 后缀。

direct tools 依赖缓存。新 server 第一次接入时，先通过 `mcp` proxy 调 `connect`、`list` 或 `search` 刷新缓存；重启 Ori 后，符合 `directTools` 规则的 direct tools 才会在默认工具列表中出现。

### Manager

`MCPManager` 负责：

- 合并 MCP 配置。
- 懒连接和显式连接。
- `eager`/`keep-alive` 启动连接。
- idle timeout 后关闭非 keep-alive session。
- 失败后进入 backoff，默认 60 秒。
- 刷新 tool/resource/prompt 元数据缓存。
- 关闭应用时关闭 MCP sessions 并保存缓存。

### Transport

Ori 使用官方 MCP Go SDK。transport 选择规则：

- 配置了 `command` 时，默认使用 `stdio`。
- 配置了 `url` 且没有 `command` 时，默认使用 `streamable_http`。
- `url` 自动模式下，streamable HTTP 失败后会回退到 SSE；如果 URL 不是 `/sse` 结尾，会尝试追加 `/sse`。
- 可以显式设置 `transport` 为 `stdio`、`streamable_http`、`streamableHttp`、`http` 或 `sse`。
- `headers` 只用于 HTTP/SSE transport。

### 结果转换

MCP 返回值会转换成 Ori 的 `llm.Content`：

- text content -> `llm.TextContent`
- image content -> `llm.ImageContent`
- embedded resource text -> `llm.TextContent`
- embedded binary resource -> 简短文本摘要
- `structuredContent` 和 `_meta` 放入 tool result 的 `Details`
- MCP `isError: true` 会作为工具错误反馈给模型

## 配置加载

Ori 默认只加载两个 MCP 配置文件，后者覆盖前者：

1. `~/.ori/mcp.json`
2. `<workspace>/.ori/mcp.json`

如果 `~/.ori/config.json` 中写了 `tools.mcp`，它会作为内联配置最后应用。用户级配置推荐放在 `~/.ori/mcp.json`，项目共享配置推荐放在 `<workspace>/.ori/mcp.json`。

路径和环境变量会展开：

- `~` 会展开为用户 home。
- `${NAME}` 或 `$NAME` 会从环境变量展开。
- `command`、`args`、`env`、`headers`、`url` 都支持环境变量展开。

默认缓存路径是：

```text
~/.ori/mcp-cache.json
```

缓存只保存 tool/resource/prompt 元数据和 server config hash，不保存明文 env/header secret。hash 会随影响连接和工具暴露的配置变化而变化，从而触发缓存失效。

## 配置字段

全局 `settings`：

| 字段 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `idleTimeout` | number | `600` | 秒。非 keep-alive session 空闲多久后关闭。 |
| `failureBackoff` | number | `60` | 秒。连接失败后多久内避免重复重连。 |
| `cachePath` | string | `~/.ori/mcp-cache.json` | 元数据缓存路径。 |
| `directTools` | boolean 或 string[] | `false` | 全局 direct tools 开关或允许列表。 |

单个 server：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `command` | string | stdio server 启动命令，例如 `npx`、`uvx`。 |
| `args` | string[] | stdio server 参数。 |
| `env` | object | 注入 stdio 子进程的环境变量。 |
| `url` | string | HTTP/SSE MCP endpoint。 |
| `headers` | object | HTTP/SSE 请求头。 |
| `transport` | string | 显式 transport。 |
| `timeout` | number | 秒。HTTP transport 请求超时；也是未设置 `toolTimeout` 时的默认 tool 调用超时。 |
| `toolTimeout` | number | 秒。单次 tool/resource/prompt 调用超时，优先于 `timeout`。 |
| `lifecycle` | string | `lazy`、`eager`、`keep-alive`。默认 `lazy`。 |
| `enabled` | boolean | 是否启用该 server。 |
| `enabledTools` | string[] | 只缓存和暴露这些远端 tools。 |
| `excludeTools` | string[] | 排除这些远端 tools。 |
| `directTools` | boolean 或 string[] | server 级 direct tools 配置，优先于全局。 |

## 接入示例

### stdio server

`~/.ori/mcp.json`：

```json
{
  "settings": {
    "idleTimeout": 600,
    "failureBackoff": 60,
    "directTools": false
  },
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["-y", "chrome-devtools-mcp@latest"],
      "lifecycle": "lazy",
      "directTools": ["take_screenshot"]
    }
  }
}
```

### MiniMax MCP

推荐把 API key 放在 shell 环境中，不要把真实 key 写进仓库或文档：

```bash
export MINIMAX_API_KEY="your_api_key"
```

`~/.ori/mcp.json`：

```json
{
  "mcpServers": {
    "MiniMax": {
      "command": "uvx",
      "args": ["minimax-coding-plan-mcp", "-y"],
      "env": {
        "MINIMAX_API_KEY": "${MINIMAX_API_KEY}",
        "MINIMAX_API_HOST": "https://api.minimaxi.com"
      },
      "lifecycle": "lazy",
      "directTools": ["web_search"]
    }
  }
}
```

第一次使用时先通过 proxy 刷新缓存：

```json
{
  "action": "connect",
  "server": "MiniMax"
}
```

或者直接搜索：

```json
{
  "action": "search",
  "query": "web_search"
}
```

缓存刷新后重启 Ori，`web_search` 会以 direct tool 形式暴露为：

```text
mcp_minimax_web_search
```

### HTTP MCP server

```json
{
  "mcpServers": {
    "remote": {
      "url": "http://localhost:3845/mcp",
      "headers": {
        "Authorization": "Bearer ${MCP_TOKEN}"
      },
      "lifecycle": "keep-alive",
      "timeout": 30
    }
  }
}
```

如果 server 只支持 SSE，也可以显式写：

```json
{
  "mcpServers": {
    "remote-sse": {
      "transport": "sse",
      "url": "http://localhost:3845/sse"
    }
  }
}
```

## 使用方式

用户通常不需要手写 tool call，只要要求 Ori 使用 MCP server 即可。下面的 JSON 是模型实际可调用的 `mcp` proxy 参数形态，适合调试时参考。

查看状态：

```json
{
  "action": "status"
}
```

连接并刷新某个 server：

```json
{
  "action": "connect",
  "server": "MiniMax"
}
```

列出某个 server 的 tools/resources/prompts：

```json
{
  "action": "list",
  "server": "MiniMax"
}
```

搜索工具：

```json
{
  "action": "search",
  "query": "search"
}
```

查看工具 schema：

```json
{
  "action": "describe",
  "server": "MiniMax",
  "tool": "web_search"
}
```

调用工具：

```json
{
  "action": "call",
  "server": "MiniMax",
  "tool": "web_search",
  "arguments": {
    "query": "百合竹怎么养护"
  }
}
```

读取 resource：

```json
{
  "action": "resources",
  "server": "my-server",
  "resource_action": "read",
  "uri": "file:///example/resource.txt"
}
```

获取 prompt：

```json
{
  "action": "prompts",
  "server": "my-server",
  "prompt_action": "get",
  "name": "review",
  "arguments": {
    "topic": "MCP integration"
  }
}
```

## 接入 Checklist

1. 选择配置位置：个人配置用 `~/.ori/mcp.json`，项目配置用 `<workspace>/.ori/mcp.json`。
2. 确认 MCP server 类型：本地进程用 `command`/`args`，远端服务用 `url`。
3. 把 secret 放到环境变量中，用 `${NAME}` 在配置里引用。
4. 启动 Ori。
5. 让模型调用 `mcp status` 或 `mcp connect` 检查连接。
6. 调用 `mcp list` 或 `mcp search` 确认工具元数据。
7. 调用 `mcp describe` 查看工具参数。
8. 用 `mcp call` 传 `arguments` 调用工具。
9. 需要 direct tools 时，配置 `directTools`，刷新缓存并重启 Ori。

## 常见问题

### No MCP servers configured

检查配置路径是否正确。Ori 只读取 `~/.ori/mcp.json`、`<workspace>/.ori/mcp.json`，以及 `~/.ori/config.json` 中的 `tools.mcp`。

### stdio server 启动失败

确认 `command` 在 Ori 运行环境的 `PATH` 中可用，例如 `npx`、`uvx`。如果命令依赖 shell 初始化文件，建议写绝对路径或确保启动 Ori 的 shell 已加载对应环境。

### HTTP server 连接失败

默认会先试 `streamable_http`。如果服务只支持 SSE，可以显式设置：

```json
{
  "transport": "sse",
  "url": "http://localhost:3845/sse"
}
```

### Server is in backoff

连接失败后会进入失败退避，默认 60 秒。修复命令、环境变量或 URL 后，可以等待退避结束，或重启 Ori。

### direct tool 没出现

direct tools 来自缓存。先通过 `mcp connect`、`mcp list` 或 `mcp search` 刷新缓存，然后重启 Ori。还要确认：

- `settings.directTools` 或 server 的 `directTools` 已开启。
- tool 没有被 `excludeTools` 排除。
- 如果配置了 `enabledTools`，该 tool 必须在允许列表中。

### Tool 参数报缺少字段

调用 MCP tool 时，远端工具参数必须放在 `arguments` 对象中：

```json
{
  "action": "call",
  "server": "MiniMax",
  "tool": "web_search",
  "arguments": {
    "query": "百合竹怎么养护"
  }
}
```

不要把工具参数放在 `uri`、`query` 或顶层字段中，除非只是使用 proxy 的 `search` action。

## 安全说明

MCP server 可能是本地进程，也可能是远端服务。它们可能读取本地文件、访问网络或执行额外逻辑。接入前应确认来源可信，并遵循：

- 不把真实 API key 提交到仓库。
- 优先用环境变量注入 secret。
- 对不信任的 server 使用 `enabledTools` 做允许列表。
- 对高风险工具用 `excludeTools` 排除。
- 不把 OAuth、浏览器 profile 或系统级 token 交给未知 server。

## 当前不支持

第一版 MCP 支持聚焦核心可用能力，暂不支持：

- OAuth 流程。
- host-specific config imports。
- `/mcp` TUI 管理面板。
- MCP UI/AppBridge。
- MCP sampling。
