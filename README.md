# `agy-cli-api`

A lightweight, pure-Go HTTP & Server-Sent Events (SSE) API wrapper around the local Google Antigravity (`agy`) CLI.

Designed for accessing your local Antigravity agent (along with all your local MCP integrations, plugins, skills, and workspaces) remotely from a personal mobile or web client over a private VPN/tunnel (such as Tailscale, WireGuard, or Cloudflare Tunnel).

---

## Features

- **Real-Time SSE Streaming (`POST /api/v1/chat/stream`)**: Streams `init`, `step_update` (text deltas, thinking, tool calls), `result` (token usage, duration), and `done` events directly from `agy -p --output-format stream-json`.
- **Single-Turn JSON Chat (`POST /api/v1/chat`)**: Sends a prompt (new or resumed conversation) and returns the complete structured response.
- **Model Quota & Limit Inspection (`GET /api/v1/usage`)**: Fetches real-time weekly and 5-hour quota buckets (`remaining_fraction`, `remaining_percent`, `reset_time`) per model family via `agy -p "/usage" --output-format json` with zero token cost.
- **Models & MCP Discovery (`GET /api/v1/models`, `GET /api/v1/mcp`)**: Lists all available models, custom agents, and local MCP servers.
- **Conversation History & FTS5 Search (`GET /api/v1/conversations`, `/search`, `/{id}`)**: Reads `~/.gemini/antigravity-cli/conversation_summaries.db` (read-only pure-Go SQLite) and full `transcript.jsonl` / `transcript_full.jsonl` logs.
- **Active Turn Cancellation (`POST /api/v1/conversations/{id}/cancel`)**: Stops a running CLI process mid-stream.

---

## Quick Start

```bash
# Copy optional environment config
cp .env.example .env

# Run tests
go test ./...

# Build and start the server
go build -o bin/agy-cli-api ./cmd/server
./bin/agy-cli-api
```

---

## Configuration (`.env` or Environment Variables)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `8080` | HTTP server port |
| `AGY_BIN` | `agy` (or `~/.local/bin/agy`) | Path to the `agy` CLI executable |
| `AGY_DATA_DIR` | `~/.gemini/antigravity-cli` | Path to the Antigravity CLI data directory |
| `AGY_DEFAULT_WORKSPACE` | Current working directory | Default workspace directory when `workspace_dir` is omitted |
| `AGY_DEFAULT_PERMISSION_MODE` | `skip-permissions` | Default permission mode (`skip-permissions`, `auto`, `accept-edits`, `plan`) |
| `AGY_CORS_ORIGINS` | `*` | Comma-separated list of allowed CORS origins |

---

## API Reference

### 1. Health Check
```bash
curl http://localhost:8080/healthz
```

### 2. Model Quota / Usage Limits
```bash
curl http://localhost:8080/api/v1/usage
```

### 3. Available Models & Agents
```bash
curl http://localhost:8080/api/v1/models
```

### 4. Configured MCP Servers
```bash
curl http://localhost:8080/api/v1/mcp
```

### 5. List & Search Conversations
```bash
# List recent conversations
curl "http://localhost:8080/api/v1/conversations?limit=20&offset=0"

# Full-text search across conversations
curl "http://localhost:8080/api/v1/conversations/search?q=pong"

# Get full conversation summary + transcript steps
curl "http://localhost:8080/api/v1/conversations/<conversation_id>"
```

### 6. Stream Chat (SSE)
```bash
curl -N -X POST http://localhost:8080/api/v1/chat/stream \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Hello! List my workspace files.",
    "conversation_id": "",
    "workspace_dir": "/Users/dika/Documents/github/akalin-ai",
    "model": "gemini-4-argon-medium",
    "effort": "medium",
    "permission_mode": "skip-permissions"
  }'
```

### 7. Non-Streaming Chat
```bash
curl -X POST http://localhost:8080/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "Reply with PONG",
    "permission_mode": "skip-permissions"
  }'
```

### 8. Cancel Active Conversation Turn
```bash
curl -X POST "http://localhost:8080/api/v1/conversations/<conversation_id>/cancel"
```
