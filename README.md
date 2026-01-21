# CodeLoom

CodeLoom is an MCP server that indexes a codebase into a graph and exposes tools for context, impact, and dependency analysis.

## Build

```
go build ./cmd/codeloom
```

## Quick start

```
./codeloom start --transport=sse --port 3003
./codeloom index ./path/to/repo
```

## Transports

CodeLoom supports three transports plus a combined mode. The default is SSE. If stdin is piped and no transport is explicitly provided, it auto-selects stdio.

- SSE (default)
  - Start: `codeloom start --transport=sse --port 3003`
  - Endpoints: `GET /sse`, `POST /message`
- Streamable HTTP
  - Start: `codeloom start --transport=streamable-http --port 3003 --http-path=/mcp`
  - Endpoint: `/mcp`
- Both (SSE + Streamable HTTP on the same port)
  - Start: `codeloom start --transport=both --port 3003 --http-path=/mcp`
- Stdio
  - Start: `codeloom start --transport=stdio`

## Health endpoints

- `GET /health` for liveness
- `GET /ready` for readiness and initialization status

## Config file

Example `~/.codeloom/config.toml`:

```
[server]
transport = "sse"          # stdio | sse | streamable-http | both | auto
port = 3003
http_path = "/mcp"
watcher_debounce_ms = 100
index_timeout_ms = 60000
```

## Environment variables

- `CODELOOM_TRANSPORT`
- `CODELOOM_HTTP_PATH`
- `CODELOOM_WATCHER_DEBOUNCE_MS`
- `CODELOOM_INDEX_TIMEOUT_MS`

## MCP client configs

If your client connects over stdio, it must pass `-transport=stdio` now that the default is SSE.
Sample configs are also available in `examples/`.

### Stdio

```json
{
  "mcpServers": {
    "codeloom": {
      "command": "codeloom",
      "args": ["start", "-transport=stdio"]
    }
  }
}
```

### SSE

```json
{
  "mcpServers": {
    "codeloom": {
      "url": "http://127.0.0.1:3003/sse"
    }
  }
}
```

### Streamable HTTP

```json
{
  "mcpServers": {
    "codeloom": {
      "url": "http://127.0.0.1:3003/mcp"
    }
  }
}
```

## Systemd

See `systemd/codeloom.service` and `systemd/codeloom.env` for a working systemd unit template. It defaults to SSE, with a commented Streamable HTTP alternative.

## Notes

- If your MCP client offers a reasoning tool, the tool name is `sequential_thinking`.
