# Spike: snapshot-based replica hydration (cluster-bootstrap primitive)

**Date**: 2026-07-01
**Origin**: follow-on from the coi-screen mmap validation (`SPIKE_COI_SCREEN_VALIDATION_2026-07-01.md`), which measured mmap reopen at ~6 ms vs ~8.2 s (JSON) at ICIJ scale. The observation: *that reopen speed is attractive for spinning up new cluster nodes.*
**Status**: primitive **validated**; forward work is a v2.0 thread ("Beyond single-node").
**Artifact**: `pkg/storage/snapshot_hydration_test.go` → `TestSnapshotHydration_FromShippedFile`.

## The idea

The expensive part of naïve cluster bootstrap is **base-state hydration** — a new node must acquire the graph before it can serve. Today that's rebuild-from-scratch (~11.5 s cold) or JSON reopen (~8.2 s at ~937K). mmap reopen collapses that to milliseconds because the node *maps* a self-contained snapshot (header → nodes → edges → CSR adjacency → membership → metadata) and serves lazily. So: a new read replica could pull `snapshot.mmap` from shared/object storage, map it, and be read-ready in ms.

## What was validated

`TestSnapshotHydration_FromShippedFile` de-risks the load-bearing assumption **before any cluster layer exists**: that a snapshot written by one instance, **copied to a different directory**, maps and serves reads correctly from a **fresh instance** (i.e. the mmap layout is position-independent — no absolute-path or live-process-state coupling). Correctness is gated by the #440 JSON↔mmap equivalence oracle: the hydrated node must enumerate **byte-identically** to the origin's live state.

Results (snapshot copied to a fresh dir, then mapped by a new instance):

| scale | open (map shipped snapshot) | first read served | parity |
|---|---:|---:|---|
| 50K nodes / 70K edges | 451 µs | 2 µs | byte-identical ✅ |
| **936,908 nodes / 1,316,003 edges** | **7.4 ms** | **3 µs** | byte-identical ✅ |

The 3 µs first read confirms genuine lazy-mapping (not a silent rebuild). **"New replica pulls a snapshot and is read-ready in ~7 ms at ~1M-node scale" is demonstrated, not assumed.**

## What it enables

- **Fast read-replica spin-up** — scale read capacity out in seconds, not minutes.
- **Near-instant failover** — a standby that maps the latest snapshot promotes immediately.
- **Cheap rolling restarts / deploys** — each node is read-ready in ms.

## What it does NOT yet prove (the honest boundary)

The primitive is base-state hydration only. Between it and a usable cluster replica sit four gaps:

1. **Freshness / delta-tail.** A snapshot is point-in-time; the 7.4 ms gets a node to *snapshot-time* state. A replica still needs the writes since the snapshot (WAL tailing / a replication stream). **This is the biggest gap** — base hydration without a delta path yields a stale replica.
2. **Distribution / transfer.** 7.4 ms is the *local map* time. The ~450 MB snapshot still has to reach the node over the network / shared storage. The enterprise `r2-backup` plugin already ships snapshots to R2 — a plausible substrate, but the fetch time is separate from the map time.
3. **Encryption-in-mmap.** mmap mode is **plaintext-only + in-memory-adjacency-only** today (`CLAUDE.md` § snapshot format): encrypted stores and `UseDiskBackedEdges` fall back to JSON. A sensitive-data cluster (offshore-leaks-shaped) needs an encrypted-mmap path that doesn't exist yet.
4. **`pkg/cluster` join path.** The cluster code is real (~2,800 LOC) but its production wiring is unverified — *"no sharded write path"* (`CLAUDE.md` § known pitfalls). Fast hydration is a building block for a bootstrap story, not the cluster itself.

Also: mmap is off by default and only single-node-validated (coi-screen). Cluster use is a new context needing its own validation (consistency/staleness bounds, read-your-writes).

## Proposed sequencing

**Near-term (v1.x, additive, low-risk):**
- Give `cmd/import-icij` an mmap opt-in (honor `GRAPHDB_STORAGE_MODE` or a `--mmap` flag) — also unblocks the coi-screen consumer runbook.
- Snapshot ship-and-serve (this spike) — **done**, kept as a correctness gate.
- A "fetch snapshot from R2 → map → serve" path built on the existing `r2-backup` distribution substrate.

**Mid-term:**
- Delta-tail: apply the WAL (or a replication stream) after mapping the snapshot, so a hydrated replica reaches *current* state. Closes gap #1.

**v2.0 ("Beyond single-node"):**
- Wire hydration into `pkg/cluster`'s join path; add encryption-in-mmap for sensitive clusters; define staleness bounds / read-your-writes semantics for snapshot-served replicas.

## Recommendation

Capture this as a v2.0 roadmap thread. The **primitive is validated** — position-independent, byte-identical, ~7 ms at ICIJ scale — so the assumption the cluster-bootstrap story would rest on is sound. The next concrete de-risking step is **the delta-tail (freshness)**, since that's the widest gap between "base hydration" and "usable replica"; everything else (distribution, encryption, cluster wiring) is engineering with known shape.
