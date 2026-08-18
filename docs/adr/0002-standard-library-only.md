# ADR 0002: Standard library only

- **Status:** Accepted
- **Date:** 2026-08-18

## Context

An assistant daemon has obvious candidate dependencies: a router (chi, gorilla),
a logger (zerolog, zap), a config loader (viper), an HTTP client wrapper, a
test assertion library.

Each is individually reasonable. Together they are a supply chain, a set of
version constraints, and a build that fails when a transitive dependency yanks
a tag.

## Decision

Depend on nothing outside the Go standard library. `go.mod` has no `require`
block and there is no `go.sum`.

This is affordable because Go 1.22's standard library covers everything needed:

| Need | Standard library |
| --- | --- |
| Method-aware routing with path variables | `http.ServeMux` (1.22) |
| Structured logging | `log/slog` (1.21) |
| Config decoding with strict fields | `encoding/json` + `DisallowUnknownFields` |
| Multi-error reporting | `errors.Join` (1.20) |
| Test HTTP servers | `net/http/httptest` |
| Race detection, coverage, fuzzing | `go test` |

## Consequences

**Good.** `git clone && go build ./...` works offline, on any machine with a Go
toolchain, forever. There is no dependency audit, no `go.sum` conflict in a
merge, and no transitive CVE to triage. The build is reproducible by
construction.

**Bad.** Some things must be written by hand: the arithmetic parser in
`internal/skill/math.go`, the middleware chain, the `Duration` JSON wrapper.
That is roughly 300 lines that a library would have provided.

**Accepted trade.** Those 300 lines are fully understood and fully tested. A
library would be neither. This is a judgement call that suits a project at this
stage, and it is reversible: if a real need appears — a database driver, a gRPC
server — this ADR gets superseded rather than quietly violated.

## Alternatives considered

- **A minimal dependency budget (three libraries, say).** Budgets erode. "Zero"
  is a line that holds; "a few" is a negotiation restarted at every PR.
- **Vendoring.** Solves availability but not audit surface or version drift.
