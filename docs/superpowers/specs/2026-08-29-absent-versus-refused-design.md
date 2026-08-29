# Design: absent versus refused — a reason code for the mmap read path

**Date**: 2026-08-29
**Status**: approved, not implemented
**Planning doc**: `docs/NEXT_STEPS_2026-06-18.md` § "Not started", item 4
**Origin**: #507's OOM sweep (`pkg/storage/mmap_oom_test.go`)

## Context

`mmapSnapshot.getNode(id) (*Node, bool)` (`pkg/storage/mmap_snapshot_reader.go:188`)
returns one bool for three different causes:

| Cause | Reachable on `main` today | Correct answer |
|---|---|---|
| No directory entry at that ID | yes | `ErrNodeNotFound` |
| Record body did not decode | **yes** | an error |
| `alloc.Bytes` refused a length-driven buffer | test injection only | an error |

The bool travels up through six loader helpers and reaches the public boundary,
where it becomes `ErrNodeNotFound`. A caller cannot tell "there is no such node"
from "there is a node and this store could not produce it".

### The planning doc's framing is too narrow

Item 4 scopes this to memory pressure, citing
`TestSnapshotReadPathUnderAllocationFailure` (1 of 3 nodes vanishing under
`FailOnce`, 2 of 3 under `FailAllFrom`). That framing understates the defect,
because `alloc.Bytes` cannot fail in production: with no allocator installed it
is a plain `make` and the error is always nil (`pkg/alloc/alloc.go:75-77`).

The **second** row is the production defect. The snapshot CRC covers the header,
the node/edge/adjacency/membership directories and the metadata blob. It does
**not** cover record bodies — deliberately, so that open need not page in the
whole file (`pkg/storage/mmap_snapshot_format.go:181-188`, and the block comment
at `:222`). A record damaged by bit rot, a partial write, a truncated copy or a
hostile file survives open, fails `decodeNodeRecordAt`, and reads as an absent
node. `FuzzMmapSnapshotCorruption` found exactly this class before any fuzzing.

Cheap reopen moved integrity checking from open time to decode time. Decode time
then needs an error channel, and it does not have one. That is the whole defect.

### Why this matters more than the OOM story

`buildNodeListFromIDs` (`pkg/storage/query_operations.go:5`) documents itself as
"Skips any node IDs that don't exist in storage". Under record damage that
comment is the bug report: the skip is silent and the caller sees a short list
with no signal.

## Error taxonomy

One new exported sentinel in `pkg/storage/errors.go`:

```go
// ErrRecordUnreadable means a record exists but could not be produced. It is
// never a statement about existence: the record's ID is in the directory.
// Unwrap for the cause — errRecordDamaged, or alloc.ErrNoMemory.
ErrRecordUnreadable = errors.New("record unreadable")
```

and one unexported cause:

```go
errRecordDamaged = errors.New("record did not decode")
```

Construction sites wrap with `%w` so both discriminations work off one value:

- `errors.Is(err, storage.ErrRecordUnreadable)` — the class.
- `errors.Is(err, alloc.ErrNoMemory)` — refusal, not damage.

**Rejected**: two independent sentinels (`ErrRecordCorrupt` plus a re-exported
`ErrNoMemory`). It mirrors SQLite's `SQLITE_CORRUPT`/`SQLITE_NOMEM` split more
literally, but it forces a caller to check two values to ask one question.

## Internal helper shape

The six helpers in `pkg/storage/mmap_snapshot_loader.go` move from `(T, bool)`
to `(T, error)`, where **absent is `ErrNodeNotFound` / `ErrEdgeNotFound`**, not a
nil-nil pair:

```go
func (gs *GraphStorage) resolveNodeRefLocked(id uint64) (*Node, error)
func (gs *GraphStorage) resolveEdgeRefLocked(id uint64) (*Edge, error)
func (gs *GraphStorage) resolveNodeRefOwnedLocked(id uint64) (*Node, bool, error) // bool is `owned`
func (gs *GraphStorage) resolveEdgeRefOwnedLocked(id uint64) (*Edge, bool, error)
func (gs *GraphStorage) materializeNodeLocked(id uint64) (*Node, error)
func (gs *GraphStorage) materializeEdgeLocked(id uint64) (*Edge, error)
```

The layers below move with them:

```go
func decodeNodeRecordAt(buf []byte, off int64) (*Node, error)
func decodeEdgeRecordAt(buf []byte, off int64) (*Edge, error)
func (m *mmapSnapshot) getNode(id uint64) (*Node, error)
func (m *mmapSnapshot) getEdge(id uint64) (*Edge, error)
```

43 non-test call sites read the six helpers. Most shrink, because
`if !exists { return nil, ErrNodeNotFound }` becomes `if err != nil { return nil, err }`.

**Why not `(T, bool, error)`**: it is the smaller conceptual step, and that is
the objection. A call site can read `ok` and drop `err` — the exact defect under
repair. Making absence an error value puts the compiler on the change's side.

**Cost**: `errors.Is` on the read path replaces a bool test. The same path clones
a node, so the comparison does not register. Measure it anyway (see Risks).

## Multi-record read policy: abort, everywhere

One unreadable record fails the whole read. This applies uniformly to
`GetAllNodesForTenant`, the four `*PageForTenant` methods, `buildNodeListFromIDs`,
`buildEdgeListFromIDs`, and the adjacency walks in `node_adjacency.go`.

**Rationale**: one rule, no exceptions to remember, and a short result can never
be mistaken for a complete one. That last property is the whole point.

**Accepted cost — an availability regression.** Today one damaged record vanishes
and the store keeps serving. After this change it fails that tenant's
enumeration, so an operator loses the ability to list what survived. This is a
deliberate trade of availability for honesty, and it is reversible later by the
follow-up below. `CheckInvariants` (`pkg/storage/invariants.go`) remains available
as the diagnostic path, so damage is still locatable.

**Rejected — "skip and report"**: return what decoded plus an error naming the
count. It keeps availability, but a caller that reads only the slice is back at
the defect, so the guarantee would rest on caller discipline.

**Rejected — "split by shape"**: abort for a lookup of named IDs, skip for an
open enumeration. Defensible on the merits — the caller of an enumeration asked
for what is there — but it puts a special case in the interface, and a reader
must then remember which method is which.

**Follow-up, not built here**: an explicit `SkipUnreadable` read option, or a
repair-oriented enumerator, restoring availability as an opt-in that says what it
is doing.

## The tenant side-channel: accepted and documented

A damaged record's tenant string is inside the record, so the failure necessarily
precedes the tenant check. `GetNodeForTenant(id, "acme")` returning
`ErrRecordUnreadable` therefore reveals that the node directory holds an entry at
that ID.

This puts one hole in the existing unified-error rule (`node_operations.go:344-347`,
"a distinct ErrCrossTenant would let an attacker enumerate ... via response-shape
inference"). The hole opens only when the file is damaged or memory is refused,
and it reveals directory occupancy, not tenant membership.

**Rejected mitigation**: consult the per-tenant membership section first and
answer `ErrNodeNotFound` when the ID is not in the tenant's set. It closes the
leak completely and costs a membership lookup (~11ms at 937k nodes) on the
single-node read path, which is the cost cheap reopen exists to avoid.

The decision is recorded as a comment beside the existing rule, not silently.

## Blast radius outside `pkg/storage`

| Consumer | Change |
|---|---|
| `pkg/api` single-record handlers | **none.** They already branch `errors.Is(err, storage.ErrNodeNotFound)` → 404, default → 500 (`handlers_nodes.go:222`, `handlers_edges.go:208`). A new error class routes to 500 with no edit |
| `pkg/api` list handlers, 4 call sites | accept and map the new `error` return (`handlers_nodes.go:92,94`, `handlers_edges.go:146,148`) |
| `clients/go`, `workers/graphdb-client` | none. They speak HTTP, not the Go interface |
| `pkg/graphql` (14 sites), `pkg/query` (6), `pkg/api` (4), `pkg/search` (1), `pkg/algorithms` (1) | **26 production sites** must accept an error, because of the three enumerators below |

**Seven public signatures must move, not four.** Corrected 2026-08-29 after
counting: the first draft of this spec claimed four.

| Method | File | External production callers |
|---|---|---|
| `NodesPageForTenant` | `pagination.go:69` | 1 (`pkg/api`) |
| `NodesByLabelPageForTenant` | `pagination.go:97` | 1 (`pkg/api`) |
| `EdgesPageForTenant` | `pagination.go:127` | 1 (`pkg/api`) |
| `EdgesByTypePageForTenant` | `pagination.go:155` | 1 (`pkg/api`) |
| `GetNodesByLabelForTenant` | `tenant_operations.go:146` | 18 |
| `GetAllNodesForTenant` | `tenant_operations.go:212` | 4 |
| `GetAllEdgesForTenant` | `tenant_operations.go:246` | 4 |

The last three are **declared in `pkg/storage/interface.go:43,44,50` and
implemented twice** — `GraphStorage` and `BTreeGraphStorage`
(`btree_storage.go:124,150,262`). The user's global `CLAUDE.md` says an
interface change gets proposed, not implemented unilaterally. So they move in
their own PR, separate from the single-record work.

## Track structure: three PRs

Corrected 2026-08-29. The first draft said two. The enumerator count above
forced the split.

**PR A — the mechanism, provably inert.** The decode functions,
`mmapSnapshot.getNode`/`getEdge`, and the six helpers move to `(T, error)`. All 43
call sites update. Every public boundary collapses any error back to today's
answer, so external behaviour does not move. The evidence that it is inert is the
existing suite, unchanged and green. `ErrRecordUnreadable` is defined here but is
not yet reachable from outside the package.

**PR B — the single-record contract.** `GetNode`, `GetNodeForTenant`, `GetEdge`,
`GetEdgeForTenant`, `WithNodeRefForTenant`, `UpdateNode*`, `DeleteNode*` and
`verifyNodeExists*` stop collapsing. **`pkg/api` needs no edit**, because its
handlers already branch `ErrNodeNotFound` → 404 and default → 500. A
`docs/CONSUMER_CONTRACTS.md` row lands. `CheckInvariants` reports a damaged record
as a violation instead of treating it as missing
(`pkg/storage/storage_helpers.go:312` currently skips it by design).

**PR C — the enumeration contract.** The seven bare-return methods gain an
`error`, `pkg/storage/interface.go` changes, `BTreeGraphStorage` follows, and the
26 production call sites in five packages update. Propose the interface change
before starting, per the parallel-agent rule.

PR B is small and high value. PR C is large and mechanical. Splitting them stops
the sharpest defect — a single-record read that lies about existence — from
waiting on a cross-package interface migration.

## Red-first evidence

Per `CLAUDE.md` § "Red-first", each PR lands with a test seen to fail against the
pre-fix code, and the PR body records what it printed.

1. **`TestDamagedRecordIsNotReportedAsMissing`** (new). Write a valid snapshot,
   corrupt one node record body in place, reopen, then assert
   `GetNodeForTenant` returns an error matching `ErrRecordUnreadable`. Today it
   returns `ErrNodeNotFound`. This is the primary gate: it needs no allocator, no
   fuzzing, and no injection seam, so it fails on `main` in the default
   configuration.
2. **`TestSnapshotReadPathUnderAllocationFailure`** (extend). The sweep currently
   asserts safety only and *logs* the incompleteness it measures
   ("worst case %d of %d nodes unreadable under refusal"). Promote that to an
   assertion: every ID the directory lists must yield a correct node or an error.
3. **Negative control** for gate 1: the same test against an uncorrupted
   snapshot must return the node. Absence of the error must be attributable.

## Out of scope (YAGNI)

- The `SkipUnreadable` availability option (follow-up above).
- A memory budget or a soft heap limit. Planning doc item 6 records the absence
  as a choice.
- Retry-and-reclaim before refusing, which SQLite does and graphdb deliberately
  does not.
- The JSON snapshot path, which decodes eagerly at open and has no equivalent
  conflation.

## Risks

1. **Behaviour regression under existing damage.** A deployment already serving a
   damaged snapshot starts failing reads that previously returned short results.
   This is the intended change and it should be called out in the release notes.
2. **Hot-path cost.** `errors.Is` replaces a bool test on the single-node read
   path. Run `bench_concurrent_read_test.go` before and after, and record the
   delta. Attribute it by changing one thing (`CLAUDE.md` § "Common mistakes").
3. **A large mechanical diff hides a semantic change.** PR A's split exists for
   this reason: it must be inert, and any behaviour change inside it is a defect.
4. **`storage_helpers.go:312`'s deliberate skip.** Its comment says the invariant
   checker wants a damaged record to look missing. PR B changes that contract,
   so the invariants doc and `CheckInvariants`' own tests move with it.
5. **`scanNodeFields` / `scanEdgeFields` are dead.** Their doc comment says the
   loader uses them to build the in-memory indexes cheaply
   (`mmap_snapshot_format.go:585,646`). Nothing outside
   `mmap_snapshot_format_test.go` calls them. They are the only bool-returning
   decoders that this work leaves in place, so a later reader will trip on the
   inconsistency. PR A corrects the comment only; deleting them is a separate
   decision.
