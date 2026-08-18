# Roadmap

Each era has an **exit criterion**. Work does not move to the next era until
the current one's criterion is met, because the alternative is a project that
is 60% done in eight places at once.

## Era 0 — Foundation ✅

The skeleton: config, logging, journal, sessions, permissions, providers,
skills, router, runtime, HTTP API, CLI client, full test suite, CI.

*Exit: `make check` passes and a fresh clone answers a request offline.*

## Era 1 — Hardening

No new capability. Fill in the gaps the foundation left.

- Journal rotation and compaction — the log grows without bound today
- Graceful degradation when `data_dir` is unwritable
- Structured error taxonomy shared between transport and runtime
- Fuzz targets for the arithmetic parser and the config decoder
- Benchmarks for `router.Route` and `runtime.Handle`
- A load test that holds a sustained request rate without leaking sessions

*Exit: the fuzzers run clean for an hour and memory is flat under sustained load.*

## Era 2 — Real providers

The echo provider proves the interface. This era proves it was the right one.

- An HTTP-backed provider with retry, backoff and a circuit breaker
- Streaming responses through the interface and out over the API
- Token accounting and per-session budgets
- Provider fallback chains: try A, degrade to B, degrade to echo

*Exit: swapping the backend is a config change, with no code touched above
`internal/provider`.*

## Era 3 — Better routing

Keyword matching is honest but blunt.

- Slot extraction, so a skill receives parsed arguments rather than raw text
- Multi-turn intents that stay open across a clarifying question
- A confidence band where the router asks instead of guessing
- Router evaluation against a fixture corpus, scored on every commit

*Exit: routing accuracy is a number in CI that cannot silently regress.*

## Era 4 — Durable memory

Sessions are a rolling window on purpose. This era adds what should outlive it.

- A retrieval store separate from session history
- Explicit user-visible facts, addable and removable
- Journal replay to rebuild state after a crash
- A retention policy with real deletion, not tombstones

*Exit: the process can be killed mid-request and rebuild consistent state from
the journal.*

## Era 5 — Extension

Only once the core is stable enough to be worth extending.

- A skill manifest format and an out-of-process skill protocol
- Sandboxing for anything holding `shell.exec` or `fs.write`
- Per-skill rate limits and quotas
- A capability audit report from the journal

*Exit: a third-party skill can run without being trusted with the daemon's
address space.*

## Deliberately not planned

- **A GUI.** The API is the product; a front end can live in its own repo.
- **A plugin system before sandboxing.** In that order it is just remote code
  execution with extra steps.
- **Model-based routing before evaluation.** Without a scored corpus there is no
  way to tell an improvement from a regression.
