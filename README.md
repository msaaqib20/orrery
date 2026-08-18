# orrery

A local-first assistant daemon in Go, with **zero third-party dependencies**.

An orrery is a clockwork model of a solar system: independent parts, one shared
drive, every motion inspectable. That is the design goal here — a small runtime
where routing, permission, execution and history are separate, observable
moving parts rather than one opaque call into a model.

```
$ orreryctl ask "what is 12 * 7"
12 * 7 = 84

[skill via math, 0ms, session 3f2a1c9e8b7d6a5f4e3d2c1b]
```

## Why this exists

Most assistant projects begin as a wrapper around one model API and grow a
runtime by accident. orrery starts from the runtime and treats the model as a
replaceable part. It ships with an offline provider, so a fresh clone runs end
to end with no API key, no network and nothing to download.

## Status

**Alpha.** The architecture is settled and covered by tests; the surface is
still moving. See [docs/ROADMAP.md](docs/ROADMAP.md) for what is planned and
[CHANGELOG.md](CHANGELOG.md) for what has landed.

## Quick start

Requires Go 1.22 or newer. Nothing else.

```bash
git clone https://github.com/msaaqib20/orrery
cd orrery
make test          # the full suite, offline
make build         # binaries into ./bin
./bin/orreryd      # listens on 127.0.0.1:7717
```

In another terminal:

```bash
./bin/orreryctl ping
./bin/orreryctl skills
./bin/orreryctl ask "what time is it"
./bin/orreryctl ask "calculate (17 - 5) / 4"
./bin/orreryctl ask "tell me about herons"     # falls through to the provider
```

Or without the client:

```bash
curl -s localhost:7717/v1/message \
  -H 'content-type: application/json' \
  -d '{"text":"ping"}'
```

## How a request flows

```
POST /v1/message
      |
      v
  [journal: request]
      |
      v
  router.Route(text) ------- no match above threshold -----> provider.Complete
      |                                                            |
   match                                                           |
      |                                                            |
      v                                                            |
  policy.CheckAll(skill, capabilities)                             |
      |                    \                                       |
   allowed               denied --> [journal: denied] --> 403      |
      |                                                            |
      v                                                            |
  skill.Execute ---- declines ---------------------------------->  |
      |                                                            |
   handled                                                         |
      |                                                            |
      +---------------------------> [journal: reply] <-------------+
```

Two properties fall out of that ordering and are enforced by tests:

- **A denied skill never falls through to the provider.** Answering anyway
  would make the policy decorative.
- **A skill that *declines* does fall through.** "what is the capital of Peru"
  routes to `math`, which finds no expression and steps aside.

## Layout

```
cmd/orreryd        the daemon
cmd/orreryctl      a thin client that speaks the same public API
internal/config    defaults, file and env layers, validation
internal/journal   append-only JSONL event log
internal/session   bounded conversation history
internal/permission capability policy, deny by default
internal/provider  completion backend interface + offline echo backend
internal/skill     deterministic handlers and their registry
internal/router    keyword intent matching
internal/runtime   the orchestrator that owns the request lifecycle
internal/httpapi   HTTP transport: decode, delegate, encode
```

Dependencies point one way, from `httpapi` down to `config` and `version`.
Nothing below `runtime` imports anything above it.

## Documentation

| Document | What it covers |
| --- | --- |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Layers, invariants, concurrency model, extension points |
| [docs/API.md](docs/API.md) | Every endpoint, request and error code |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Every setting, its default and its env override |
| [docs/TESTING.md](docs/TESTING.md) | How the suite is organised and what it guarantees |
| [docs/ROADMAP.md](docs/ROADMAP.md) | Planned eras and their exit criteria |
| [docs/adr/](docs/adr/) | Architecture decision records |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Workflow, review expectations, commit style |
| [SECURITY.md](SECURITY.md) | Threat model and how to report a vulnerability |

## Adding a skill

Implement two methods and register it. Nothing else changes.

```go
type Weather struct{}

func (Weather) Descriptor() skill.Descriptor {
    return skill.Descriptor{
        Name:         "weather",
        Summary:      "Reports the current forecast.",
        Patterns:     []string{"weather", "forecast", "will it rain"},
        Capabilities: []permission.Capability{permission.CapNetHTTP},
        Priority:     15,
    }
}

func (Weather) Execute(ctx context.Context, in skill.Input) (skill.Output, error) {
    return skill.Output{Text: "..."}, nil
}
```

Declaring `CapNetHTTP` means the daemon refuses to run it until an operator
grants that capability in the config. The refusal is logged at start-up, not
discovered by a user.

## Adding a provider

Implement `provider.Provider` — `Name()` and `Complete(ctx, Request)` — and
register it. `internal/provider/echo.go` is the reference implementation and is
deliberately short. Honour `ctx`; the runtime imposes its own timeout on top.

## Renaming the module

The module path is a placeholder. To make it yours:

```bash
NEW=github.com/you/yourrepo
grep -rl 'github.com/msaaqib20/orrery' . | xargs sed -i "s|github.com/msaaqib20/orrery|$NEW|g"
go build ./...
```

## Licence

MIT. See [LICENSE](LICENSE).
