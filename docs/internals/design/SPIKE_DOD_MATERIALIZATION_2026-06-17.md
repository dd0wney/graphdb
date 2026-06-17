# Spike: Data-Oriented Design for the binary storage format (graphdb ask #1, post-Stage-2b)

**Date:** 2026-06-17
**Status:** Investigation complete. Lever 1 (Clone-skip) → implemented as Stage 2c (this branch). Levers 2–3 → documented future candidates, gated on an open question.
**Predecessor:** `SPIKE_REOPEN_COST_2026-06-16.md` (Stages 1/2a/2b). Throwaway spike code lived in `/tmp/dod_spike`, deleted after measuring.

## The question

Stage 2b took mmap reopen *open* to ~8ms and the membership-index lookup to ~11ms (937k IDs).
The remaining cost on a full `GetAllNodesForTenant`-after-reopen is **node materialization**:
decoding + heap-allocating ~937k `*Node` structs, each with a `map[string]Value` property bag.
Can a Data-Oriented Design (columnar / Structure-of-Arrays, lazy property bags) layout attack
that — and where exactly does the cost live?

## What was measured

A standalone Go program at the consumer's **936,908 nodes** (~3 properties each; 104.8 MB record
region in the repo's mmap record format), running the `resolveNodeRefLocked → decode → Clone`
path that `GetAllNodesForTenant` executes. `runtime.MemStats` for allocation, wall time, two runs
(stable):

| Variant | Wall | Alloc | Mallocs | GC |
|---|---|---|---|---|
| **A. AoS decode + Clone** (current path) | **~640ms** | 1151 MB | 17.8M | 9 |
| A2. AoS decode only (no Clone) | ~335ms | 596 MB | 11.2M | 4 |
| **B. Lazy decode (no property map)** | **~91ms** | 112 MB | 3.7M | 0 |
| **C. Pure columnar sum** (one int column, no struct) | **~0ms** | 0 MB | 0 | 0 |
| D. Lazy decode + build all maps | ~290ms | 161 MB | 9.4M | 1 |

## Findings

- **H1 — the `map[string]Value` property bag dominates: CONFIRMED.** B (no map) 91ms vs A2 (with
  map) 335ms ⇒ the per-node map construction is **~73% of decode** (11.2M → 3.7M mallocs when
  dropped). The map + `Value` boxing is the allocator.
- **H2 — returning `*Node{map}` reallocates regardless of on-disk layout: CONFIRMED.** D (lazy
  decode, then build every map) 290ms ≈ A2 335ms. Deferring doesn't help if you ultimately build
  the map for every node; the win is realized **only when the map is never built** (B).
- **H3 — pure columnar touch-all is ~free: CONFIRMED (beyond expectation).** C ~0ms / 0 alloc vs
  A ~640ms — SoA is essentially free for single-field scans, but only for queries that don't need
  `*Node` objects.
- **Surprise — the `Clone()` after decode is redundant and doubles the cost** (335ms decode →
  640ms with Clone). The mmap read path *already* produces a fresh, heap-owned copy
  (`decodeNodeRecordAt` copy-on-reads); the unconditional `Clone()` in enumeration re-allocates the
  whole map + every `Value.Data` a second time. For mmap-base-materialized nodes the Clone is pure
  waste — a **~2× win with no format change**.

## Levers (cost/benefit order)

1. **Skip the redundant Clone on the mmap-base read path (Stage 2c — DONE on this branch).**
   ~2× materialization (640 → ~335ms), no format/API change. `resolveNodeRefLocked` returns either
   a live overlay pointer (must Clone) or a fresh mmap copy (already owned); an `…OwnedLocked`
   variant reports which, so enumeration callers Clone only the shared pointer. Low risk; correctness
   hinges on the owned/shared distinction (overlay pointer must never be handed out uncloned).
2. **Lazy property bag (DoD-flavored, medium cost — future).** Don't build `map[string]Value` until
   a property is read ⇒ 3.6× decode (335 → 91ms) for property-agnostic enumeration. But
   `Node.Properties` is a public map field accessed directly across the codebase (query engine,
   graphql, algorithms); making it lazy means replacing direct `.Properties` access with an accessor
   or hiding a materializer behind the field. The blast radius (incl. the open-core consumer
   contract) is the real question — needs its own design spike before committing.
3. **Full columnar / SoA storage (largest scan win, highest cost — pivot).** ~free for single-column
   analytical scans, but only behind an API that exposes columns/handles instead of `[]*Node`. That
   is a *separate columnar query path*, not a swap of the existing storage format — out of scope for
   the current "reopen + enumerate returning nodes" surface.

## Recommendation

**Proceed with Lever 1 now** (cheap, contained, real ~2× on the materialization path that exists
regardless of mode). **Hold Levers 2–3** pending the open question below.

### Open question (gates Levers 2–3) — for the user / consumer

Is full-graph `GetAllNodesForTenant`-on-reopen actually a consumer hot path, or is the realistic
access pattern bounded / by-label (already ~0 after Stage 2b)? Levers 2–3 change or bypass the
widely-used `*Node`/`Properties` public type — only worth that blast radius if full-graph
materialization on reopen is confirmed-hot. Until then they stay documented, not built.
