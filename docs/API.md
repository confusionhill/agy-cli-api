# `agy-cli-api` — Complete API Reference

**Base URL:** `http://<host>:8080` (default port `8080`, configurable via `PORT`)  
**Content-Type:** `application/json; charset=utf-8` (except `POST /api/v1/uploads` which accepts `multipart/form-data`, `POST /api/v1/chat/stream` which returns `text/event-stream`, and `GET /api/v1/media` which returns binary file content).

---

## Table of Contents

1. [Overview & Error Format](#1-overview--error-format)
2. [System & Discovery Endpoints](#2-system--discovery-endpoints)
   - [`GET /healthz`](#get-healthz)
   - [`GET /api/v1/models`](#get-apiv1models)
   - [`GET /api/v1/usage`](#get-apiv1usage)
   - [`GET /api/v1/mcp`](#get-apiv1mcp)
3. [Conversation History & Search Endpoints](#3-conversation-history--search-endpoints)
   - [`GET /api/v1/conversations`](#get-apiv1conversations)
   - [`GET /api/v1/conversations/search`](#get-apiv1conversationssearch)
   - [`GET /api/v1/conversations/{id}`](#get-apiv1conversationsid)
   - [`POST /api/v1/conversations/{id}/cancel`](#post-apiv1conversationsidcancel)
4. [Chat & Real-Time SSE Streaming](#4-chat--real-time-sse-streaming)
   - [`POST /api/v1/chat`](#post-apiv1chat)
   - [`POST /api/v1/chat/stream`](#post-apiv1chatstream)
   - [SSE Event Schemas](#sse-event-schemas)
5. [Multimodal Attachments & Media](#5-multimodal-attachments--media)
   - [`POST /api/v1/uploads`](#post-apiv1uploads)
   - [`GET /api/v1/media`](#get-apiv1media)
6. [OpenAI Compatibility Endpoints (/v1/...)](#6-openai-compatibility-endpoints-v1)
   - [`POST /v1/chat/completions`](#post-v1chatcompletions)
   - [`GET /v1/models`](#get-v1models)
   - [`GET /v1/models/{id}`](#get-v1modelsid)

---

## 1. Overview & Error Format

All endpoints support CORS (`Access-Control-Allow-Origin`, `Access-Control-Allow-Methods: GET, POST, OPTIONS`).

When an error occurs (`4xx` or `5xx`), the server returns a JSON object with an `error` message:

```json
{
  "error": "prompt or attachment is required"
}
```

---

## 2. System & Discovery Endpoints

### `GET /healthz`

Verifies that the HTTP server is running and the local `agy` binary is executable.

#### Response `200 OK`
```json
{
  "status": "ok",
  "agy_bin": "/Users/dika/.local/bin/agy",
  "default_workspace": "/Users/dika/Documents/github/akalin-ai",
  "default_permission_mode": "skip-permissions",
  "uploads_dir": "/Users/dika/.gemini/antigravity-cli/uploads",
  "error": "",
  "timestamp": "2026-11-08T18:03:22Z"
}
```

---

### `GET /api/v1/models`

Lists all available models (`agy models`) and custom agents (`agy agents`).

#### Response `200 OK`
```json
{
  "models": [
    {
      "id": "gemini-4-argon-medium",
      "name": "Gemini 4 Argon (Medium)"
    },
    {
      "id": "gemini-4-argon-high",
      "name": "Gemini 4 Argon (High)"
    },
    {
      "id": "gemini-3.8-flash-high",
      "name": "Gemini 3.8 Flash (High)"
    },
    {
      "id": "claude-sonnet-5-5-high",
      "name": "Claude Sonnet 5.5 (High)"
    }
  ],
  "agents": [
    {
      "name": "research",
      "description": "Read-only codebase researcher"
    }
  ]
}
```

---

### `GET /api/v1/usage`

Fetches real-time model quota limits (`agy -p "/usage" --output-format json`) without consuming any tokens.

#### Response `200 OK`
```json
{
  "description": "Within each group, models share a weekly limit and a 5-hour limit...",
  "groups": [
    {
      "name": "Gemini Models",
      "description": "Models within this group: Gemini Flash, Gemini Pro",
      "buckets": [
        {
          "id": "gemini-weekly",
          "name": "Weekly Limit Remaining",
          "description": "You have used some of your weekly limit, it will fully refresh in 5 days, 14 hours.",
          "window": "weekly",
          "remaining_fraction": 0.9845825433731079,
          "remaining_percent": 98.46,
          "reset_time": "2026-11-14T08:41:39Z"
        },
        {
          "id": "gemini-5h",
          "name": "Five Hour Limit Remaining",
          "window": "5h",
          "remaining_fraction": 1,
          "remaining_percent": 100,
          "reset_time": "2026-11-08T23:03:25Z"
        }
      ]
    },
    {
      "name": "Claude and GPT models",
      "description": "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
      "buckets": [
        {
          "id": "3p-weekly",
          "name": "Weekly Limit Remaining",
          "window": "weekly",
          "remaining_fraction": 0.1984855979681015,
          "remaining_percent": 19.85,
          "reset_time": "2026-11-10T11:43:56Z"
        }
      ]
    },
    {
      "name": "Gemini 4 Argon",
      "description": "For paid Pro plans, Argon quota is separate...",
      "buckets": [
        {
          "id": "gemini-argon-wtu-weekly",
          "name": "Gemini 4 Argon Weekly Limit",
          "window": "weekly",
          "remaining_fraction": 0.25010961294174194,
          "remaining_percent": 25.01,
          "reset_time": "2026-11-14T08:35:24Z"
        }
      ]
    }
  ]
}
```

---

### `GET /api/v1/mcp`

Lists all configured MCP servers and their statuses (`agy mcp list`).

#### Response `200 OK`
```json
{
  "mcp_servers": [
    {
      "name": "codegraph",
      "type": "stdio",
      "status": "enabled",
      "command_or_url": "/Users/dika/.local/bin/codegraph serve --mcp"
    },
    {
      "name": "lookin",
      "type": "stdio",
      "status": "enabled",
      "command_or_url": "/Users/dika/.local/bin/lookin-mcp"
    }
  ]
}
```

---

## 3. Conversation History & Search Endpoints

### `GET /api/v1/conversations`

Lists conversation summaries from `~/.gemini/antigravity-cli/conversation_summaries.db`, ordered by `last_modified_time DESC`.

#### Query Parameters
| Parameter | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `limit` | `int` | `50` (max `200`) | Number of conversations to return |
| `offset` | `int` | `0` | Pagination offset |
| `workspace` | `string` | `""` | Substring filter on `workspace_uris` |
| `status` | `string` | `""` | Exact match filter on `status` (e.g. `CASCADE_RUN_STATUS_IDLE`, `CASCADE_RUN_STATUS_RUNNING`) |

#### Response `200 OK`
```json
{
  "count": 1,
  "conversations": [
    {
      "conversation_id": "9506f739-9dce-4233-87ae-528bd688db80",
      "title": "Build Go API",
      "preview": "reply with just the word PONG",
      "step_count": 5,
      "last_modified_time": "2026-11-08 17:47:52.55632+00:00",
      "workspace_uris": [
        "file:///Users/dika/Documents/github/akalin-ai"
      ],
      "status": "CASCADE_RUN_STATUS_IDLE",
      "project_id": "default-cli-project",
      "last_user_input_time": "2026-11-08 17:47:27.657604+00:00"
    }
  ]
}
```

---

### `GET /api/v1/conversations/search`

Performs full-text search across conversation titles, user inputs, and agent responses via the SQLite FTS5 `conversations_fts` table.

#### Query Parameters
| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `q` | `string` | **Yes** | — | Search query string |
| `limit` | `int` | No | `25` (max `100`) | Maximum number of search hits |

#### Response `200 OK`
```json
{
  "query": "PONG",
  "count": 1,
  "results": [
    {
      "conversation_id": "9506f739-9dce-4233-87ae-528bd688db80",
      "title": "reply with just the word PONG",
      "workspace": "akalin-ai",
      "project": "CLI Project",
      "user_snippet": "reply with just the word PONG",
      "agent_snippet": "PONG",
      "step_count": 5,
      "last_modified_time": "1794160047935802000"
    }
  ]
}
```

---

### `GET /api/v1/conversations/{id}`

Returns both the summary metadata and the full step-by-step transcript for a conversation (`brain/<id>/.system_generated/logs/transcript.jsonl`, automatically resolving any truncated fields from `transcript_full.jsonl`).

#### Path Parameters
- `id`: The UUID of the conversation (e.g. `9506f739-9dce-4233-87ae-528bd688db80`).

#### Response `200 OK`
```json
{
  "summary": {
    "conversation_id": "14cdde93-caa8-4be1-a489-3d2868feb6c6",
    "title": "AdventureQuest Worlds Screenshot",
    "preview": "What game and character name is shown...",
    "step_count": 4,
    "last_modified_time": "2026-11-08 18:06:32+00:00",
    "workspace_uris": [
      "file:///Users/dika/Documents/github/akalin-ai"
    ],
    "status": "CASCADE_RUN_STATUS_IDLE",
    "project_id": "default-cli-project",
    "last_user_input_time": "2026-11-08 18:06:17+00:00"
  },
  "steps": [
    {
      "step_index": 0,
      "source": "USER_EXPLICIT",
      "type": "USER_INPUT",
      "status": "DONE",
      "created_at": "2026-11-08T18:06:17Z",
      "content": "<USER_REQUEST>\nWhat game and character name is shown...\n</USER_REQUEST>"
    },
    {
      "step_index": 1,
      "source": "MODEL",
      "type": "PLANNER_RESPONSE",
      "status": "DONE",
      "created_at": "2026-11-08T18:06:17Z",
      "input_tokens": 11173,
      "output_tokens": 173,
      "tool_calls": [
        {
          "name": "read_file",
          "args": {
            "Path": "\"/Users/dika/.../media.jpg\""
          }
        }
      ]
    },
    {
      "step_index": 2,
      "source": "MODEL",
      "type": "GENERIC",
      "status": "DONE",
      "created_at": "2026-11-08T18:06:23Z",
      "content": "Showing binary file of type image/jpeg",
      "media": [
        {
          "mime_type": "image/jpeg",
          "uri": "file:///Users/dika/.gemini/antigravity-cli/brain/14cdde93-caa8-4be1-a489-3d2868feb6c6/.system_generated/logs/media/step_2_media_0.jpg"
        }
      ]
    },
    {
      "step_index": 3,
      "source": "MODEL",
      "type": "PLANNER_RESPONSE",
      "status": "DONE",
      "created_at": "2026-11-08T18:06:23Z",
      "content": "Based on the screenshot: AdventureQuest Worlds (AQW)...",
      "thinking": "The image depicts AdventureQuest Worlds...",
      "input_tokens": 3397,
      "output_tokens": 399,
      "cache_read_tokens": 9119
    }
  ]
}
```

---

### `POST /api/v1/conversations/{id}/cancel`

Cancels an actively running CLI process for the specified `conversation_id`.

#### Response `200 OK`
```json
{
  "conversation_id": "9506f739-9dce-4233-87ae-528bd688db80",
  "cancelled": true
}
```

---

## 4. Chat & Real-Time SSE Streaming

Both `POST /api/v1/chat` and `POST /api/v1/chat/stream` accept the exact same JSON request body (`ChatRequest`).

### `ChatRequest` JSON Body Schema

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `prompt` | `string` | Yes (unless `attachments` or `attachment_paths` is non-empty) | The user message / instruction to send to the agent. |
| `conversation_id` | `string` | No | UUID of an existing conversation to resume (`--conversation`). Omit or leave empty to start a new conversation. |
| `workspace_dir` | `string` | No | Working directory (`cmd.Dir`) for the agent run. Defaults to `AGY_DEFAULT_WORKSPACE`. |
| `add_dirs` | `string[]` | No | Additional workspace directories to grant access to (`--add-dir`). |
| `model` | `string` | No | Model ID from `/api/v1/models` (e.g. `gemini-4-argon-medium`, `claude-sonnet-5-5-high`). |
| `effort` | `string` | No | Reasoning effort level: `low`, `medium`, `high`, `xhigh`, or `max`. |
| `agent` | `string` | No | Custom agent name from `/api/v1/models` (`agents` list). |
| `permission_mode` | `string` | No | Execution permission mode: `skip-permissions` (default), `auto`, `accept-edits`, or `plan`. |
| `disable_slash_commands` | `bool` | No | If `true`, passes `--disable-slash-commands`. |
| `json_schema` | `string` | No | Optional JSON schema string or path to enforce structured output (`--json-schema`). |
| `attachments` | `Attachment[]` | No | Array of inline base64 or path attachments (`{filename, mime_type, data_base64, path}`). |
| `attachment_paths` | `string[]` | No | Array of local file paths (e.g. returned by `POST /api/v1/uploads`) to attach to the prompt. |

---

### `POST /api/v1/chat`

Executes the turn synchronously and returns the final `ResultPayload` once the agent completes its response.

#### Example Request
```json
{
  "prompt": "Reply with PONG",
  "model": "gemini-4-argon-medium",
  "permission_mode": "skip-permissions"
}
```

#### Response `200 OK`
```json
{
  "conversation_id": "9506f739-9dce-4233-87ae-528bd688db80",
  "status": "SUCCESS",
  "response": "PONG\n",
  "duration_seconds": 6.631975,
  "num_turns": 1,
  "usage": {
    "input_tokens": 11102,
    "output_tokens": 17,
    "thinking_tokens": 11,
    "cache_read_tokens": 0,
    "total_tokens": 11119
  }
}
```

---

### `POST /api/v1/chat/stream`

Executes the turn and streams real-time **Server-Sent Events (`text/event-stream`)** as `agy` emits NDJSON updates.

#### SSE Event Schemas

1. **`event: init`** — Emitted immediately when `agy` initializes the session (provides `conversation_id`, active `cwd`, available `tools`, and `permission_mode`):
   ```text
   event: init
   data: {"event":"init","conversation_id":"9506f739-9dce-4233-87ae-528bd688db80","init":{"cwd":"/Users/dika/Documents/github/akalin-ai","tools":["call_mcp_tool","run_command","view_file","write_to_file"],"permission_mode":"request-review"}}
   ```

2. **`event: step_update`** — Emitted for every step transition (`user_input`, `agent_response` text deltas, tool executions, and thinking):
   ```text
   event: step_update
   data: {"event":"step_update","step_update":{"conversation_id":"9506f739-9dce-4233-87ae-528bd688db80","step_index":1,"state":"ACTIVE","step_type":"agent_response","text_delta":"PONG"}}
   ```

3. **`event: result`** — Emitted when the turn finishes with the full aggregated `response` and `usage` metrics:
   ```text
   event: result
   data: {"event":"result","result":{"conversation_id":"9506f739-9dce-4233-87ae-528bd688db80","status":"SUCCESS","response":"PONG\n","duration_seconds":6.63,"num_turns":1,"usage":{"input_tokens":11102,"output_tokens":17,"thinking_tokens":11,"cache_read_tokens":0,"total_tokens":11119}}}
   ```

4. **`event: error`** — Emitted if the CLI process exits with an error:
   ```text
   event: error
   data: {"event":"error","error":"..."}
   ```

5. **`event: done`** — Terminal SSE event indicating the HTTP stream is complete:
   ```text
   event: done
   data: {"status":"completed"}
   ```

---

## 5. Multimodal Attachments & Media

### `POST /api/v1/uploads`

Uploads one or more files (`multipart/form-data`, up to 100 MiB total) to `~/.gemini/antigravity-cli/uploads/`. You can then pass the returned `path` in `attachment_paths` (or `attachments[].path`) when calling `/api/v1/chat` or `/api/v1/chat/stream`.

#### Request
- `Content-Type: multipart/form-data`
- Form field: `file` (or `files`, repeatable)

#### Response `200 OK`
```json
{
  "count": 1,
  "files": [
    {
      "filename": "screenshot.jpg",
      "path": "/Users/dika/.gemini/antigravity-cli/uploads/1794161200000_a1b2c3d4_screenshot.jpg",
      "uri": "file:///Users/dika/.gemini/antigravity-cli/uploads/1794161200000_a1b2c3d4_screenshot.jpg",
      "mime_type": "image/jpeg",
      "size": 148290
    }
  ]
}
```

---

### `GET /api/v1/media`

Serves a media file stored inside `~/.gemini/antigravity-cli/brain/` (such as `step.media[].uri` from conversation transcripts) or `~/.gemini/antigravity-cli/uploads/`. Requests for paths outside those directories return `403 Forbidden`.

#### Query Parameters
| Parameter | Type | Description |
| :--- | :--- | :--- |
| `uri` | `string` | `file://` URI of the media file (e.g. `file:///Users/dika/.gemini/antigravity-cli/brain/.../step_2_media_0.jpg`) |
| `path` | `string` | Alternative to `uri`: absolute filesystem path within the data/uploads directory |

#### Response `200 OK`
Returns the raw binary file with its detected `Content-Type` (`image/jpeg`, `image/png`, `application/pdf`, `audio/mpeg`, etc.).

---

## 6. OpenAI Compatibility Endpoints (`/v1/...`)

`agy-cli-api` exposes a standard OpenAI-compatible API so third-party tools (Cursor, Continue.dev, Cline, Aider, LiteLLM, LangChain, or official `openai` SDKs) can use your local Antigravity CLI as an LLM provider.

### Configuration
- **Base URL:** `http://localhost:8080/v1`
- **API Key:** `not-needed` (any dummy string)

---

### `POST /v1/chat/completions`

Creates a model response for the given chat conversation. Supports both single-turn JSON and real-time Server-Sent Events (`stream: true`).

#### Request Parameters
| Parameter | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `model` | `string` | Yes | Model ID (e.g. `gemini-4-argon-low`, `gemini-4-argon-medium`, `gemini-3.8-flash-low`, `claude-opus-5-5-medium`, etc.) |
| `messages` | `array` | Yes | List of message objects (`{"role": "system"|"user"|"assistant", "content": "..."}`) |
| `stream` | `boolean` | No | If `true`, returns standard OpenAI SSE chunks (`chat.completion.chunk`) ending with `data: [DONE]` |
| `conversation_id` | `string` | No | Optional Antigravity conversation ID to resume (also accepted via `X-Conversation-Id` header) |
| `workspace_dir` | `string` | No | Optional host path to run the prompt inside |
| `effort` | `string` | No | Reasoning effort: `low`, `medium`, `high`, `xhigh`, `max` |

#### Non-Streaming Example:
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon-low",
    "messages": [
      {"role": "user", "content": "Hello!"}
    ],
    "stream": false
  }'
```

#### Streaming Example:
```bash
curl -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-4-argon-low",
    "messages": [
      {"role": "user", "content": "Count from 1 to 5"}
    ],
    "stream": true
  }'
```

---

### `GET /v1/models`

Lists all available models discovered dynamically from `agy models`.

```bash
curl http://localhost:8080/v1/models
```

---

### `GET /v1/models/{id}`

Retrieves metadata for a specific model.

```bash
curl http://localhost:8080/v1/models/gemini-4-argon-medium
```


