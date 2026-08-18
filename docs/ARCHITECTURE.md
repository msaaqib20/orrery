# Architecture

## The shape of the thing

orrery is one process with three concerns kept strictly apart: **deciding what
to do** (router, policy), **doing it** (skills, providers), and **remembering
what happened** (sessions, journal). Transport sits on top and holds no
decisions at all.

```
              ┌───────────────────────────────┐
              │        cmd/orreryd            │  wiring, signals, lifecycle
              └───────────────┬───────────────┘
                              │
              ┌───────────────▼───────────────┐
              │      internal/httpapi         │  decode → delegate → encode
              └───────────────┬───────────────┘
                              │
              ┌───────────────▼───────────────┐
              │      internal/runtime         │  the only place decisions
              │   (request lifecycle owner)   │  are sequenced
              └──┬────────┬────────┬───────┬──┘
                 │        │        │       │
        ┌────────▼──┐ ┌───▼────┐ ┌─▼─────┐ │
        │  router   │ │ policy │ │ skill │ │
        └───────────┘ └────────┘ └───────┘ │
                                           │
                          ┌────────────────▼──────────────┐
                          │  provider   session   journal │
                          └───────────────────────────────┘
```

## Layering rule

Imports point downward only. `runtime` may import `skill`; `skill` may not
import `runtime`. `config` and `version` are leaves that everything may use.

This is not decoration. It is what makes it possible to test the router with no
runtime, the runtime with no HTTP server, and the HTTP server with a real
runtime and a fake provider — which is exactly how the test suite is arranged.

## Request lifecycle

`runtime.Handle` is the single entry point and always performs the same steps
in the same order:

1. **Reject empty input.** Before anything is allocated or journalled.
2. **Resolve the session.** An empty id allocates a new one.
3. **Journal the request.**
4. **Append the user turn.** History is written before the answer exists, so a
   crash mid-request still leaves an accurate record of what was asked.
5. **Route.** A deterministic keyword score against skill descriptors.
6. **Authorise, then execute.** Never the other way round.
7. **Append the assistant turn.**
8. **Journal the reply.**

### Why authorise before execute

A skill declares its capabilities in its `Descriptor`, which is static data the
router already reads. That means the runtime knows what a skill will need
before it constructs any input for it. Checking at that point makes denial a
routing-level outcome that can be journalled and returned as a clean 403,
rather than an error thrown from somewhere inside a handler that has already
started work.

### Why a denial does not fall through

`callSkill` returns an error on denial, and `dispatch` propagates it. It would
be easy — and wrong — to fall through to the provider instead. A policy that
can be routed around by phrasing a request differently is not a policy.

A skill that *declines* is a different case entirely: it returns an ordinary
error, the runtime journals `handled: false`, and the provider takes over. The
two paths are distinguished by where the error comes from, not by its content.

## Invariants

These hold everywhere and each has a test that fails if it stops holding.

| Invariant | Enforced by |
| --- | --- |
| A denied skill never reaches the provider | `TestDeniedSkillIsNotSilentlyReplaced` |
| A declining skill always reaches the provider | `TestDecliningSkillFallsThroughToProvider` |
| Routing is deterministic for identical input | `TestRoutingIsDeterministic` |
| Stored sessions cannot be mutated by callers | `TestCloneIsolatesCallers` |
| A journal failure never fails a request | `TestJournalFailureDoesNotFailTheRequest` |
| Journal sequence numbers are unique and resume across restarts | `TestFileResumesSequenceAcrossReopen` |
| Provider calls are bounded by a timeout | `TestProviderTimeoutIsEnforced` |
| Config validation reports every problem at once | `TestValidateReportsEveryProblem` |

## Concurrency model

Every shared component is safe for concurrent use, and each one says so in its
doc comment. The rules:

- `session.Store`, `provider.Registry`, `skill.Registry`, `permission.Policy`
  and `router.Router` guard their state with a `sync.RWMutex`.
- Everything handed out by those types is a **copy**. `Session.Clone` is the
  clearest case: callers get their own turns slice and cannot reach back into
  the store.
- `Skill.Execute` and `Provider.Complete` must be safe for concurrent calls and
  must honour `ctx`. Both are stated in the interface docs.
- The journal serialises writes behind a mutex and flushes each line. Durability
  beats throughput for an audit log.

`go test -race ./...` runs the whole suite under the race detector, and several
packages carry explicit concurrency tests that hammer their type from eight
goroutines at once.

## Error strategy

- Sentinel errors (`ErrDenied`, `ErrNotFound`, `ErrEmptyText`) are wrapped with
  `%w` so callers use `errors.Is` rather than string matching.
- `Config.Validate` uses `errors.Join` to report every problem in one pass.
- The HTTP layer maps errors to status codes with an explicit `switch`. A new
  failure mode falls to the `default` case and surfaces as a 500, which is
  noisy on purpose — silent absorption is how error handling rots.

## What is deliberately missing

- **No dependency injection framework.** `runtime.Options` is a struct; `New`
  validates it and names the missing field.
- **No plugin loader.** Skills are compiled in. Dynamic loading is an
  attack surface that a project at this stage has not earned.
- **No embeddings or model-based routing.** Keyword scoring is worse at
  understanding and far better at being explained, replayed and tested.
- **No third-party libraries.** See [adr/0002](adr/0002-standard-library-only.md).

## Extension points

| To add | Implement | Register in |
| --- | --- | --- |
| A skill | `skill.Skill` | `skill.RegisterBuiltins` |
| A model backend | `provider.Provider` | `buildRuntime` in `cmd/orreryd` |
| A capability | a `permission.Capability` constant | `permission.Known` |
| A transport | anything calling `runtime.Handle` | `cmd/orreryd` |

Adding a transport is the interesting one: `httpapi` is roughly 200 lines of
decode-and-encode with no logic in it, so a Unix socket or gRPC front end is a
parallel package rather than a refactor.
