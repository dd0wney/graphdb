# 3. An error return for the seven enumeration methods

Date: 2026-08-30
Status: accepted

## Context

`pkg/storage` has seven public methods that enumerate records and cannot report
a failure:

| Method | File:line | Returns today |
|---|---|---|
| `GetNodesByLabelForTenant` | `tenant_operations.go:146` | `[]*Node` |
| `GetAllNodesForTenant` | `tenant_operations.go:212` | `[]*Node` |
| `GetAllEdgesForTenant` | `tenant_operations.go:247` | `[]*Edge` |
| `NodesPageForTenant` | `pagination.go:69` | `([]*Node, uint64)` |
| `NodesByLabelPageForTenant` | `pagination.go:98` | `([]*Node, uint64)` |
| `EdgesPageForTenant` | `pagination.go:129` | `([]*Edge, uint64)` |
| `EdgesByTypePageForTenant` | `pagination.go:158` | `([]*Edge, uint64)` |

Each walks a membership list and materializes records. When a record does not
decode, each **skips it and continues**. The caller gets a shorter slice and no
signal. An incomplete enumeration is byte-indistinguishable from a complete one.

PR #512 fixed the single-record half of this: `GetNode`, `GetNodeForTenant`,
`GetEdge` and `GetEdgeForTenant` now return `ErrRecordUnreadable` instead of
collapsing a damaged record to `ErrNodeNotFound`. The snapshot CRC covers the
header, the directories and the metadata blob — **not** record bodies — so bit
rot, a partial write, a truncated copy or a hostile file survives open and fails
only at decode. That is the production cause, not memory pressure.

The enumeration half was deferred because it changes a public interface, and
the parallel-agent rule in the user's `CLAUDE.md` says to propose such a change
rather than implement it.

### The second argument, which is stronger than the first

The correctness argument above is the obvious one. There is a better one, raised
by the `github.com/dd0wney/fault` session on 2026-08-30 while reviewing this
proposal:

> A method that skips a record it cannot decode and returns no error is
> invisible to fault injection. A sweep can fail the read of a record, the
> method swallows the failure, the enumeration returns a short list, and every
> count a test could measure stays exactly right. The test suite reports full
> coverage of a path that reports nothing.

That library states the general rule for its own adapters:

> a method that calls Trip but ignores the answer keeps the pass count exactly
> right, so counting passes never proves the injected error is returned: assert
> on the error as well.

So this is not only a correctness change. **With no error return, no fault sweep
can ever reach these paths.** `pkg/vfs/vfstest` can inject a read failure
underneath any of the seven methods today, and no assertion in this repository
can detect that it did. The change is the precondition for testing the code at
all — which matters more here than in most repositories, because the SQLite
comparison programme (`docs/internals/design/SQLITE_TESTING_SCORECARD.md`) is
built on exactly this kind of sweep.

### Measured blast radius

Counted on `b9cb010`, not taken from the previous planning document, which
understated it:

| Surface | Count | Note |
|---|---|---|
| Public methods to change | 7 | table above |
| `pkg/storage/interface.go` lines | 3 | lines 43, 44, 50 — only three of the seven are on the interface |
| Second implementation | 3 | `BTreeGraphStorage` at `btree_storage.go:124,150,262` |
| Production call sites | **35** | graphql 14, api 8, query 6, storage 5, search 1, algorithms 1 |
| Test call sites | **99** | must compile, need not all assert |

`docs/NEXT_STEPS_2026-06-18.md` records "26 production call sites across five
packages". That is wrong on both numbers: `pkg/api` has 8 rather than 4, and
`pkg/storage` itself has 5 internal callers the count omitted. The real figure
is 35 across six packages, plus 99 test sites — 134 edits in total.

## Decision

**Approved on 2026-08-30.**

Give each of the seven methods an `error` return, in the Go convention of a
usable partial result beside a non-nil error:

```go
func (gs *GraphStorage) GetAllNodesForTenant(tenantID string) ([]*Node, error)
func (gs *GraphStorage) NodesPageForTenant(tenantID string, afterID uint64, limit int) ([]*Node, uint64, error)
```

Semantics:

- The slice holds every record that **did** decode. A caller that wants
  best-effort data keeps it.
- The error is non-nil when any record was skipped, wraps `ErrRecordUnreadable`,
  and names the offending IDs.
- The error is nil when the enumeration was complete. That is the whole point:
  nil now *means* complete, and today it means nothing.

Returning a partial result rather than `nil` matters for migration. A call site
that mechanically threads the error through behaves exactly as it does today,
so the change cannot silently drop data during the migration itself.

**`errcheck` is what makes this real.** It runs in CI and it failed PR #494 on
exactly this class of finding. An unchecked error return at any of the 35 sites
fails the lint job, so the migration cannot be completed cosmetically by
assigning to `_`. Each site has to make a decision, and a reviewer can see which
decision it made.

### Migration order

**The signature change is atomic. It cannot be split into independently green
steps, and an earlier draft of this section was wrong to say that it could.**

That draft proposed five PRs: `pkg/storage` internals first, then
`interface.go` and `BTreeGraphStorage`, then the HTTP-facing consumers, then
the internal ones. The first step alone does not compile, so a reader who
follows that order reaches a broken tree before the second step begins. Two
compile-time assertions are the reason:

```
pkg/storage/interface.go:156   var _ Storage = (*GraphStorage)(nil)
pkg/storage/btree_storage.go:665   var _ Storage = (*BTreeGraphStorage)(nil)
```

Three of the seven methods sit on the `Storage` interface (`interface.go`
lines 43, 44 and 50). Change the `*GraphStorage` bodies and the first
assertion fails, because the receiver no longer matches the interface. Change
the interface to match and the second assertion fails, because
`BTreeGraphStorage` still carries the old shape. Change both and every
consumer of the interface fails, because `pkg/algorithms` takes
`storage.Storage` as a parameter in 32 non-test functions and holds one in a
struct field twice, at `pkg/algorithms/view.go:48` and
`pkg/algorithms/view.go:84`:

```
grep -h '^func .*storage\.Storage' pkg/algorithms/*.go | wc -l   # 32
```

So one commit must carry all of it:

- the seven method bodies, plus the 5 in-package callers;
- the three `interface.go` lines and the three `BTreeGraphStorage` methods;
- all 35 production call sites, across six packages;
- all 99 test call sites, which must compile even where they do not assert.

The four page methods are NOT on the interface, so only three of the seven
force the interface edit. That does not help: those three are enough to break
both assertions, and the page methods are reached through the concrete
`*storage.GraphStorage` that `pkg/api`, `pkg/graphql` and `pkg/query` already
hold.

A commit split inside that single PR is still worth having, and is what the
implementation used: the mechanical migration first, the new gate second.
That split is readable. It is not independently landable, and the difference
matters.

**The fault sweep lands separately.** A sweep over the seven methods is purely
additive: it adds test files and changes no signature, so it does not share
the atomicity constraint above. It is the step that proves the change was
worth making — until it exists, the migration buys a signature and not a gate
— but it is a follow-up PR rather than a step inside this one.

## Alternatives Considered

### 1. Do nothing

**For:** zero risk, zero churn, 134 edits avoided. The condition needs a damaged
record, which no consumer has reported.

**Against:** the failure is silent by construction, so "no consumer has reported
it" is not evidence of absence — a short enumeration looks exactly like a small
tenant. `TestSnapshotReadPathUnderAllocationFailure` measured 1 of 3 nodes
vanishing under fail-once and 2 of 3 under fail-all-from. The data says the path
is reachable.

**Rejected.** Also fails the testability argument: a permanent hole in the fault
sweep.

### 2. Log the damage and keep the signature

**For:** no caller migration at all. An operator gets a line in the log.

**Against:** the caller still cannot tell. A query returns a wrong answer and the
process continues as though it were right. A log line is not available to the
code that must decide whether to serve the result, and `pkg/api` cannot turn it
into a status code. Fault injection still cannot assert anything.

**Rejected** — this is the shape the code already has, minus the silence.

### 3. A parallel strict method for each (`GetAllNodesStrictForTenant`)

**For:** no migration; new code opts in; mirrors the existing `Foo` /
`FooForTenant` convention this repository already uses.

**Against:** the `ForTenant` pair exists to migrate *away* from the tenant-blind
form, and it has not finished after many releases. A second axis multiplies
seven methods into fourteen and leaves the unsafe one as the shorter, more
discoverable name. The default stays wrong, which is the actual defect.

**Rejected** — but this is the strongest alternative, and it is the fallback if
the migration proves too large to land safely.

### 4. Return `(nil, err)` on the first damaged record

**For:** simplest semantics; no partial state to reason about.

**Against:** one damaged record makes a whole tenant unreadable, converting a
localized data defect into a total outage for that tenant. It also makes the
migration dangerous: any site that threads the error through without handling it
now returns nothing where it used to return most of the data.

**Rejected** in favour of the partial result.

### 5. A metric or counter for damaged records

**For:** cheap, no signature change, feeds the existing observability surface.

**Against:** solves monitoring, not correctness. The caller still serves a wrong
answer. Worth doing **in addition**, never instead.

**Deferred, not rejected** — a counter alongside the error return is a good
follow-up.

### 6. Panic

**Rejected** without much thought. A damaged record on disk is an expected
failure for a database, and a library that panics on it is unusable in a server.

## Consequences

**Good**

- `nil` starts meaning something. A complete enumeration becomes distinguishable
  from an incomplete one, at every one of the 35 sites.
- The seven methods become reachable by fault injection for the first time.
- `pkg/api` and `pkg/graphql` can return an honest status instead of a short
  list.
- `CheckInvariants` gains a second, independent way to observe damage.

**Bad**

- 134 edits, across six packages, touching a public interface with two
  implementations. This is the largest single API change since the repository
  went to 1.0.
- Every downstream consumer that implements `pkg/storage`'s interface breaks at
  compile time. That is loud rather than silent, which is the right failure
  mode, but it is still a breaking change and needs a major or minor version
  decision.
- The `clients/go` SDK and the TypeScript Workers client may surface the new
  error; neither was audited for this ADR.

**Neutral**

- The change is mechanical at 99 of the test sites, which will dominate the diff
  while carrying almost none of the risk. Reviewers should be pointed at the 35
  production sites and told to ignore the rest.

## References

- PR #512 — the single-record half, `ErrRecordUnreadable`, and the
  `membershipContains` tenant guard.
- PR #513 — the cross-principal equivalence sweep that step 3 must not break.
- PR #521 — the snapshot write path refusing a damaged node record.
- `docs/internals/design/SQLITE_TESTING_SCORECARD.md` row 4 — the open item this
  closes.
- `docs/CONSUMER_CONTRACTS.md` — CC11, CC12.
- `github.com/dd0wney/fault` — the adapter rule quoted above.

## Note on how this design was produced

The blast radius in the previous planning document (26 sites, five packages) was
carried forward without being re-measured. It is wrong. The figures in this ADR
were counted on `b9cb010` with the commands recorded here, so the next reader can
re-run them rather than trust the table:

```
M='GetAllNodesForTenant|GetAllEdgesForTenant|GetNodesByLabelForTenant|NodesPageForTenant|NodesByLabelPageForTenant|EdgesPageForTenant|EdgesByTypePageForTenant'
grep -rnE "\.($M)\(" pkg/ cmd/ clients/ --include='*.go' | grep -v '_test.go' | wc -l
```

The testability argument in the Context section is not this session's. It came
from the `fault` library session, which had extracted `pkg/vfs`/`pkg/vfs/vfstest`
into a standalone library and had already written the general rule down for its
own adapters. It is the argument that should persuade, and it was not the one
this session started with.
