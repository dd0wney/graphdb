# Spike: coi-screen mmap validation — answering decision B-1

**Date**: 2026-07-01
**Task**: `v1.1-coi-screen-validation` (v1.1 "Validate & observe").
**Decision under test**: **B-1** — *is full-graph `GetAllNodesForTenant`-on-reopen a real consumer hot path?* B-1 gates DoD Levers 2–3 and is (with the property-based oracle, #440) a precondition for **v1.2 mmap-default**.
**Harness**: `pkg/storage/reopen_cost_bench_test.go` → `TestReopenCost_CoiScreen` (gated behind `GRAPHDB_REOPEN_BENCH=1`).

## TL;DR

**No — full-graph enumeration is not a hot path for the coi-screen workload.** At ICIJ scale (~937K nodes / ~1.3M edges), the coi hot path is a label-bucket resolve scan plus a *microsecond* adjacency BFS; it never enumerates the graph. mmap reopen is **~1370× faster** than JSON (6 ms vs 8.2 s), and the operations coi actually uses (label lookup + adjacency) are mmap's cheap paths. **mmap-default (v1.2) is safe for this workload.**

## Why this is the right question to measure

The coi-screen consumer (sibling repo `../coi-screen`, embeds graphdb as a library — **not vendored here**, so the true end-to-end binary can't run in this tree) implements conflict-of-interest screening as:

1. **Resolve** — `GetNodesByLabelForTenant(tenant, "Officer"/"Entity")` (label index) + an in-bucket, soundex-blocked linear scan to resolve party names to node IDs.
2. **Connect** — a bounded, interest-type-restricted adjacency BFS via `GetOutgoing/IncomingEdgesForTenant`, capped by `maxHops`/`maxDegree`.

It never calls `GetAllNodesForTenant` / `GetAllNodesAcrossTenants`. So B-1 ("is enumeration-on-reopen hot?") is really: does coi pay the ~479 ms full-enumeration cost that DoD Levers 2–3 target, or does it live entirely on the cheap label+adjacency paths? This spike measures the coi access pattern at scale in both modes and contrasts it with the enumeration path it avoids.

## Method

`TestReopenCost_CoiScreen` builds an **ICIJ-shaped** store (Entity/Officer/Intermediary/Address nodes; `officer_of` / `intermediary_of` / `registered_address` edges; proportions from `gen-icij-synth.py`), planting a clean 2-hop conflict — officers *Robert Smith* and *Jane Doe* both `officer_of* the shared entity *Acme Holdings Ltd*. For **both mmap and JSON** modes it builds, snapshots (`Close`), reopens, then measures:

- **reopen** — `NewGraphStorageWithConfig` on the persisted dir.
- **resolve** — label lookup + in-bucket name scan (the consumer's Resolve stage).
- **bfs** — 2-hop bounded adjacency BFS over `officer_of` (the consumer's Connect stage).
- **full-enum** — `GetAllNodesForTenant` (the path coi avoids), for contrast.

Correctness gate: both modes must flag the planted conflict identically and enumerate the full node set. Deterministic (seed 1729). Scale via `GRAPHDB_REOPEN_NODES`/`_EDGES` (default 936908 / 1316003).

## Results (936,908 nodes / 1,316,003 edges, local Fedora dev machine)

| metric | mmap | json |
|---|---:|---:|
| cold build | 11.47 s | 11.35 s |
| snapshot (`Close`) | 5.67 s | 5.59 s |
| **reopen** | **6 ms** | **8,217 ms** |
| coi **resolve** (label scan, 337,286 officers) | 262 ms | 322 ms |
| coi **bfs** (2-hop adjacency) | 171 µs | 46 µs |
| full **enumeration** (coi *avoids* this) | 416 ms | 641 ms |
| planted conflict flagged | ✅ | ✅ |

(Verified at 50K scale too; same qualitative result — mmap reopen ~0 ms, conflict flagged in both modes.)

## Findings

1. **B-1 = No.** The coi hot path is `resolve` (262 ms) + `bfs` (171 µs). Full enumeration (416 ms) is never on that path — coi resolves seeds by label and expands by adjacency. Enumeration-on-reopen is not a coi hot path, so **DoD Levers 2–3 (which optimize the full-enum residual) do not benefit coi-screen.**

2. **mmap reopen is ~1370× cheaper** at ICIJ scale (6 ms vs 8.2 s). This is the "cheap reopen" ask paying off end-to-end on a realistic shape. Combined with the property-based oracle (#440), the v1.2 mmap-default precondition is met **for this workload**.

3. **The real coi cost is the resolve scan, not enumeration or reopen.** `resolve` (262 ms) is a linear scan of the 337K-officer label bucket. If coi latency ever matters, the optimization target is a **name/property index on the label bucket** — not enumeration. This redirects future optimization effort away from DoD Levers 2–3 for this consumer.

4. **Adjacency BFS is trivially cheap** (tens–hundreds of µs) in both modes — the graph traversal itself is never the bottleneck.

5. **Consumer contracts hold in mmap at scale.** The bench transitively exercises **CC3** (adjacency survives reopen — the BFS found the conflict post-reopen) and **CC4** (bulk-imported data visible to `*ForTenant` readers — batch writes enumerated correctly); the dedicated contract tests (`TestEdgeAdjacencySurvivesReopen`, etc.) also pass.

## Limitations (honest scope)

- **The real `../coi-screen` consumer binary was not run** — that repo isn't checked out in this tree. *(Closed 2026-09-13: `COI_SCREEN_REAL_CORPUS_2026-09-13.md` runs it on the real corpus.)* This spike validates the storage *primitives* coi depends on (label resolve, adjacency BFS, reopen, CC3/CC4) at ICIJ scale, not the consumer's own resolver/soundex/path-ranking logic. See the runbook below to close that gap.
- **Synthetic corpus**, not the real ICIJ CSVs (not present locally). *(Wrong on 2026-09-13: the 2023-09-06 `full-oldb` package was at `/mnt/ssd2/Workspace/icij/` all along. Probe a recorded "not present" before it costs a decision.)* Structure and scale mirror ICIJ; exact degree distributions differ. The planted conflict and hub nodes reproduce the shapes that matter for the access pattern.
- **Single machine, single run** per mode. Numbers are directional at the order-of-magnitude that decides B-1, not a statistical benchmark.

## Runbook — real end-to-end consumer validation (deferred)

> **Corrected 2026-09-13.** Step 3 below could not work as written: the graphdb
> *library* never reads `GRAPHDB_STORAGE_MODE` (only `cmd/server`,
> `cmd/graphdb-admin` and `cmd/import-icij` do), and coi-screen did not set
> `StorageConfig.UseMmapSnapshot`, so graphdb refused every default import at
> open. coi-screen `28a759d` reads the variable itself; with that commit the env
> line is correct. For the *real* corpus, replace step 2 with
> `python3 scripts/icij-merge-nodes.py <unzipped full-oldb dir> all-nodes.csv`
> and import `all-nodes.csv` + `relationships.csv`. Results, numbers and the
> `Close`-rewrite finding: `COI_SCREEN_REAL_CORPUS_2026-09-13.md`.

To validate the actual consumer (requires the sibling repo):

```bash
# 1. Check out the consumer alongside graphdb
git clone https://github.com/dd0wney/coi-screen ../coi-screen
# 2. Generate a synthetic corpus + import in mmap mode
python3 scripts/gen-icij-synth.py /tmp/icij-synth
./bin/import-icij \
  --nodes /tmp/icij-synth/nodes.csv --edges /tmp/icij-synth/edges.csv --data ./data/icij-mmap
#    import-icij writes mmap snapshots by default (v1.2); pass --storage-mode json
#    or GRAPHDB_STORAGE_MODE=json to force the JSON path.
# 3. Run the real screen against the mmap data dir
GRAPHDB_STORAGE_MODE=mmap go run ../coi-screen/cmd/coi \
  --data ./data/icij-mmap --party "Robert Smith" --party "Jane Doe" --max-hops 2
#    Expect: the planted conflict is `flagged`.
# 4. scripts/consumer-drive.sh runs steps 1–3 (it SKIPs if ../coi-screen is absent).
```

Follow-up (done): `cmd/import-icij` now writes mmap snapshots by default with a `--storage-mode`/`GRAPHDB_STORAGE_MODE` opt-out, so the runbook's step 2 works without a code change.

## Recommendation

- **Answer B-1 in the planning docs: enumeration-on-reopen is not a coi-screen hot path.** Deprioritize DoD Levers 2–3 for this consumer; if coi latency is ever a concern, index the label-bucket resolve scan instead.
- **Proceed toward v1.2 mmap-default** for this workload — reopen is ~1370× cheaper and every coi hot-path op is a mmap-cheap path. Keep the property-based oracle (#440) as the correctness gate.
- File the `import-icij` mmap opt-in as a small follow-up so the real consumer can be driven end-to-end.
