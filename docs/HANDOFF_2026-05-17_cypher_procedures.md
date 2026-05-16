# Handoff — Wire algorithms into Cypher procedure registry (Phase A)

**Date written**: 2026-05-17
**For**: next agent working in `dd0wney/graphdb` (this repo)
**From**: agent working in sibling repo `dd0wney/energy-based-model` (ebm-reason)
**Predecessor planning**: see `NEXT_STEPS_2026-05-15.md` for the broader queue; this task is independent of the named tracks there.

---

## Mission

Wire four existing algorithms in `pkg/algorithms/` as Cypher `CALL` procedures in `pkg/query/procedures.go`, following the pattern that already exists for `algo.shortestPath`:

1. `algo.kHop(sourceID, maxHops)` — k-hop neighborhood BFS
2. `algo.nodeSimilarity(nodeA, nodeB, metric)` — Jaccard / Overlap / Cosine over neighbor sets
3. `algo.linkPrediction(fromID, toID, method)` — Common Neighbours / Adamic-Adar / Preferential Attachment
4. `algo.pageRank()` — PageRank scores for all nodes (or for a seed set)

Each must be tenant-scoped (`*ForTenant` variants) and return Cypher-shaped results (`[]map[string]any{{ "key": value }}`).

## Why now — sibling-repo context

The sibling repo `dd0wney/energy-based-model` (referred to here as ebm-reason) is building a `CDataflowExpert` that does inter-procedural CERT-C vulnerability analysis. ADR 0002 in that repo (`../energy-based-model/docs/adr/0002-dataflow-expert.md`) records the design decision: rather than building a learned GNN, use graphdb's existing graph-algorithms + LSA-embeddings + GraphRAG triad. The dataflow expert will:

1. Use Joern to produce a Code Property Graph (CPG) from C source — functions as nodes, calls / dataflow as edges
2. Ingest the CPG into graphdb (nodes + edges + LSA embeddings on Function nodes)
3. Query via Cypher `CALL` procedures to confirm or rule out vulnerability candidates flagged by a function-level JEPA

Currently only `algo.shortestPath` is exposed via Cypher. The dataflow expert needs four more procedures (above) to be useful. That's Phase A; this handoff is Phase A.

The work plan in ADR 0002:
- **Phase A** (this handoff): wire algorithms into Cypher procedure registry
- **Phase B** (ebm-reason side, after Phase A merges): build Joern ingest + `GraphdbCPGBackend`
- **Phase C** (deferred, possibly): add `pkg/gnn/` if Phase B's deterministic results prove insufficient

ebm-reason has already landed the scaffolding (commit `2aee2ae` on branch `feat/data-priority-flow-edge-attr`): `CDataflowExpert`, `CPGBackend` Protocol, `StubCPGBackend`, ADR 0002 itself. It's waiting on this handoff's work to unlock the production backend.

## Concrete tasks

### Task A1 — `algo.kHop`

**File to edit**: `pkg/query/procedures.go`

**Algorithm**: `algorithms.KHopNeighbours` in `pkg/algorithms/khop.go:41`. Existing signature takes `*storage.GraphStorage` concrete type — see Watch-for #1 below.

**Procedure shape**:
```cypher
CALL algo.kHop(sourceID, maxHops) YIELD byHop, distances, totalReachable
```

**Procedure args**:
- `sourceID uint64` — node to BFS from
- `maxHops int` — depth limit (must be ≥ 1)
- Optional `direction string` (default `"out"`; accepts `"out"`, `"in"`, `"both"`) — corresponds to `NeighborDirection` enum
- Optional `edgeTypes []string` (default nil = all) — restrict expansion to named edge types

**Return**: one row with three fields:
```go
[]map[string]any{{
    "byHop":          opts.ByHop,          // map[int][]uint64
    "distances":      opts.Distances,      // map[uint64]int
    "totalReachable": opts.TotalReachable, // int
}}
```

### Task A2 — `algo.nodeSimilarity`

**File to edit**: `pkg/query/procedures.go`

**Algorithm**: `algorithms.NodeSimilarityPairForTenant` in `pkg/algorithms/node_similarity.go:155`.

**Procedure shape**:
```cypher
CALL algo.nodeSimilarity(nodeA, nodeB) YIELD score
CALL algo.nodeSimilarity(nodeA, nodeB, "cosine") YIELD score
```

**Procedure args**:
- `nodeA uint64`, `nodeB uint64`
- Optional `metric string` (default `"jaccard"`) — accepts `"jaccard"`, `"overlap"`, `"cosine"` per the `SimilarityMetric` enum

**Return**:
```go
[]map[string]any{{"score": score}}
```

Also worth wiring `algo.nodeSimilarityFor(sourceID)` later for top-k retrieval; recommend deferring to Task A4 or a follow-up if it expands scope.

### Task A3 — `algo.linkPrediction`

**File to edit**: `pkg/query/procedures.go`

**Algorithm**: `algorithms.PredictLinkScoreForTenant` in `pkg/algorithms/link_prediction.go:70`.

**Procedure shape**:
```cypher
CALL algo.linkPrediction(fromID, toID) YIELD score
CALL algo.linkPrediction(fromID, toID, "adamicAdar") YIELD score
```

**Procedure args**:
- `fromID uint64`, `toID uint64`
- Optional `method string` (default `"commonNeighbours"`) — accepts `"commonNeighbours"`, `"adamicAdar"`, `"preferentialAttachment"` per the `LinkPredictionMethod` enum

**Return**: same shape as `algo.nodeSimilarity`.

### Task A4 — `algo.pageRank`

**File to edit**: `pkg/query/procedures.go`

**Algorithm**: `algorithms.PageRankForTenant` in `pkg/algorithms/pagerank.go:54`.

**Procedure shape**:
```cypher
CALL algo.pageRank() YIELD scores
```

**Procedure args**: none required; reads tenant's full graph by default. Accept optional `damping float64` and `maxIterations int` if the underlying options struct exposes them.

**Return**:
```go
[]map[string]any{{"scores": result.Scores}}  // map[uint64]float64
```

PageRank is expensive on large graphs. Consider: emit a warning log if the result has >100k entries, or surface tenant-side limits via the existing rate-limiting infrastructure. Not strictly required for Phase A — but flag in the PR description so reviewers don't miss it.

## Existing pattern — your template

`pkg/query/procedures.go` already implements one procedure end-to-end. Read it before starting:

```go
// Procedure type (lines 32-33)
type Procedure func(ctx context.Context, graph storage.Storage,
                    tenantID string, args []any) ([]map[string]any, error)

// Registry (lines 36-39)
var procedureRegistry = map[string]Procedure{
    "algo.shortestPath": shortestPathProcedure,
}

// Procedure body (lines 49-72)
func shortestPathProcedure(_ context.Context, graph storage.Storage,
                           tenantID string, args []any) ([]map[string]any, error) {
    if len(args) < 2 {
        return nil, fmt.Errorf("algo.shortestPath requires 2 arguments...; got %d", len(args))
    }
    startID, ok := coerceToUint64(args[0])
    if !ok { return nil, fmt.Errorf("...") }
    // ... call algorithm ...
    return []map[string]any{{"path": path}}, nil
}
```

The `coerceToUint64` helper at the bottom of the file is already shared — reuse it for node-ID args. For string args (`direction`, `metric`, `method`), do a `string` type assertion with a clear error message on mismatch.

## Watch for — gotchas

### 1. Interface vs concrete graph type

`Procedure` takes `storage.Storage` (interface, per Decision 6 = B in `NEXT_STEPS_2026-05-13.md`). `ShortestPathForTenant` was refactored to take the interface. **The other algorithms still take `*storage.GraphStorage` (concrete type)**:

```go
algorithms.KHopNeighbours(graph *storage.GraphStorage, ...)
algorithms.PageRank(graph *storage.GraphStorage, ...)
algorithms.NodeSimilarityPairForTenant(graph *storage.GraphStorage, ...)
algorithms.PredictLinkScoreForTenant(graph *storage.GraphStorage, ...)
```

You have two choices:

**Option α**: refactor each algorithm to take `storage.Storage` (matches the `shortestPath` precedent). This is the structurally cleaner path and matches Decision 6 = B's direction. Estimated work: ~30 min per algorithm if their internals only use methods already on the interface; longer if they use storage internals that aren't exposed.

**Option β**: type-assert inside the procedure body:
```go
graphImpl, ok := graph.(*storage.GraphStorage)
if !ok {
    return nil, fmt.Errorf("algo.kHop: graph must be *storage.GraphStorage; got %T", graph)
}
result, err := algorithms.KHopNeighbours(graphImpl, sourceID, opts)
```
Faster, but pushes the same decision down the road and creates an asymmetry between procedures.

**Recommended**: Option α. Worth the extra time. If you discover that algorithms genuinely need concrete-type access (e.g., reaching into unexported fields), surface that as a finding and fall back to Option β with a TODO that names the constraint.

### 2. Gemini-bulk archive does NOT compile on main

Per the `CLAUDE.md` in this repo, there's an archive branch `origin/archive/gemini-bulk-2026-05-13` whose `pkg/query/procedures.go` registers `gnn.messagePass` and `llm.generate` — but those depend on `pkg/gnn` and `pkg/intelligence` which **don't exist on OSS main**. The current `procedures.go` already has a comment about this:

```go
// Future procedures:
//   - gnn.messagePass — skipped; pkg/gnn doesn't exist on OSS
//   - llm.generate — dropped; pkg/intelligence doesn't exist on OSS
```

**Do not lift from the archive without checking imports.** Use `git ls-tree origin/archive/gemini-bulk-2026-05-13^3 -- pkg/query/procedures.go` plus `grep '^\s*"github\.com/dd0wney/cluso-graphdb/' <file>` to enumerate. If an import isn't on main, narrow the lift.

### 3. Tenant-strict variants only

Per the `*ForTenant` convention documented in `CLAUDE.md`: every algorithm has a tenant-blind (`KHopNeighbours`) and tenant-strict (`KHopNeighboursForTenant`) variant. **Procedures must use the tenant-strict variant** — the procedure's `tenantID string` parameter is already provided by the caller. Cross-tenant lookups return `ErrNodeNotFound` (per the existence-leak side-channel discipline); pass that through.

If `KHopNeighbours` doesn't have a `ForTenant` variant yet, you may need to add one. Mirror the pattern from `KHopNeighboursForTenant` or `PredictLinkScoreForTenant` if they exist; if not, this is genuinely new tenant-scoping work and the PR should split: tenant-scope first, procedure-wire second.

### 4. Tests are required

Each new procedure needs a test in `pkg/query/procedures_test.go` (or a new file if the existing one doesn't exist — `pkg/query/` already has tests for `CallOperator`, so the harness is in place). Test pattern:

- Set up a tenant-scoped GraphStorage with a small fixture (5-10 nodes, 10-20 edges)
- Call the procedure via the registry lookup (not the function directly — exercises the dispatch path)
- Assert on the returned map structure

Existing test files in `pkg/query/` are the template; copy their tenant-setup boilerplate.

### 5. Run the full test discipline before PR

Per `CLAUDE.md`'s pre-PR section:

```bash
go build ./...
go vet ./...
go test ./pkg/query/ -short -timeout 90s -count=1
go test ./pkg/algorithms/ -short -timeout 90s -count=1   # if you refactor signatures
golangci-lint run ./...
```

PR title format: `feat(query): wire algo.{kHop,nodeSimilarity,linkPrediction,pageRank} as Cypher procedures` or similar. The PR body should:
- Link to this handoff
- Link to ADR 0002 in the sibling repo: `../energy-based-model/docs/adr/0002-dataflow-expert.md`
- Say which Option (α or β) was chosen for the interface-vs-concrete question
- List which `*ForTenant` variants exist and which were added

## Acceptance criteria

The handoff is done when:

1. ✅ `pkg/query/procedures.go` registers all four procedures (`algo.kHop`, `algo.nodeSimilarity`, `algo.linkPrediction`, `algo.pageRank`) in `procedureRegistry`.
2. ✅ Each procedure has a unit test in `pkg/query/procedures_test.go` that exercises happy path + 1-2 error cases (bad arg shape, unknown tenant).
3. ✅ Full test suite passes: `go test ./pkg/query/ ./pkg/algorithms/ -count=1`.
4. ✅ `golangci-lint run ./...` clean.
5. ✅ PR opened with a body that addresses every "Watch for" item.

What's **out of scope** for this handoff:

- Adding `pkg/gnn/` or any learned-GNN infrastructure (deferred to Phase C in ADR 0002).
- LSA embedding queryability (separate Phase A sub-task; can be tackled now if you have spare time, but not blocking ebm-reason's Phase B start).
- Joern integration, CPG ingest, or any code-graph-specific schema work — that's all on the ebm-reason side (Phase B).
- Any orchestration / scheduling layer for expensive procedures (pageRank on large graphs) — note in PR description, don't implement.

## References

- **ADR 0002** in sibling repo: `../energy-based-model/docs/adr/0002-dataflow-expert.md` — the architectural decision motivating this work, including why we chose graph algorithms over a learned GNN
- **ebm-reason scaffold commit**: `2aee2ae` on branch `feat/data-priority-flow-edge-attr` — `CDataflowExpert`, `CPGBackend` Protocol, `StubCPGBackend`, 10 tests, ADR 0002
- **graphdb existing pattern**: `pkg/query/procedures.go` (the `shortestPathProcedure` template you'll mirror)
- **graphdb conventions**: this repo's `CLAUDE.md` (tenant-scoping, atomic commit hygiene, archive-import gotcha)
- **graphdb planning**: `docs/NEXT_STEPS_2026-05-15.md` (broader queue context; this task is independent)
- **graphdb algorithms package**: `pkg/algorithms/` (the source of truth for what's available — read the function signatures before designing the procedure wrappers)

## Why the design lands here, in one paragraph

ebm-reason has a function-level vulnerability classifier (val_top1=0.610 on 14,659 vulnerable C functions across PrimeVul + DiverseVul + MegaVul). It can flag suspicious functions but can't reason about cross-file dataflow — buffer overflows and integer underflows that only reveal themselves through caller/callee composition slip past it. The dataflow expert closes that gap by querying graphdb's CPG-of-the-project for feasible taint paths. Graph algorithms + tenant-scoped Cypher procedures + LSA embeddings are the right substrate because dataflow analysis is fundamentally graph traversal with semantic filtering, producing auditable paths (which a security operator can review) rather than confidence scores (which they can't). The procedures you're about to wire are what makes that whole pipeline work.

Good luck. Ping the ebm-reason agent's PR (or open an issue against `oit-cyber/energy-based-model`) if Phase A's scope shifts under you — Phase B is paused on your work.
