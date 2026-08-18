# ADR 0004: An append-only journal as the record of truth

- **Status:** Accepted
- **Date:** 2026-08-18

## Context

When an assistant answers unexpectedly, the useful question is *why*: which
skill matched, what score, was anything denied, did the provider fail. Ordinary
logs answer that badly — they are unstructured, rotated, and written for humans
reading one line at a time.

## Decision

Every externally visible decision is written to an append-only,
newline-delimited JSON file: one object per line, monotonically sequenced,
never rewritten in place.

Event kinds: `request`, `routed`, `skill`, `provider`, `reply`, `denied`,
`error`, `lifecycle`.

Three properties are load-bearing:

- **Sequence numbers resume across restarts.** `OpenFile` scans the existing
  file for the highest sequence rather than restarting at one, so ordering
  survives a reboot.
- **Each line is flushed on write.** A journal that loses its tail in a crash is
  worthless as an audit trail, so durability beats throughput.
- **A journal failure never fails a request.** `runtime.record` logs the error
  and continues. Losing an audit line is bad; dropping a user's answer is worse.

## Consequences

**Good.** The full path of any request is reconstructable. `jq` is a sufficient
analysis tool. The format is append-only, so it is safe to tail, ship or replay,
and Era 4 can rebuild state from it after a crash.

**Bad.** It grows without bound — rotation and compaction are Era 1 work. Each
write costs a flush, which caps throughput. Request text is stored in plaintext,
so `data_dir` holds everything anyone has said to the daemon and must be
protected accordingly.

**Interface, not implementation.** `journal.Journal` has two implementations:
`File` for real use and `Memory` for tests, which lets every runtime test assert
on the exact sequence of recorded events without touching disk.

## Alternatives considered

- **SQLite.** Better querying, but a CGo dependency or a pure-Go reimplementation,
  and it violates ADR 0002 for a benefit this project cannot yet use.
- **Structured logs only.** No sequencing, no replay, and rotation would silently
  destroy the audit trail.
- **In-memory ring buffer.** Loses everything on restart, which is precisely when
  the history is most wanted.
