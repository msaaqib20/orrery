# ADR 0003: Capability-based permissions, checked before execution

- **Status:** Accepted
- **Date:** 2026-08-18

## Context

Skills do privileged things: read the clock, read conversation history, and
eventually touch the filesystem, the network and the shell. Something has to
decide which are allowed.

Three designs were considered:

1. **Trust everything.** Skills are compiled in, so they are trusted code.
2. **Check inside the skill.** Each skill asks the policy when it needs to.
3. **Declare up front, check before execution.** A skill states its
   capabilities in its descriptor; the runtime checks them before calling it.

## Decision

Option 3. `skill.Descriptor` carries a `Capabilities []permission.Capability`
field. `runtime.callSkill` calls `policy.CheckAll` before `skill.Execute`.

The default posture is deny: `permissions.default_allow` is `false`, and a
capability nobody granted is refused.

## Consequences

**A skill cannot acquire privilege at the wrong moment.** Its needs are static
data available to the router, so authorisation is a routing-level decision.

**Denial is a first-class outcome.** It is journalled as `denied` and returned
as a clean 403, rather than surfacing as an opaque error from inside a handler.

**A denial must not fall through to the provider.** This was the subtle part.
The obvious implementation — treat denial like any other skill failure and let
the provider answer — means a user can route around the policy by rephrasing.
`runtime.callSkill` returns the error, `dispatch` propagates it, and
`TestDeniedSkillIsNotSilentlyReplaced` fails if that ever changes.

**Gaps are visible at start-up.** `warnUngranted` logs every registered skill
the policy would refuse, so a missing grant is found before a user finds it.

**Capabilities are coarse.** `fs.read` is all-or-nothing; there are no path
scopes. That is honest for this stage. Sandboxing is Era 5, and until it exists
the policy is a guard rail against mistakes, not a defence against hostile code.

## Alternatives considered

- **Checking inside the skill.** Puts the security decision in the least
  reviewed code and makes it easy to forget entirely.
- **Capability tokens passed into `Execute`.** More precise, and worth revisiting
  when out-of-process skills arrive. Overbuilt for compiled-in handlers.
