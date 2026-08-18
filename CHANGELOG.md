# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

Era 1 (Hardening) work. See [docs/ROADMAP.md](docs/ROADMAP.md).

## [0.1.0-alpha] - 2026-08-18

The foundation. Every layer is present, tested and documented; the surface is
still expected to move.

### Added

- **Runtime** — `runtime.Handle` owns the request lifecycle: journal, route,
  authorise, execute, record. Route and execute are separate steps so that a
  permission denial is a routing-level outcome rather than a handler error.
- **Router** — deterministic keyword scoring against skill descriptors, with a
  configurable threshold and ties broken by priority then name.
- **Permissions** — capability policy with deny-by-default, subject and prefix
  wildcards, and validation of capability names at start-up.
- **Skills** — `clock`, `help`, `math`, `ping` and `recall`, plus a registry
  that validates descriptors on registration. `math` is a full recursive-descent
  parser with correct operator precedence.
- **Providers** — a backend-neutral `Provider` interface, a registry, and the
  offline `Echo` implementation so a fresh clone runs with no API key.
- **Sessions** — bounded per-session turn windows with TTL eviction and an
  injectable clock. Everything handed out is a copy.
- **Journal** — append-only JSONL with sequence numbers that resume across
  restarts, a `Memory` implementation for tests, and replay.
- **Config** — defaults, then file, then environment, then validation, with
  unknown fields rejected and every problem reported at once.
- **HTTP API** — `/v1/message`, `/v1/skills`, `/v1/sessions/{id}`,
  `/v1/version`, `/healthz`, `/readyz`, with request ids, body limits, panic
  recovery and a consistent error envelope.
- **orreryctl** — a client that speaks the same public API and holds no logic
  of its own.
- **Docs** — architecture, API reference, configuration reference, testing
  guide, roadmap, and four ADRs.
- **CI** — fmt, vet, build and race-detector tests across Linux, macOS and
  Windows, plus a check that `go.mod` stays dependency-free.

### Notes

- Requires Go 1.22 or newer, for `http.ServeMux` method patterns.
- Zero third-party dependencies, by design and enforced in CI.
- The API is not authenticated and binds to loopback. See [SECURITY.md](SECURITY.md).

[Unreleased]: https://github.com/msaaqib20/orrery/compare/v0.1.0-alpha...HEAD
[0.1.0-alpha]: https://github.com/msaaqib20/orrery/releases/tag/v0.1.0-alpha
