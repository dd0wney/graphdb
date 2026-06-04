# Session handoff — 2026-06-04 08:10 UTC

**Date**: 2026-06-04 (single continuation session: picked up Phase C from the 0442Z handoff, then extended the invariant checker to `propertyIndexes` — which found + fixed a transaction bug — and **cut the v0.4.0 release** prompted by the stór consumer)
**Outgoing model**: Claude Opus 4.8 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

The parallel-invariant test harness reached its planned end: Phase C (metamorphic cross-path equivalence, `#314`) and the `propertyIndexes` checker (`#316`) both shipped, the latter finding and fixing a real `Transaction.Commit` property-index drift. graphdb cut **`v0.4.0`** — its first release capturing the entire durability/persistence hardening wave since `v0.3.0` — so the stór consumer can drop `replace => ../graphdb` and pin. A cross-repo correction landed: `Transaction.Commit` is **not** dormant; stór is a live caller (do not deprecate that path).

---

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #314 | test(storage): metamorphic cross-path equivalence (improved testing C) | Phase C. One op-script through all four write paths; asserts identical `VectorSearchForTenant` top-k (translated to logical handles), closing the count-only vector blind spot in `#310`/`#311`. Non-vacuity teeth-test + teeth-mutation proven. Test-only. |
| #315 | docs(planning): mark Phase C done (#314) | Shape-A reconciliation. |
| #316 | test(storage): extend invariant checker to propertyIndexes (+ transaction fix it found) | Two commits: **fix** — `Transaction.Commit`'s existing-node update skipped `updatePropertyIndexes` (property-index drift, #288 class); **test** — checker assertion (exact membership, no empties, no dupes) + 4 teeth cases + property matrix across live/batch/WAL-replay/transaction. **FTS deliberately excluded** — it's API-layer/admin-rebuilt/non-persisted, not a storage invariant. |
| #317 | docs(planning): propertyIndexes done + FTS reframed + Transaction-caller correction | Shape-C correction: retired the "Transaction dormant/zero callers" claim (stór is a live caller). |

**Also (not a PR): cut `v0.4.0`** — annotated tag on `cf51aff` (`#316`) + published GitHub release ([link](https://github.com/dd0wney/graphdb/releases/tag/v0.4.0)), Latest, non-draft. Captures ~440 commits since `v0.3.0` incl. the persistence fixes (#287 adjacency, #305 vector index) that predate every prior tag.

---

## Current state

- **`origin/main` HEAD**: `1d0cb11` (`#317`).
- **Open PRs**: none.
- **Open branches**: `main` only (all merged with `--delete-branch`).
- **Uncommitted**: none except the pre-existing untracked `.claude/scheduled_tasks.lock` (ignore; carried across prior handoffs).
- **Release**: `v0.4.0` is Latest.
- **Test/lint**: `main` green. `#316` (the only production change — `transaction_commit.go`) verified: full `pkg/storage` suite (203s), invariant/property/metamorphic surface `-race -count=3`, `go vet`, `golangci-lint run ./...` 0 issues, and dependent `pkg/query` + `pkg/api` green. 12/12 teeth cases, 6/6 property-matrix cells.

---

## What's next

`NEXT_STEPS_2026-06-03.md` is current (reconciled by `#315`/`#317`). **No critical path is forced.** Earned/standing candidates:

- **Productization** — `v0.4.0` just unblocked the consumer-pin path; the off-path productization track (Python SDK, the 4 documented-but-unbuilt enterprise plugins, onboarding docs) is now the highest *external* value. The single-node-by-design framing + deployment-ordering note belong here.
- **Small silent-bug backlog** — `CreateVectorIndex` not WAL-logged (an index created after the last snapshot is lost on crash; note: `CreatePropertyIndex` **is** WAL-logged, confirmed this session); LSA-index stale after WAL-replay. Both narrow.
- **Invariant-checker is now "done" for the storage structures** — global+per-tenant label/type, counts, adjacency, vector (count-only), and propertyIndexes (exact) are all covered. FTS is consciously out of scope (API-layer). No obvious next storage structure to add.

### New gaps surfaced this session (for the next planning checkpoint)

- **FTS bootstrap** — the only real open FTS question is whether a deployment auto-rebuilds the FTS index at API startup (`POST /search/index` is the only populator and it's not persisted). Ops/bootstrap concern, not a storage invariant — would be an `pkg/api`-layer test if pursued.

---

## Stale assumptions to retire

1. **`NEXT_STEPS_2026-06-03.md:59` "`Transaction` had zero non-test callers … dormant"** — ALREADY corrected by `#317` (added a do-not-deprecate flag). stór's `Register`/`LinkRealisation` are live `Transaction.Commit` callers. New memory `project_transaction_path_live_consumer`. **Do not deprecate/refactor the Transaction path on a no-callers basis.**
2. **"Phase C is the readiest earned item" / "extend checker to propertyIndexes + FTS"** — both retired: Phase C done (`#314`), propertyIndexes done (`#316`), FTS reframed out of the storage checker (`#317`).
3. **"Tags stop at v0.3.0 — don't pin, use `replace`"** (stór-side assumption) — retired: `v0.4.0` exists and is the right pin (incl. #287/#305/#316).
4. **Memory `feedback_parallel_invariant_coverage`** updated this session to include `#316` (propertyIndexes) — the checker now covers exact property-index membership, not just the structures listed when it was written.

---

## Open questions for the user

1. **Next direction: productization vs. the small WAL-logging backlog vs. stop?** No forced critical path. `v0.4.0` makes productization the highest external-value move; the WAL-logging items are quick correctness closes. The user's call.
2. **Cross-repo (informational, not graphdb-gated):** stór will drop `replace` and pin `v0.4.0` (~2-line `go.mod`), then proceed to M1.b (inter-process locking — stór-side, graphdb won't provide it). No graphdb action required unless stór surfaces a new divergence.

---

## Next-session prompt (paste-ready)

See `docs/internals/design/NEXT_SESSION_PROMPT.md` (singleton, regenerated by this handoff).

---

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-06-03.md` (current; reconciled through `#317`).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded).
4. If touching the `Transaction` path or any write path: read stale-assumption #1 + memory `project_transaction_path_live_consumer` before assuming anything about callers.
