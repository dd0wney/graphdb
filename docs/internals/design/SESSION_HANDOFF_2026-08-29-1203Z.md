# Session handoff — 2026-08-29 12:03 UTC

**Date**: 2026-08-29 (second session of the day; 0 PRs merged, 4 opened, two plans executed end to end)
**Outgoing model**: Claude Opus 5 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## TL;DR

Four pull requests are open and none is merged. They form a **three-deep stack**
plus one independent document. The stack's middle and top get **almost no CI**,
because every workflow except `benchmark.yml` filters `pull_request` to
`[main, develop]` — so a green tick on #512 and #513 means far less than it looks
like. Nine defects were found and left unfixed, deliberately.

## What is done this session

No pull request merged. Four opened.

| PR | Title | Notes |
|---|---|---|
| #511 | `refactor(storage)`: a reason code for the mmap read path (PR A of 3) | Base `main`. **10/10 green.** Inert by construction: 43 call sites move from `(T, bool)` to `(T, error)` and every public boundary collapses the error back. Carries a correction comment about the DCCC gate, see §6 |
| #512 | `fix(storage)`: a damaged record stops reading as a missing one (PR B of 3) | Base `fix/storage-absent-versus-refused`. Seven commits. The actual fix, plus the tenant guard that closes the side-channel it opened, plus `CheckInvariants` reporting damage, plus an OOM sweep that now asserts rather than logs |
| #513 | `test(api)`: a cross-principal equivalence sweep (PR C of 3) | Base `fix/storage-unreadable-single-record`. Ten commits. Eight rows asserting a stranger cannot distinguish another tenant's resource from one that never existed, intact and under fault. Adds `pkg/storage/storagetest` |
| #514 | `docs(design)`: what PostgreSQL's cluster harness has that graphdb does not | Base `main`. **10/10 green.** Independent of the stack |

## Current state

- **`origin/main`**: `e322cbf`, unchanged this session.
- **Open branches**: `docs/cluster-test-harness`, `fix/storage-absent-versus-refused`,
  `fix/storage-unreadable-single-record`, `test/cross-principal-equivalence`,
  `main-prerebase-backup` (pre-existing, not this session's).
- **Uncommitted**: none, in either the primary checkout or the worktrees.
- **Worktrees**: two under `$SCRATCH` (`equiv-wt`, `ho-wt`) plus `doc-wt`. Remove them when the branches merge.
- **Gates**, run locally at each branch tip: `go build`, `go vet`, `gofmt -s -l`
  clean; `pkg/storage` and the five dependent packages pass; `-race` clean on the
  mmap and snapshot paths; `contract-guard` 11 contracts / 15 tests;
  `dccc` 587/634 with the caveat in §6; every `*-selftest` run before its gate.

### The CI shortfall, which is the most important line in this document

`test.yml` and `lint.yml` both carry:

```yaml
pull_request:
  branches: [ main, develop ]
```

So a pull request whose BASE is not `main` or `develop` runs no Tests, no Lint,
no Coverage, no Security Scan. Only `benchmark.yml` is unrestricted.

Measured: #511 and #514 (base `main`) each got **10 checks**. #512 got **1**.
#513 got **0**.

**Do not read #512 or #513 as CI-verified.** They are locally verified only.
Retargeting each to `main` is what makes their CI real, and that has to happen
before merge anyway (see §7).

## What is next

1. **Merge the stack, in order, without losing a dependent.** See §7.
2. **The nine findings below.** None is fixed. They are ranked in §5.1.
3. **PR C's remaining scope** — `POST /traverse` and `POST /shortest-path` read
   caller-supplied node IDs and are outside the sweep's table. Both map every
   error to 404 today, so no leak is live.
4. **The absent-versus-refused track's PR D**, still unplanned: the seven
   enumeration signatures, `pkg/storage/interface.go`, `BTreeGraphStorage`, and
   26 production call sites in five packages. The user's global `CLAUDE.md` says
   propose an interface change rather than implement it.

### 5.1 Nine findings, none fixed

| # | Where | What | Why it matters |
|---|---|---|---|
| 1 | `pkg/storage/mmap_snapshot_persist.go:20` | `snapshotMmapLocked` DROPS a damaged node record where the edge path REFUSES | `Close()` calls `Snapshot`, so one clean shutdown erases the directory entry and every diagnostic #512 adds goes quiet. **Put this first** |
| 2 | `pkg/wal/wal.go:230` | `isResourceError` classifies by `errors.As(err, &pathErr)`; the vfs driver returns a bare wrapped error | A device error during recovery reads as a torn tail, `currentLSN` is derived from the last entry, the next `Append` reuses an LSN, and the NEXT recovery drops that record. Fix: return `&os.PathError{...}` from the driver, plus a predicate table |
| 3 | `scripts/dccc.sh:80-89` | The resolver takes the FIRST match; row `C4` names a symbol declared twice | DCCC measures `BTreeGraphStorage.CreateNodeWithTenant`, whose body has no vector code, for a row whose note is "Node creation drives vector index maintenance". The floor of 14 is calibrated against the wrong body, so repairing the resolution fires the floor as a shrinkage. Prefer REFUSING an ambiguous symbol |
| 4 | `pkg/storage/node_operations.go:154` | `CreateNodeWithUniquePropertyForTenant` skips an unreadable record in its uniqueness scan | The check passes and a duplicate lands. This is the atomic-claim primitive behind CC10 and graphdb-coord |
| 5 | `pkg/api/handlers_nodes.go:185`, `handlers_edges.go:243` | `GET` collapses every error to 404 | The OWNER never sees the diagnostic #512 exists to provide, on the most common read endpoint |
| 6 | `pkg/vfs/vfstest/sweep_role.go:98` | `opConstName` holds 11 string literals with no test | Its output is a paste-ready regression test. A wrong name yields a pin that compiles, passes, and names a different operation. Mutation testing CANNOT reach string literals |
| 7 | `pkg/vfs/vfstest/sweep_role.go:58` | A shortening trace is invisible to the stability check | `min(n-1, len(trace), len(prev))` compares less rather than refusing. The correct condition is `len(trace) < n-1` at the TERMINATING pass |
| 8 | `openMmapSnapshotWithFS` | Eight error paths call `release()` and no test proves any fires | `Munmap` is not garbage-collected; a missed path leaks address space until exit |
| 9 | `~/.claude/skills/ste-check/check.py` | Rule 3.4 misses past-perfect that is wrapped, negated, or adverb-separated | Reproduced: `had\nwritten`, `had not visited`, `had clearly written` all give 0 findings; `had written` gives 1. The LENGTH rules were repaired for wrapping and 3.4 was not |

### 5.2 Gaps this session surfaced

- `pkg/vfs/map_other.go` (`//go:build !unix`) compiles and **is never executed**.
  No CI leg is Windows, and the only cross-compile is goreleaser at release time.
- `pkg/cluster` dials TCP at three sites and no test in the package imports
  `net`, `net/http` or `os/exec`. See #514.
- A response header is an observable the equivalence sweep excludes. Nothing
  leaks today because `respondError` sets none.

## Stale assumptions to retire

1. **`docs/NEXT_STEPS_2026-06-18.md` item 4** frames absent-versus-refused as a
   memory-pressure defect. `alloc.Bytes` cannot fail in production — with no
   allocator installed it is a plain `make` (`pkg/alloc/alloc.go:75-77`). The
   production cause is a damaged record body, which the snapshot CRC does not
   cover. #512 corrects this in the planning doc; it is not on `main` until merge.
2. **The same item says "8 internal call sites"**. The true count is 43 for the
   resolve/materialize helpers, and **seven** public signatures must move, not
   four. Three cross `pkg/storage/interface.go` with two implementations.
3. **`COUPLING_AND_INTERFERENCE.md`'s DCCC baseline**. The number is 587/634 and
   one of its 17 rows measures the wrong function. See finding 3.
4. **Any belief that a green tick on a stacked PR means CI passed.** See §4.
5. **The user's auto-memory `mmap-json-mode-switch-loses-data`** is unchanged by
   this session and still needs the rewrite the previous handoff described.

## Open questions for the user

1. **Contract row numbering.** #512 claims `CC11`, #513 claims `CC12`. Each was
   numbered against a tip the other does not carry. Confirm at merge time.
2. **Finding 4 is a consumer-contract risk.** `CreateNodeWithUniquePropertyForTenant`
   backs CC10 and graphdb-coord. Fix before or after the stack merges?
3. **`pkg/storage/storagetest` is new public surface** on a library others import.
   Three symbols, judged sound by review, but it is a decision not a detail.

## Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## How to use this handoff

1. Read this, then check whether #511 to #514 merged and in what order.
2. If any merged with `--delete-branch`, check whether its dependents survived.
3. Then `docs/NEXT_STEPS_2026-06-18.md`, then `CLAUDE.md` § "Orient first".
4. If picking up finding 1, read `pkg/storage/mmap_snapshot_persist.go:20-55`
   first — the edge branch already does the right thing and the node branch
   does not.
