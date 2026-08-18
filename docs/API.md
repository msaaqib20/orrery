# HTTP API

Base URL defaults to `http://127.0.0.1:7717`. All bodies are JSON.

Every response carries an `X-Request-Id` header. If the client sends one (up to
64 characters) it is preserved, so a trace can be followed across processes.

## POST /v1/message

The only endpoint that does any work.

**Request**

```json
{ "session_id": "optional", "text": "what is 12 * 7" }
```

Unknown fields are rejected with `400 bad_json`. Bodies over the configured
limit (1 MiB by default) are rejected with `413 body_too_large`.

**Response**

```json
{
  "session_id": "3f2a1c9e8b7d6a5f4e3d2c1b",
  "text": "12 * 7 = 84",
  "source": "skill",
  "skill": "math",
  "score": 1,
  "elapsed_ms": 0
}
```

`source` is `skill` or `provider`. When it is `provider`, `provider` is set and
`skill` and `score` are omitted.

Omitting `session_id` allocates a new session and returns its id. Pass that id
back to continue the conversation.

## GET /v1/skills

```json
{
  "skills": [
    {
      "name": "clock",
      "summary": "Reports the current date and time.",
      "capabilities": ["clock.read"],
      "examples": ["what time is it"]
    }
  ]
}
```

Sorted by name. `capabilities` shows what the skill *needs*, not what it has
been granted — a skill can appear here and still return 403.

## GET /v1/sessions/{id}

```json
{
  "id": "3f2a1c9e8b7d6a5f4e3d2c1b",
  "created_at": "2026-08-18T09:00:00Z",
  "updated_at": "2026-08-18T09:00:04Z",
  "turns": [
    { "role": "user", "text": "ping", "at": "2026-08-18T09:00:04Z" },
    { "role": "assistant", "text": "pong", "at": "2026-08-18T09:00:04Z" }
  ]
}
```

Returns `404 session_not_found` for an unknown or evicted session. Sessions are
a rolling window, not an archive: only the most recent `session.max_turns`
turns are kept, and idle sessions are pruned.

## GET /healthz

Liveness. Answers as long as the process is up.

```json
{ "status": "ok", "uptime_ms": 128400 }
```

## GET /readyz

Readiness, including what the daemon is wired to.

```json
{ "status": "ready", "skills": 5, "providers": ["echo"], "active_provider": "echo" }
```

## GET /v1/version

```json
{
  "version": "0.1.0-alpha",
  "commit": "unknown",
  "built_at": "unknown",
  "go_version": "go1.22.0",
  "platform": "linux/amd64"
}
```

## Errors

Every error uses one envelope:

```json
{
  "error": {
    "code": "permission_denied",
    "message": "permission denied: clock may not use clock.read (no matching grant)",
    "request_id": "9f2c1a4b8e7d6c5a"
  }
}
```

Match on `code`, not on `message`.

| Status | Code | Meaning |
| --- | --- | --- |
| 400 | `empty_text` | `text` was missing or whitespace |
| 400 | `empty_body` | No request body was sent |
| 400 | `bad_json` | Malformed JSON, or an unknown field |
| 400 | `missing_id` | No session id in the path |
| 403 | `permission_denied` | The matched skill lacks a granted capability |
| 404 | `session_not_found` | Unknown or evicted session |
| 405 | — | Wrong method for a known path |
| 408 | `client_gone` | The client cancelled the request |
| 413 | `body_too_large` | Body exceeded `limits.max_body_bytes` |
| 500 | `internal_error` | Anything else, including a recovered panic |

## Worked example

```bash
# Start a conversation and capture the session id.
SID=$(curl -s localhost:7717/v1/message \
  -H 'content-type: application/json' \
  -d '{"text":"ping"}' | grep -o '"session_id":"[^"]*"' | cut -d'"' -f4)

# Continue it.
curl -s localhost:7717/v1/message \
  -H 'content-type: application/json' \
  -d "{\"session_id\":\"$SID\",\"text\":\"what did i say\"}"

# Read the transcript back.
curl -s "localhost:7717/v1/sessions/$SID"
```
