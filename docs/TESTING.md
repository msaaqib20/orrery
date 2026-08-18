# Testing

```bash
make test        # go test ./...
make race        # go test -race ./...
make cover       # coverage report to coverage.out and a summary
make check       # fmt check + vet + race, the pre-push gate
```

The whole suite runs offline in seconds and touches no network. Anything that
needs a backend uses `provider.Static`, the in-package test double.

## How the suite is arranged

Every package tests three things: the happy path, the failure modes, and — for
anything shared — concurrent access.

| Package | What its tests pin down |
| --- | --- |
| `config` | Precedence between defaults, file and env; that unknown fields are rejected; that validation reports every problem at once |
| `journal` | Sequence numbers are unique, resume across a reopen, and survive a round trip through disk |
| `session` | The rolling window trims oldest-first; returned sessions are copies; TTL eviction uses an injected clock rather than sleeping |
| `permission` | Deny by default; wildcards behave; unknown capability names are rejected |
| `provider` | The registry rejects duplicates; `Echo` is deterministic; cancellation is honoured |
| `skill` | Descriptor validation; the arithmetic parser gets precedence right; every built-in honours cancellation |
| `router` | Scoring, thresholds, and that ties break deterministically |
| `runtime` | The full request lifecycle, including both fall-through paths and the denial path |
| `httpapi` | Status codes, error envelopes, body limits, request ids, panic recovery |
| `cmd/orreryctl` | Error envelope parsing and context handling against a real `httptest` server |

## Conventions

**No sleeping.** Anything time-dependent takes an injected clock:
`session.Store.SetClock`, `skill.Input.Now`, `provider.Echo.Now`. A test that
sleeps is a test that will be flaky on a loaded CI runner.

**Test the invariant, not the implementation.** `TestDeniedSkillIsNotSilentlyReplaced`
asserts that the provider was never called, not that a particular branch ran.
It should survive a rewrite of `dispatch`.

**Failure paths get equal weight.** Roughly half the assertions are about what
happens when something goes wrong: a malformed body, a dead backend, a closed
journal, a cancelled context.

**Concurrency tests are explicit.** Every type documented as safe for
concurrent use has a test that drives it from eight goroutines, and `make race`
runs the whole suite under the detector.

## Adding tests for a new skill

```go
func TestWeatherHandlesCancellation(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    if _, err := (Weather{}).Execute(ctx, Input{Text: "weather"}); !errors.Is(err, context.Canceled) {
        t.Errorf("Execute returned %v, want context.Canceled", err)
    }
}
```

`TestBuiltinsHonourCancellation` covers every registered built-in in one loop,
so a new skill is checked automatically once it is added to `RegisterBuiltins`.

## Coverage

Coverage is a smoke alarm, not a target. A package well under the rest is worth
a look; chasing a number produces tests that assert the code does what the code
does.

```bash
make cover
go tool cover -html=coverage.out
```
