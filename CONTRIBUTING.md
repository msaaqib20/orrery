# Contributing

## Getting set up

You need Go 1.22 or newer. That is the entire list — there are no dependencies
to fetch and no tooling to install.

```bash
git clone https://github.com/msaaqib20/orrery
cd orrery
make test
```

## Before you push

```bash
make check    # gofmt check, go vet, and the suite under the race detector
```

CI runs the same three things plus a build on Linux, macOS and Windows, so a
clean `make check` should mean a green PR.

## Ground rules

**No third-party dependencies.** This is
[ADR 0002](docs/adr/0002-standard-library-only.md), and CI fails if `go.mod`
grows a `require` block. If you have a case for breaking it, open an issue and
propose a superseding ADR — don't add the import and hope.

**Failure paths get tested too.** A PR that tests only the happy path will be
sent back. What happens on a malformed body, a dead backend, a cancelled
context, a closed journal?

**No sleeping in tests.** Inject a clock. `session.Store.SetClock`,
`skill.Input.Now` and `provider.Echo.Now` all exist for this.

**Respect the layering.** Imports point downward. If a lower package needs
something from a higher one, the abstraction is in the wrong place — say so in
the PR rather than adding the import.

**Document decisions, not mechanics.** A comment explaining what a line does is
usually noise. A comment explaining why it is that way, and what was rejected,
is worth keeping. Significant decisions get an ADR.

## Commit messages

```
scope: imperative summary under 72 characters

Why this change, and what it changes about the behaviour. Wrap at 72.
Mention anything a future reader would find surprising.

Fixes #123
```

Scopes are package names: `runtime`, `httpapi`, `skill`, `docs`, `ci`.

Small, reviewable commits. A branch that renames things *and* changes behaviour
is two PRs.

## Adding a skill

1. Implement `skill.Skill` in `internal/skill/`.
2. Declare every capability it needs in the descriptor. Under-declaring is a
   security bug, not a convenience.
3. Choose a priority. Specific skills must outrank broad ones — see the
   comment in `math.go` for why `clock` sits above it.
4. Add it to `RegisterBuiltins`.
5. Add a default grant in `config.Default` if it needs one.
6. Test the happy path, the declining path, and cancellation.

## Adding a provider

Implement `provider.Provider`, honour `ctx`, and make it safe for concurrent
use. `internal/provider/echo.go` is the reference. Register it in
`buildRuntime` in `cmd/orreryd/main.go`.

## Review

Expect questions about trade-offs rather than style — style is `gofmt`'s job.
The most common request is for a test that pins down the behaviour being
described in the PR text.
