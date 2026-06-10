# Design — M-1: purge deleted-tenant data from the WAL (remanence)

**Status:** proposal, awaiting decision (Track S / `AUDIT_security_2026-06-10.md` M-1).
**Companion:** M-2 (the LSA-snapshot half) shipped in #384. This is the WAL half.

## Problem

`DELETE /api/v1/tenants/{id}` cascades the tenant's nodes/edges out of the
in-memory graph and appends `OpDeleteNode`/`OpDeleteEdge` records to the
WAL. But the tenant's **original** `OpCreateNode`/`OpCreateEdge` records —
carrying its full property data (PII) — **remain in the WAL** until the
next snapshot+truncate cycle. On a long-running server that window is
hours+. A filesystem-access reader (or a crash-then-replay) sees the
deleted tenant's data after the API returned 200. This is a GDPR
right-to-erasure gap (the audit's DR-1, converged on by two auditors).

## Why the obvious fix is unsafe

The recovery model (verified in `pkg/storage/persistence_replay.go`):
**load snapshot (base state) → replay the ENTIRE WAL on top.** `Close()`
purges by `Snapshot()` (writes full current state) then `Truncate()`
(full-clears the WAL). That's safe at `Close()` because there are **no
concurrent writers** at shutdown.

Doing the same mid-flight (during a live tenant-delete) is **not** safe:

- `Snapshot()` takes `gs.mu.RLock` and serializes the in-memory state at a
  point-in-time (writes hold `gs.mu.Lock`, so the RLock excludes them).
- `Truncate()` is a **separate** call that full-clears the WAL.
- A write that lands **between** the snapshot's RLock release and the
  truncate (LSN N+1) is in the WAL but **not** in the snapshot. The full
  truncate deletes it → **the write is lost** (not in snapshot, not in
  WAL). Data loss for a concurrent tenant.

This is why M-1 was deferred to design, not shipped with M-2.

## Options

### Option A — `TruncateUpTo(lsn)` checkpoint  ★ recommended
Standard write-ahead-log checkpoint:
1. Under the snapshot's `gs.mu.RLock`, capture `N = wal.GetCurrentLSN()`
   **together with** the serialized in-memory state. Because writers hold
   `gs.mu.Lock`, the RLock guarantees the snapshot reflects exactly the
   writes with LSN ≤ N (no write can interleave).
2. `wal.TruncateUpTo(N)` rewrites the WAL keeping only entries with
   **LSN > N** (the concurrent writes), dropping ≤ N (now captured in the
   new snapshot — including the deleted tenant's creates).
3. Replay model is unchanged: snapshot (state ≤ N) + WAL (entries > N).

**Why no loss:** `TruncateUpTo` takes the WAL's `w.mu.Lock` (same lock
`Append` takes), so during the rewrite no append can interleave; entries
appended before it grabs the lock are LSN > N and get copied to the new
file; entries appended after wait, then land in the new file. Crash safety
reuses the existing `.new` + `os.Rename` atomic-replace pattern that
`Truncate()` already uses.

**Implementation surface (non-trivial — this is the cost):**
- `Snapshot()` must capture/return the boundary LSN consistent with the
  serialized state (small change; `GetCurrentLSN()` already exists on the
  plain WAL).
- `TruncateUpTo(lsn)` implemented for **all three** WAL backends — plain
  (`wal.go`), batched (`batched_wal.go`), compressed (`compressed_wal.go`)
  — each rewrites-keeping-suffix with its own format. (~3 implementations +
  a `GraphStorage.CompactWAL()` orchestrator + `handleDeleteTenant` call.)
- Crash-recovery teeth tests per backend (kill between rename and the
  in-memory LSN reset; assert no loss + no resurrection).

### Option B — compact under `gs.mu.Lock`
Hold the global write lock across snapshot+truncate so nothing interleaves.
Rejected: `Snapshot()` takes `gs.mu.RLock` internally → can't nest under
`Lock` (Go RWMutex isn't reentrant → deadlock) without a lock-free snapshot
core refactor; and it stalls **all** writers for the full snapshot
duration (seconds at large N). Strictly worse than A.

### Option C — interim: documented window + on-delete `Close`-style purge only when safe
Don't add `TruncateUpTo`. Instead: (1) document the remanence window in the
`pkg/compliance` GDPR control; (2) optionally trigger a full snapshot+truncate
**only** when the caller can guarantee quiescence (e.g. a maintenance-mode
flag), else leave purge to the next organic `Close()`. Lowest effort, but
does not immediately erase under live traffic — a weaker GDPR posture.

## Recommendation

**Option A.** It's the correct, no-data-loss checkpoint and the only one
that immediately erases under live traffic. The cost is real (3 WAL-backend
`TruncateUpTo` impls + crash-recovery tests on the durability layer), which
is why it wants explicit sign-off rather than a drive-by edit.

**If the appetite for a durability-layer change is low right now:** ship
**Option C as an interim** (document the window + wire `pkg/compliance`),
and schedule Option A. C is a few hours; A is a focused multi-day task with
careful crash-recovery testing.

## Decision requested

1. **A or C?** (A = the real fix, durability-layer change; C = interim,
   document + defer.)
2. If A: confirm scope includes all three WAL backends (plain/batched/
   compressed), or restrict to the default backend first with the others
   flagged.

This doc is the spike output; no WAL code changed. Pairs with the L-1 edge
TOCTOU and L-4 zombie-tenant items (both deferred to "the M-1 work").
