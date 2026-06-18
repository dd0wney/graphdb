# Design: Split excessively large files (mechanical, same-package)

**Date:** 2026-06-18
**Status:** Approved (brainstorming) → pending implementation plan
**Scope decisions (from brainstorming):** worst-offenders threshold (source >800 LOC, test >1200 LOC), both source and test, **mechanical same-package** splitting only.

## Context

Several files in `pkg/` have grown large enough to hurt navigability and to be awkward to hold in context for editing. This is a focused maintainability pass: physically reorganize the genuinely-heterogeneous large files into smaller, responsibility-grouped files **within the same package**, changing nothing about behavior, symbols, signatures, or import paths.

**Guiding constraint (Ousterhout / the project's stated philosophy):** file size alone is not a defect. A large file with one tight responsibility is fine, and splitting it can manufacture shallow modules with leaky boundaries. So the candidate set (8 files over the threshold) is filtered by **cohesion** — only files that aggregate *unrelated* things get split. Mechanical same-package splitting does **not** create new modules: same package, same interfaces, identical symbols — only the physical file boundaries change.

**Why this is safe in Go:** files in the same package share a single declaration scope. Moving a `func`/`type`/`var` to a sibling file is a no-op to the compiler and to every consumer. The only mechanical risks are (a) per-file import blocks and (b) gofmt — both caught by `go build` + `gofmt`.

## Cohesion verdict (the filter applied)

Candidate set = source >800 LOC (3) + test >1200 LOC (5) = 8 files.

| File | LOC | Verdict | Reason |
|---|---|---|---|
| `pkg/query/physical_plan.go` | 1274 | **SPLIT** | 18 independent operator structs (Scan, IndexSeek, Expand, Filter, Project, Create, Set, Delete, Remove, Merge, Unwind, Union, OptionalMatch, Aggregate, NestedLoopJoin, HashJoin, Call). Aggregation, not cohesion. |
| `pkg/query/executor_test.go` | 2788 | **SPLIT** | 47 tests across distinct themes: match/read, mutation, where-clause, pagination/ordering, helper units. |
| `pkg/api/handlers_vectors_test.go` | 1354 | **SPLIT** | Audit-sanctioned (code-quality Track C4). Groups: index CRUD, search, property-filter. |
| `pkg/query/conformance_test.go` | 1453 | **SPLIT (moderate)** | 49 conformance tests; splittable by feature area (clauses / functions / vector). |
| `pkg/algorithms/centrality_test.go` | 1239 | **SPLIT (moderate)** | Node-centrality (degree/closeness/betweenness) vs edge-betweenness are independent groups. |
| `pkg/search/lsa.go` | 923 | **EXTRACT only** | One cohesive LSA algorithm; only the linear-algebra helper block (from the `// --- Linear algebra helpers ---` marker to EOF) is a clean extract. |
| `pkg/storage/node_operations.go` | 921 | **OPTIONAL follow-up** | Cohesive (all node CRUD). Weak split case. #423 (merged) grew/owned it; revisit only if a clear CRUD-vs-bulk seam holds up on reading. |
| `pkg/storage/mmap_reopen_test.go` | 1296 | **OPTIONAL follow-up** | Reopen-core vs Stage2* groups exist, but the file is a single cohesive parity suite sharing one fixture/oracle. Low priority. |

## Per-file split plan

Final groupings are confirmed by reading each file during implementation; the splits below are the intended shape. New files are siblings in the same package and directory.

### `pkg/query/physical_plan.go` → operator-family files
- Keep in `physical_plan.go`: the `PhysicalOperator` interface and any shared helpers/planner glue.
- `physical_ops_scan.go`: `NodeScanOperator`, `IndexSeekOperator`, `ExpandOperator`, `OptionalMatchOperator`
- `physical_ops_mutate.go`: `CreateOperator`, `SetOperator`, `DeleteOperator`, `RemoveOperator`, `MergeOperator`
- `physical_ops_project.go`: `FilterOperator`, `ProjectOperator`, `UnwindOperator`, `UnionOperator`, `AggregateOperator`
- `physical_ops_join.go`: `NestedLoopJoinOperator`, `HashJoinOperator`
- `physical_ops_call.go`: `CallOperator` (isolates the in-progress C3.x/C6 spike operator)

### `pkg/query/executor_test.go` → theme files
- Keep in `executor_test.go`: shared fixtures/helpers + `TestNewExecutor`.
- `executor_match_test.go`: `TestExecutor_Match*`, `TestExecutor_EmptyMatch`, `TestExecutor_MatchPath`
- `executor_mutation_test.go`: `TestExecutor_CreateNode/SetProperty/DeleteNode/CreateRelationship`
- `executor_where_test.go`: `TestExecutor_WhereClause*`
- `executor_pagination_test.go`: `Limit`/`Skip`/`SkipExceedsResults`/`Distinct`/`SortRows*`
- `executor_helpers_test.go`: `TestMatchStep_*`, `TestConvertValue`

### `pkg/api/handlers_vectors_test.go` → (Track C4)
- `handlers_vectors_crud_test.go`: Create/List/Get/Delete/Conflict/MethodNotAllowed/CRUD_Integration
- `handlers_vectors_search_test.go`: Search/NaNInf/ScoreCalculation/EmptyIndex/LabelFilterExclusion/TenantIsolation/DistanceToScore
- `handlers_vectors_propertyfilter_test.go`: the 7 `TestVectorSearch_PropertyFilter_*`

### `pkg/query/conformance_test.go` → feature files
- Keep shared setup in `conformance_test.go`.
- `conformance_clauses_test.go`: Explain/Profile/Collect/With/OptionalMatch/Union/Merge/Case/Parameterized
- `conformance_functions_test.go`: String/Numeric/Search function tests
- `conformance_vector_test.go`: VectorSimilarity*/VectorSyntheticScore

### `pkg/algorithms/centrality_test.go` → split edge-betweenness out
- Keep node centrality (degree/closeness/betweenness + `ComputeAllCentrality`) in `centrality_test.go`.
- `centrality_edge_test.go`: `TestEdgeBetweennessCentrality_*` + `TestComputeAllCentrality_IncludesEdgeBetweenness`

### `pkg/search/lsa.go` → extract helpers
- `lsa_linalg.go`: the linear-algebra helper block only. Core LSA index stays put.

## Sequencing

One atomic PR per file, ordered by confidence/value. Each is independent (different packages/files), so they can land in any order; this order front-loads the lowest-risk and highest-value:

1. `api/handlers_vectors_test.go` (audit-backed, lowest risk)
2. `query/executor_test.go` (biggest single win, 2788 LOC)
3. `query/physical_plan.go` (biggest source file)
4. `query/conformance_test.go`
5. `algorithms/centrality_test.go`
6. `search/lsa.go` (helper extract)

**Optional follow-ups (not promised here):** `storage/node_operations.go`, `storage/mmap_reopen_test.go` — only if reading confirms a clean seam.

## Verification (per PR)

A split is correct iff behavior is provably unchanged:
- `go build ./...` clean.
- `go test ./pkg/<area>/ -count=1` passes the **identical** test set (same test names, same count) — the green output is the evidence, not a LOC delta.
- `gofmt -l` reports nothing on the touched files; `goimports` minimal per-file import blocks.
- `golangci-lint run ./pkg/<area>/...` (CI gate; local toolchain may be blocked by the go1.25/1.26.4 gap — CI runs it).
- For test-file splits: diff the `go test -v` test-name list before/after to prove no test was dropped or renamed.
- PR description records before/after LOC per file.

## Out of scope (YAGNI)

- No sub-packages, no new interfaces, no import-path changes.
- No breaking up god-functions or changing responsibilities (that is the architecture-audit track HIGH-1/HIGH-2, not a file-size pass).
- No touching files under the threshold.
- No logic reordering within a file beyond what grouping the moves requires.
- No renames of any symbol.

## Risks

- **Merge conflicts with active work.** Splitting churns whole files. Mitigation: land these promptly, one package at a time; `node_operations.go`/`mmap_reopen_test.go` were intentionally sequenced after #423 (now merged).
- **Accidentally dropping a test in a move.** Mitigation: the before/after `go test -v` name-list diff in the verification step.
- **Hidden file-local unexported helpers** assumed unique but colliding after a move. Mitigation: `go build` catches duplicate declarations immediately; same-package scope means no visibility change.
