# ADR 0001: Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-08-18

## Context

Decisions made early in a project are the ones most likely to be questioned
later, and least likely to have their reasoning written down. Six months on,
the code shows *what* was decided but never *what else was considered*, so the
same debate is had again with less information.

## Decision

Record every significant architectural decision as a numbered file in
`docs/adr/`, using the format of this file: context, decision, consequences.

A decision is significant if reversing it would touch more than one package, or
if a reasonable engineer would ask "why is it like this?".

ADRs are immutable once accepted. A decision that changes gets a new ADR that
supersedes the old one, and the old one is marked accordingly. Editing history
defeats the purpose.

## Consequences

- Reviewers can point at an ADR instead of re-arguing a settled question.
- The set of ADRs is a reading order for someone new to the codebase.
- There is a small ongoing cost to writing them, which is the point: it
  discourages significant decisions being made casually.
