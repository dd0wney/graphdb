# Design: complete `Transaction` as a durable, tenant-aware Go primitive

**Date**: 2026-06-03
**Status**: approved (brainstorm), implementation pending
**Context**: `pkg/storage` Transaction API (`transaction_types.go`, `transaction_ops.go`, `transaction_commit.go`).

## Problem

`Transaction.Commit` applies buffered changes directly to in-memory structures and **bypasses durability and consistency entirely**:

- **No WAL write** — committed transactions do not survive a crash (not replayed).
- **No tenant indexes** (`addNodeToTenantIndex`) — transaction-created nodes are invisible to every `*ForTenant` read (`GetNodesByLabelForTenant`, `GetAllNodesForTenant`) and uncounted in `tenantStats`.
- **No vector index, no property index, no stats, no observers.**
- `deletedNodes`/`deletedEdges` are buffered fields with no writer method and no apply step — dead.

The Transaction API has **zero non-test callers** (not on the `Storage` interface, not in `pkg/api`/`pkg/graphql`/`cmd`); the production bulk path is `Batch` (`BeginBatch`/`Batch.Commit`, which *is* WAL-durable). This work makes `Transaction` a correct, durable Go primitive so it can be relied on.

## Scope (decided)

- **Role**: internal Go primitive. **No** new HTTP/GraphQL surface.
- **Atomicity**: all-or-none at a single fsync. A crash mid-commit yields all buffered ops or none.
- **Isolation**: last-writer-wins. Commits serialize on `gs.mu`; no conflict detection between concurrent transactions (documented guarantee).
- **Operations**: creates + updates. **Deletes deferred** — remove the dead `deletedNodes`/`deletedEdges` fields; `tx.DeleteNode`/`DeleteEdge` + cascade is a documented follow-up.
- **Consistency**: commit maintains shard maps, global + per-tenant indexes, property indexes, vector indexes, stats, and fires observers — identical to the direct write paths.
- **Tenant model**: tenant-bound transaction.

## Approach (chosen)

### A1 — reuse the locked write helpers (not a second implementation)

Extract the post-ID-allocation body of `createNodeLocked`/`createEdgeLocked` into shared helpers:

- `persistNodeLocked(node *Node) (*wal.Pending-or-entry, []vectorInsertPlan, error)` — shard store, global label index, `addNodeToTenantIndex`, edge-list init, property index insert, vector *plan* (decode under lock), stats, WAL entry. Caller holds `gs.mu`.
- `persistEdgeLocked(edge *Edge)` — the edge analogue (shard store, global type index, `addEdgeToTenantIndex`, adjacency, stats, WAL entry; reuses the A6a tenant-ownership validation for endpoints).

`createNodeLocked` becomes: allocate ID → build node → `persistNodeLocked`. `Transaction.Commit` calls the same `persist*Locked` on its pre-built buffered objects. **One source of truth for "what persisting means"** — structurally prevents the drift that caused the current gaps.

*Rejected A2* (hand-complete `applyCreatedNodes`): duplicates the write logic; the duplication is the root cause of the present gaps and will drift again.

### B1 — atomic batch-WAL primitive on both modes

Add a storage-facing batch write that records N `(opType, data)` entries and makes them durable with a **single fsync**, all-or-none, on both the batched and plain WAL — built on the existing `WAL.AppendBatch` (`batched_wal.go:161`, already "write-all + one flush"). Exposed via an exported WAL entry-input type (the current `AppendBatch` takes the unexported `pendingEntry`).

*Rejected B2* (reuse per-op `enqueueWAL`): atomic only under `BatchedWAL` (entries fill one batch); on the plain default it is N independent fsyncs, so a crash leaves a partial transaction — not a transaction.

## Commit data flow

1. Acquire `gs.mu` (commit serializes here → last-writer-wins).
2. For each buffered create/update: call the shared `persist*Locked` helper, collecting (a) the encoded WAL entry and (b) any vector-insert plans. **No fsync yet.**
3. Release `gs.mu`.
4. **One atomic batch fsync** of all collected WAL entries (B1). Crash before ⇒ none durable (in-memory state died with the process); after ⇒ all durable.
5. Apply vector inserts off-lock (the Track P item-3 pattern).
6. Dispatch observer notifications (auto-embed sees committed nodes).
7. Mark committed.

**Replay** uses the existing per-op `OpCreateNode`/`OpUpdateNode` entries — no new opcode, because the batch is atomic at the fsync boundary (replay sees all or none).

## Tenant model

- Add `BeginTransactionForTenant(tenantID string) (*Transaction, error)`; `tx` carries the tenant.
- `tx.CreateNode` stamps `node.TenantID`; `tx.CreateEdge` validates both endpoints belong to the tx's tenant (reuse `verifyNodeExistsForTenant`, the A6a guard).
- `BeginTransaction()` stays as `BeginTransactionForTenant(default)` — backward-compatible.

## Error handling

- Endpoint-tenant-mismatch or missing-target in a buffered edge/update is detected by a **validation pass over the whole buffer under `gs.mu` before any in-memory mutation**; if it fails, `Commit` returns the error having applied nothing (all-or-none, no rollback needed because nothing was mutated yet).
- WAL batch-fsync error: propagate from `Commit` (durability failure must not be silent — unlike the fire-and-forget single-op fail-soft, a transaction commit that didn't persist must return an error so the caller knows).
- A `Rollback` after buffering simply discards buffers (unchanged).

## Implementation increments (each its own PR, TDD)

- **PR A — extract `persist*Locked` helpers** (pure refactor; `createNodeLocked`/`createEdgeLocked` delegate; existing `pkg/storage` suite + `-race` guard no behavior change). Riskiest (touches hot paths) → lands alone.
- **PR B — atomic batch-WAL primitive** (`WAL` exported batch-entry input + storage wrapper; durability test: write batch, assert all-or-none on replay across both WAL modes).
- **PR C — rewrite `Transaction.Commit`** on top of A + B: tenant-binding (`BeginTransactionForTenant`), `persist*Locked` per buffered op, atomic batch fsync, vector-apply + observer dispatch after unlock; remove dead delete fields. Tests: atomic durability (crash-sim before/after fsync via the batch-durability harness), index consistency (committed nodes visible to `*ForTenant` reads + counted + vector-searchable + observer-fired), tenant scoping + cross-tenant-edge rejection, `-race` concurrent commits.

## Testing strategy

TDD throughout. Mirror `integration_batch_durability_test.go` for the atomic-durability assertions. Pin the index-consistency guarantees that are currently violated (a test that creates via a transaction then asserts `GetNodesByLabelForTenant`/`GetAllNodesForTenant`/`CountNodesForTenant`/vector-search all see it — this fails against today's `Commit`). `-race -count` on concurrent commits.

## Out of scope / follow-ups

- **Deletes in transactions** (`tx.DeleteNode`/`DeleteEdge` + cascade) — deferred.
- **Conflict detection / optimistic concurrency** — explicitly not provided (last-writer-wins).
- **Client-facing transaction API** (HTTP/GraphQL) — not in scope.
- **`Storage`-interface exposure / production wiring** — out of scope; this makes the primitive correct, not yet wired.
