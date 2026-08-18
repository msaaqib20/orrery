# Configuration

Precedence, lowest to highest:

1. Built-in defaults (`config.Default`)
2. A JSON file passed with `-config`
3. Environment variables
4. The `-addr` flag

Validation runs last, so an invalid value is rejected no matter which layer
produced it. Unknown fields in the config file are an error, not a warning — a
typo that silently does nothing is worse than a failed start-up.

## Settings

| Field | Env | Default | Notes |
| --- | --- | --- | --- |
| `addr` | `ORRERY_ADDR` | `127.0.0.1:7717` | Loopback by default; see the security note below |
| `data_dir` | `ORRERY_DATA_DIR` | `./data` | The journal lives here |
| `log.level` | `ORRERY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `log.format` | `ORRERY_LOG_FORMAT` | `text` | `text` or `json` |
| `provider.name` | `ORRERY_PROVIDER` | `echo` | Must be a registered provider |
| `provider.timeout` | — | `30s` | Ceiling on one backend call |
| `provider.max_tokens` | — | `1024` | Passed through to the backend |
| `session.max_turns` | — | `40` | Rolling window per session |
| `session.ttl` | — | `2h` | Idle sessions are pruned every 5 minutes |
| `router.min_score` | `ORRERY_ROUTER_MIN_SCORE` | `0.6` | 0–1; below this, the provider takes the request |
| `permissions.default_allow` | — | `false` | Leave this false |
| `permissions.grants` | — | see below | Skill name (or `*`) to capability list |
| `limits.max_body_bytes` | — | `1048576` | 1 MiB |
| `limits.request_timeout` | — | `45s` | Server read timeout |
| `limits.shutdown_grace` | — | `10s` | How long in-flight requests get on SIGTERM |

Durations are written as strings: `"30s"`, `"2h"`, `"1m30s"`.

## Example

```json
{
  "addr": "127.0.0.1:7717",
  "data_dir": "/var/lib/orrery",
  "log": { "level": "debug", "format": "json" },
  "provider": { "name": "echo", "timeout": "20s", "max_tokens": 512 },
  "session": { "max_turns": 60, "ttl": "4h" },
  "router": { "min_score": 0.55 },
  "permissions": {
    "default_allow": false,
    "grants": {
      "clock": ["clock.read"],
      "recall": ["session.read"]
    }
  },
  "limits": {
    "max_body_bytes": 1048576,
    "request_timeout": "45s",
    "shutdown_grace": "10s"
  }
}
```

## Capabilities

Grants map a skill name to the capabilities it may use. A skill that needs a
capability nobody granted it is refused at request time and logged as a warning
at start-up, so the gap is visible before a user finds it.

| Capability | Covers |
| --- | --- |
| `clock.read` | Reading the system clock |
| `session.read` | Reading conversation history |
| `provider.call` | Invoking a completion backend |
| `fs.read` / `fs.write` | Filesystem access |
| `net.http` | Outbound HTTP |
| `shell.exec` | Executing commands |

Two wildcards exist:

- `"*"` as a **subject** grants to every skill.
- `"fs.*"` as a **capability** grants everything under that prefix.

Both are conveniences for development. In anything resembling production,
enumerate the grants: the wildcard is what turns a policy into a formality.

Unknown capability names are rejected at start-up rather than ignored.

## Security note on `addr`

The default binds to `127.0.0.1`. There is **no authentication** on the API. If
you change `addr` to `0.0.0.0`, anyone who can reach the port can drive the
daemon and read every session transcript. Put it behind something that
authenticates, or leave it on loopback. See [SECURITY.md](../SECURITY.md).
