# Split Excessively Large Files — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Physically reorganize 6 oversized, low-cohesion files into smaller responsibility-grouped sibling files within the same package, with zero behavior change.

**Architecture:** Pure mechanical same-package splitting. Every moved `func`/`type` keeps its name, signature, receiver, and body verbatim; only the file it lives in changes. Go same-package scope means moves are invisible to the compiler and to every consumer. Each file is one atomic PR.

**Tech Stack:** Go 1.26 (toolchain targets 1.26.4), standard `go test` / `go build` / `gofmt` / `goimports`.

## Global Constraints

- **No symbol/signature/import-path changes.** Move declarations verbatim. Never rename, never edit a body.
- **Same package only.** New files share the package clause and directory of the original. No sub-packages.
- **Behavior is provably unchanged** per task: the package's test set must be byte-identical (same test names, same count) and green before and after. Green output is the evidence — never a LOC delta.
- **Shared test helpers don't need to move.** In Go, every `*_test.go` in a package shares scope, so unexported helpers (e.g. `setupExecutorTestGraph`) stay in the original file and remain visible to the moved tests. Move only the functions listed; leave helpers in place unless a task says otherwise.
- **Atomic commits**, conventional-commit messages, imperative mood. End each commit body with:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- **Lint:** `golangci-lint` is the CI gate; locally it may fail to load on the go1.25-vs-1.26.4 toolchain gap — that is expected, CI runs it. `gofmt -l` and `go vet` must be clean locally.
- **Branch per task** off latest `origin/main`; PR with `--delete-branch` at merge once CI is green (read `statusCheckRollup`, not a piped exit code).

### Verification recipe (referenced by every task)

For package `<PKG>` (e.g. `./pkg/api/`) and the touched files `<FILES>`:

1. **Baseline test list:** `go test <PKG> -list '.*' | sort > /tmp/split-before.txt` — and confirm green: `go test <PKG> -count=1` ends in `ok`.
2. After moving, **fix imports:** `goimports -w <FILES>` (each new file gets only the imports it uses; the shrunken original may shed some).
3. **Build + vet:** `go build ./... && go vet <PKG>` — clean.
4. **Test-list parity:** `go test <PKG> -list '.*' | sort > /tmp/split-after.txt && diff /tmp/split-before.txt /tmp/split-after.txt` — **must be empty** (no test dropped or renamed).
5. **Green:** `go test <PKG> -count=1` ends in `ok`.
6. **Format:** `gofmt -l <FILES>` prints nothing.

If `goimports` is unavailable, hand-edit the import block and rely on step 3 (`go build`) to catch omissions/extras (`imported and not used` / undefined).

---

## Task 1: Split `pkg/api/handlers_vectors_test.go` (1354 LOC, audit Track C4)

**Files:**
- Modify: `pkg/api/handlers_vectors_test.go` (remove the moved tests; keep package clause, imports still used, and any unexported helpers)
- Create: `pkg/api/handlers_vectors_crud_test.go`
- Create: `pkg/api/handlers_vectors_search_test.go`
- Create: `pkg/api/handlers_vectors_propertyfilter_test.go`

**Interfaces:** none crossed — same package, test-only. Shared helpers stay put and remain visible.

- [ ] **Step 1: Baseline.** `go test ./pkg/api/ -list '.*' | sort > /tmp/split-before.txt`; `go test ./pkg/api/ -run 'Vector|VectorIndex|DistanceToScore' -count=1` ends in `ok`.

- [ ] **Step 2: Create `handlers_vectors_crud_test.go`.** Start with `package api` + the imports it needs. Move these functions verbatim from `handlers_vectors_test.go`:
  `TestCreateVectorIndex`, `TestCreateVectorIndex_Conflict`, `TestListVectorIndexes`, `TestGetVectorIndex`, `TestDeleteVectorIndex`, `TestVectorIndexes_MethodNotAllowed`, `TestVectorIndex_CRUD_Integration`.

- [ ] **Step 3: Create `handlers_vectors_search_test.go`.** `package api` + imports. Move verbatim:
  `TestVectorSearch`, `TestVectorSearch_NaNInfValidation`, `TestVectorSearch_ScoreCalculation`, `TestVectorSearch_EmptyIndex`, `TestVectorSearch_LabelFilterExclusion`, `TestVectorSearch_TenantIsolation`, `TestDistanceToScore`.

- [ ] **Step 4: Create `handlers_vectors_propertyfilter_test.go`.** `package api` + imports. Move verbatim the 7:
  `TestVectorSearch_PropertyFilter_NoOpWhenAbsent`, `_ExcludesNonMatching`, `_EmptyResultsNotError`, `_AndsWithLabels`, `_BoolRoundTrip`, `_NonPrimitiveRejected`, and `TestVectorSearch_PropertyFilter_*` remaining (confirm with `grep -n 'func TestVectorSearch_PropertyFilter' pkg/api/handlers_vectors_test.go` before moving — there are 7).

- [ ] **Step 5: Delete the moved functions** from `handlers_vectors_test.go`. Anything left (helpers, remaining tests) stays.

- [ ] **Step 6: Run the verification recipe** with `<PKG>=./pkg/api/` and `<FILES>` = the 4 files. All gates pass; diff empty.

- [ ] **Step 7: Commit.**
```bash
git add pkg/api/handlers_vectors_*_test.go pkg/api/handlers_vectors_test.go
git commit -m "refactor(api): split handlers_vectors_test.go by responsibility (crud/search/property-filter)"
```

---

## Task 2: Split `pkg/query/executor_test.go` (2788 LOC)

**Files:**
- Modify: `pkg/query/executor_test.go` (keep helpers `setupExecutorTestGraph`, `findRowByColumn`, and `TestNewExecutor`)
- Create: `executor_match_test.go`, `executor_mutation_test.go`, `executor_where_test.go`, `executor_pagination_test.go`, `executor_aggregation_test.go`, `executor_helpers_test.go` (all under `pkg/query/`)

**Interfaces:** none crossed. `setupExecutorTestGraph` / `findRowByColumn` remain in the original and are visible to all moved tests (same package).

- [ ] **Step 1: Baseline.** `go test ./pkg/query/ -list '.*' | sort > /tmp/split-before.txt`; `go test ./pkg/query/ -run Executor -count=1` ends in `ok`.

- [ ] **Step 2: `executor_match_test.go`** (`package query` + imports). Move verbatim: `TestExecutor_MatchSingleNode`, `TestExecutor_MatchWithProperties`, `TestExecutor_MatchPath`, `TestExecutor_EmptyMatch`, `TestExecutor_ExecuteWithText`.

- [ ] **Step 3: `executor_mutation_test.go`.** Move: `TestExecutor_CreateNode`, `TestExecutor_SetProperty`, `TestExecutor_DeleteNode`, `TestExecutor_CreateRelationship`.

- [ ] **Step 4: `executor_where_test.go`.** Move: `TestExecutor_WhereClause`, `TestExecutor_WhereClause_Equals`.

- [ ] **Step 5: `executor_pagination_test.go`.** Move: `TestExecutor_Limit`, `TestExecutor_Skip`, `TestExecutor_SkipExceedsResults`, `TestExecutor_Distinct`, `TestExecutor_SortRows_Ascending`, `TestExecutor_SortRows_Descending`, `TestExecutor_SortRows_EmptyOrderBy`, `TestExecutor_SortRows_Strings`.

- [ ] **Step 6: `executor_aggregation_test.go`.** Move all of: `TestExecutor_Aggregation_COUNT`, `_SUM`, `_AVG`, `_MIN_MAX`, `_MixedTypes`, `_NullValues`, `_StringMinMax`, `_COLLECT`, `_COLLECT_Empty`, `_COLLECT_WithGroupBy`; `TestExecutor_GroupBy_Single`, `_Multiple`, `_WithOrderBy`, `_EmptyGroup`, `_LIMIT_SKIP`; `TestExecutor_DISTINCT_With_Aggregation`, `TestExecutor_EmptyAggregation`, `TestExecutor_ComplexQuery`, `TestExecutor_Integration_WHERE_GroupBy_Aggregation`, `TestExecutor_Integration_MultipleAggregations`, `TestExecutor_Integration_ComplexWHERE_Aggregation`, `TestExecutor_Integration_DISTINCT_WHERE_OrderBy`.

- [ ] **Step 7: `executor_helpers_test.go`.** Move: `TestMatchStep_CopyBinding`, `TestMatchStep_HasLabels`, `TestMatchStep_MatchProperties`, `TestMatchStep_ValuesEqual`, `TestConvertValue`.

- [ ] **Step 8: Delete moved functions** from `executor_test.go`. It now holds only `setupExecutorTestGraph`, `findRowByColumn`, `TestNewExecutor`.

- [ ] **Step 9: Verification recipe** with `<PKG>=./pkg/query/`, `<FILES>` = the 7 files. Diff empty; green.

- [ ] **Step 10: Commit.**
```bash
git add pkg/query/executor_test.go pkg/query/executor_*_test.go
git commit -m "refactor(query): split executor_test.go by theme (match/mutation/where/pagination/aggregation/helpers)"
```

---

## Task 3: Split `pkg/query/physical_plan.go` (1274 LOC) into operator-family files

**Files:**
- Modify: `pkg/query/physical_plan.go` (keep the `PhysicalOperator` interface and any non-operator shared helpers)
- Create: `physical_ops_scan.go`, `physical_ops_mutate.go`, `physical_ops_project.go`, `physical_ops_join.go`, `physical_ops_call.go` (all `pkg/query/`)

**Interfaces:** none crossed. Each operator type implements `PhysicalOperator` (unchanged). For each type listed, move the `type X struct {...}` **and every method with receiver `X`** verbatim.

- [ ] **Step 1: Baseline.** `go test ./pkg/query/ -count=1` ends in `ok`; `go build ./...` clean. (No test names change in this task — the gate is package-green + build.)

- [ ] **Step 2: `physical_ops_scan.go`** (`package query` + imports). Move types + all their methods: `NodeScanOperator`, `IndexSeekOperator`, `ExpandOperator`, `OptionalMatchOperator`.

- [ ] **Step 3: `physical_ops_mutate.go`.** Move: `CreateOperator`, `SetOperator`, `DeleteOperator`, `RemoveOperator`, `MergeOperator` (+ all their methods).

- [ ] **Step 4: `physical_ops_project.go`.** Move: `FilterOperator`, `ProjectOperator`, `UnwindOperator`, `UnionOperator`, `AggregateOperator` (+ methods).

- [ ] **Step 5: `physical_ops_join.go`.** Move: `NestedLoopJoinOperator`, `HashJoinOperator` (+ methods).

- [ ] **Step 6: `physical_ops_call.go`.** Move: `CallOperator` (+ methods). This isolates the in-progress C3.x/C6 spike operator.

- [ ] **Step 7: Delete the moved types+methods** from `physical_plan.go`. Confirm `grep -nE '^type .*Operator' pkg/query/physical_plan.go` shows none of the moved ones; the `PhysicalOperator` interface remains.

- [ ] **Step 8: Verify.** `go build ./...` clean; `go vet ./pkg/query/` clean; `go test ./pkg/query/ -count=1` ends in `ok`; `gofmt -l` on the 6 files prints nothing. (A duplicate or missed move surfaces immediately as a `go build` redeclaration/undefined error.)

- [ ] **Step 9: Commit.**
```bash
git add pkg/query/physical_plan.go pkg/query/physical_ops_*.go
git commit -m "refactor(query): split physical_plan.go into operator-family files"
```

---

## Task 4: Split `pkg/query/conformance_test.go` (1453 LOC) by feature

**Files:**
- Modify: `pkg/query/conformance_test.go` (keep shared helpers `setupConformanceGraph`, `parseAndExecute`, `parseAndExecuteWithParams`, `setupPhase3ConformanceGraph`, `setupPhase5ConformanceGraph` + the clause/phase tests)
- Create: `conformance_functions_test.go`, `conformance_vector_test.go` (both `pkg/query/`)

**Interfaces:** none crossed. Vector tests need `setupVectorConformanceGraph` + `wireVectorSearch` — move those two helpers **with** the vector tests so that file is self-contained; all other helpers stay in `conformance_test.go` and remain visible.

- [ ] **Step 1: Baseline.** `go test ./pkg/query/ -list '.*' | sort > /tmp/split-before.txt`; `go test ./pkg/query/ -run Conformance -count=1` ends in `ok`.

- [ ] **Step 2: `conformance_vector_test.go`** (`package query` + imports). Move verbatim the helpers `setupVectorConformanceGraph`, `wireVectorSearch` AND tests: `TestConformance_VectorSimilarityInWhere`, `_VectorSimilarityInReturn`, `_VectorSyntheticScore`, `_VectorBruteForceWithoutIndex`, `_VectorExplainShowsStep`, `_VectorHighThresholdEmptyResults`, `_VectorWithANDCondition`, `_VectorScoreComparison`.

- [ ] **Step 3: `conformance_functions_test.go`.** Move: `TestConformance_StringFunctionsInWhere`, `_StringFunctionsInReturn`, `_NumericFunctionsInWhere`, `_SearchFunction`.

- [ ] **Step 4: Delete the moved functions** from `conformance_test.go`. Everything else (Explain/Profile/Collect/With/Union/Merge/Case/Parameterized + all Phase3/4/5 tests + their setups) stays.

- [ ] **Step 5: Verification recipe** with `<PKG>=./pkg/query/`, `<FILES>` = the 3 files. Diff empty; green.

- [ ] **Step 6: Commit.**
```bash
git add pkg/query/conformance_test.go pkg/query/conformance_functions_test.go pkg/query/conformance_vector_test.go
git commit -m "refactor(query): split conformance_test.go (functions + vector groups)"
```

---

## Task 5: Split `pkg/algorithms/centrality_test.go` (1239 LOC)

**Files:**
- Modify: `pkg/algorithms/centrality_test.go` (keep `setupCentralityTestGraph` + node-centrality tests)
- Create: `pkg/algorithms/centrality_edge_test.go`

**Interfaces:** none crossed. `setupCentralityTestGraph` stays in the original, visible to the moved edge tests.

- [ ] **Step 1: Baseline.** `go test ./pkg/algorithms/ -list '.*' | sort > /tmp/split-before.txt`; `go test ./pkg/algorithms/ -run Centrality -count=1` ends in `ok`.

- [ ] **Step 2: `centrality_edge_test.go`** (`package algorithms` + imports). Move verbatim: `TestEdgeBetweennessCentrality_EmptyGraph`, `_SingleNode`, `_ExactValues`, `_TopEdgesOrdering`, `_StevesUtility`, and `TestComputeAllCentrality_IncludesEdgeBetweenness`.

- [ ] **Step 3: Delete the moved functions** from `centrality_test.go`. Node-centrality tests (Degree/Closeness/Betweenness + `TestComputeAllCentrality`, `_ComplexGraph`, `_Normalization`, `BetweennessCentrality_ExactValues`, `_StevesUtility`) remain.

- [ ] **Step 4: Verification recipe** with `<PKG>=./pkg/algorithms/`, `<FILES>` = the 2 files. Diff empty; green.

- [ ] **Step 5: Commit.**
```bash
git add pkg/algorithms/centrality_test.go pkg/algorithms/centrality_edge_test.go
git commit -m "refactor(algorithms): split edge-betweenness tests out of centrality_test.go"
```

---

## Task 6: Extract `pkg/search/lsa_linalg.go` from `lsa.go` (923 LOC)

**Files:**
- Modify: `pkg/search/lsa.go` (remove the linear-algebra helper functions; keep all types incl. `sparseRow`, `bm25Entry`, and the LSA index + BM25 + query methods)
- Create: `pkg/search/lsa_linalg.go`

**Interfaces:** none crossed. The 7 unexported helpers move; the types they operate on (`sparseRow` etc.) stay in `lsa.go` and remain visible.

- [ ] **Step 1: Baseline.** `go test ./pkg/search/ -count=1` ends in `ok`; `go build ./...` clean.

- [ ] **Step 2: `lsa_linalg.go`** (`package search` + imports — likely just `math` and/or `math/rand`). Move verbatim the functions below the `// --- Linear algebra helpers ---` marker: `lsaRandMatrix`, `lsaSparseMulDense`, `lsaSparseTMulDense`, `lsaQR`, `lsaLeftMul`, `lsaGram`, `lsaJacobi`. Carry the `// --- Linear algebra helpers ---` comment to the new file as a header.

- [ ] **Step 3: Delete the moved functions** (and the now-trailing marker comment) from `lsa.go`.

- [ ] **Step 4: Verify.** `goimports -w pkg/search/lsa.go pkg/search/lsa_linalg.go`; `go build ./...` clean; `go vet ./pkg/search/` clean; `go test ./pkg/search/ -count=1` ends in `ok`; `gofmt -l` on both files prints nothing.

- [ ] **Step 5: Commit.**
```bash
git add pkg/search/lsa.go pkg/search/lsa_linalg.go
git commit -m "refactor(search): extract linear-algebra helpers into lsa_linalg.go"
```

---

## Optional follow-ups (not scheduled; only if a clean seam holds on reading)

- `pkg/storage/node_operations.go` (921) — cohesive (all node CRUD). Split only if a CRUD-vs-bulk seam is obvious; otherwise leave.
- `pkg/storage/mmap_reopen_test.go` (1296) — single cohesive parity suite sharing one fixture/oracle. Low priority.

## Self-Review

- **Spec coverage:** all 6 SPLIT/EXTRACT files in the spec have a task (Tasks 1–6); the 2 OPTIONAL files are carried as optional follow-ups, matching the spec. ✔
- **Placeholder scan:** no TBD/TODO; every move lists exact function/type names; the verification recipe is concrete. The one judgment call (the property-filter `grep` confirm in Task 1 Step 4) is a guard, not a placeholder. ✔
- **Type/name consistency:** symbol names copied from `grep` output of the live files; new filenames consistent across Files/Steps/commit commands. ✔
- **Deviation from spec (logged):** Task 2 adds `executor_aggregation_test.go` beyond the spec's 5 named theme files — the aggregation/group-by cluster (~22 funcs) is its own cohesive theme; the spec authorized confirming groupings during implementation. ✔
