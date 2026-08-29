# Design: a cross-principal observable equivalence sweep

**Date**: 2026-08-29
**Status**: approved, not implemented
**Prompted by**: the tenant existence side-channel opened by
`docs/superpowers/specs/2026-08-29-absent-versus-refused-design.md` and closed in
PR B, which an automated security review caught and no test would have.

## The invariant, stated

graphdb's tenant rule is written in prose in `CLAUDE.md` and in a dozen code
comments:

> Cross-tenant lookups return `ErrNodeNotFound` (NOT a distinct error) to avoid
> existence-leak side channels.

Stated so a machine can check it:

    for every operation OP, every resource R owned by tenant A, and every
    tenant B != A:

        observable( OP(R) as B )  ==  observable( OP(nonexistent) as B )

Nothing enforces this today. Six `*TenantIsolation` tests exist in `pkg/api`, each
hand-written for one feature, each asserting that B cannot READ A's data. None
asserts that B cannot DISTINGUISH A's data from data that never existed. Those are
different properties, and the second is the one that leaks.

## Why this is worth building here

On 2026-08-29 a change to `pkg/storage` made a damaged record return
`ErrRecordUnreadable` instead of `ErrNodeNotFound`. That was correct and closed a
real defect. It also meant a stranger got HTTP 500 where a stranger asking for a
nonexistent ID got 404 — an existence oracle.

Three facts about how that was found and fixed:

1. **No test caught it.** An automated security review did.
2. **The fix needed a manual proof of completeness.** A reviewer enumerated every
   tenant-scoped entry point in `pkg/storage` and every caller in `pkg/api`,
   `pkg/graphql` and `pkg/query`, by hand, to establish that no other path leaked.
3. **One entry point was missed on the first pass** — `verifyNodeExistsForTenant`,
   reachable through `POST /edges` with a caller-supplied node ID. The implementer
   added it on its own judgement, and it shipped with no test until a review round.

A sweep replaces steps 2 and 3 with a table.

## The observable, and where it is measured

**The observable is the pair (HTTP status code, response body).**

It must be measured at the HTTP boundary, not at the Go API. The side-channel
lived entirely in the mapping from a Go error to a status code: two errors that
both read as "not found" in `pkg/storage` became 404 and 500 in `pkg/api`. An
equivalence assertion at the storage layer would have passed.

One measured fact narrows the work. `sanitizeError` (`pkg/api/handler_helper.go:17`)
logs the real error and returns `fmt.Sprintf("%s failed", operation)`, so every 500
body is a constant per endpoint and leaks nothing. **The status code is the
channel.** The body still enters the comparison, because three distinct 404 strings
exist ("Node not found", "Edge not found", "Source or target node not found") and
a future change could make one of them conditional.

Explicitly NOT in the observable: response time. See Limits.

## The operation table

Seven rows, covering every endpoint that names a resource the caller may not own:

| # | Method | Path | ID source |
|---|---|---|---|
| 1 | GET | `/nodes/{id}` | path |
| 2 | PUT | `/nodes/{id}` | path |
| 3 | DELETE | `/nodes/{id}` | path |
| 4 | GET | `/edges/{id}` | path |
| 5 | PUT | `/edges/{id}` | path |
| 6 | DELETE | `/edges/{id}` | path |
| 7 | POST | `/edges` | body: `from_node_id` |
| 8 | POST | `/edges` | body: `to_node_id` |

Rows 7 and 8 are the same endpoint in each position. They are separate because
`CreateEdgeWithTenant` verifies the source and the target in SEPARATE calls
(`edge_operations.go:61` and `:64`), so a guard on one is not a guard on the
other. Row 7 is also the row a hand-written suite forgets, and it is the one that
leaked on 2026-08-29.

Corrected 2026-08-29: an earlier draft of this table said seven rows and
described the eighth only in prose.

## Two phases, and the second is the point

**Phase 1 — the clean sweep.** Each row, against an intact store. Asserts that a
stranger's observable for a resource owned by another tenant equals a stranger's
observable for an ID that never existed.

Phase 1 passes on `main` today. Its red-first proof is a real commit in history:
run it against `9a5feb0`, which is post-Task-7 and pre-Task-7b, and rows 1, 2, 3,
4, 5, 6 and 7 fail. That is the sweep proving it has teeth against a defect that
genuinely existed.

**Phase 2 — the sweep under fault.** Each row again, with the target record
damaged on disk so it will not decode. This is the composition that matters: the
2026-08-29 leak was invisible to either half alone. A cross-tenant test with an
intact record passes; a fault test with one principal never compares.

| Test | Would it have caught the leak? |
|---|---|
| Cross-tenant test, intact store | No — the error paths do not diverge |
| Fault injection, one principal | No — it never compares principals |
| Both, composed | Yes |

## What this is not

- **Not an authorisation test.** It says nothing about whether B *should* be
  allowed to read A's data. It says that when B is refused, the refusal carries no
  information about whether the thing exists.
- **Not a replacement for the six `*TenantIsolation` tests.** They assert
  non-disclosure of content. This asserts non-disclosure of existence.
- **Not a `pkg/storage` test.** The channel is the error-to-status mapping.

## Limits, stated rather than discovered later

1. **Timing is an observable and this covers none of it.** A cross-tenant lookup
   that returns faster than a genuine miss is a leak this cannot see.
2. **It proves indistinguishability for the pairs enumerated, not for all inputs.**
   Seven rows and two tenants is a table, not a proof.
3. **It proves conformance to the model, not that the model is right.** If the
   security rule itself is wrong, both sides agree and the sweep passes. Same
   limit as any two-sided comparison.
4. **`GET /nodes/{id}` answers 404 for every error** (`handlers_nodes.go:185`), so
   rows 1 and 4 are trivially equal today and will stay green through changes that
   would break the others. They are cheap and they are weak. Keep them, and do not
   read their passing as coverage of the mapping.

## Relationship to `github.com/dd0wney/fault`

A peer session is building a Go fault-injection library aiming at a standard
library proposal. This sweep is the composition partner for it, and it does NOT
belong in that library: three exported symbols aiming at the standard library have
no business carrying an authorisation concept.

If the sweep earns its place here, that is the evidence for proposing it as a
companion package. Not before.
