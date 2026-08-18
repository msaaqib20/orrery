# Security

## Reporting a vulnerability

Do not open a public issue. Use GitHub's private vulnerability reporting
("Report a vulnerability" under the Security tab), or email the maintainers.

Please include what an attacker can do, how to reproduce it, and the version
you tested. You will get an acknowledgement within a few days.

## Threat model

Being explicit about what this does and does not defend against.

### Assumed

- The daemon runs on a machine the operator controls.
- Skills are compiled in and therefore trusted code.
- The API is reachable only from loopback.

### Defended against

| Threat | Mitigation |
| --- | --- |
| A skill using privileges nobody granted it | Capability policy, checked before execution, deny by default ([ADR 0003](docs/adr/0003-capability-based-permissions.md)) |
| Routing around the policy by rephrasing | A denial never falls through to the provider |
| Memory exhaustion from a large request | `limits.max_body_bytes`, enforced with `http.MaxBytesReader` |
| Unbounded session growth | A rolling turn window plus TTL eviction |
| A wedged backend holding a request open | `provider.timeout`, plus server read and write timeouts |
| A panicking handler taking the process down | Recovery middleware returning a 500 |
| Config typos silently changing behaviour | Unknown fields and unknown capability names are rejected at start-up |
| Supply chain compromise | No third-party dependencies ([ADR 0002](docs/adr/0002-standard-library-only.md)) |

### Not defended against

These are real and you should know about them.

- **No authentication or authorisation on the API.** Anyone who can reach the
  port can drive the daemon and read every session transcript. The default bind
  is `127.0.0.1` for exactly this reason. Do not expose it without putting
  something that authenticates in front.
- **No transport encryption.** Plain HTTP. Terminate TLS in front of it.
- **No sandboxing.** A skill holding `fs.write` or `shell.exec` runs with the
  daemon's full privileges. The capability policy is a guard rail against
  mistakes, not a defence against hostile code. Sandboxing is Era 5.
- **Journal contents are plaintext.** `data_dir` holds every message anyone has
  sent. It is created `0750` with files at `0640`, but that is a floor, not
  encryption at rest. Treat the directory as sensitive.
- **No rate limiting.** A local client can saturate the daemon.
- **Capabilities are coarse.** `fs.read` is all-or-nothing; there are no path
  scopes yet.

## Hardening a deployment

- Leave `addr` on loopback.
- Set `permissions.default_allow` to `false` and enumerate grants. Never use
  `"*"` outside development.
- Point `data_dir` at a directory only the daemon's user can read.
- Run as an unprivileged user with a systemd unit using `ProtectSystem=strict`,
  `PrivateTmp=true` and `NoNewPrivileges=true`.
- Watch the logs for `skill is registered but not permitted` at start-up; it
  means a grant is missing.

## Supported versions

While the project is pre-1.0, only the latest release gets fixes.
