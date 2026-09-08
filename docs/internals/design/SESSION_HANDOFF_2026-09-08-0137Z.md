# Session handoff — 2026-09-08 01:37 UTC

**Date**: 2026-09-08 (single session, ~2.5 hours; 8 PRs merged, 2 open, one plan executed end to end)
**Outgoing model**: Claude Fable 5.1 (1M context) — `settings.json` carried `"model": "claude-fable-5-1[1m]"` throughout; the main loop planned, ruled and reviewed, and dispatched every implementation.
**Delegation**: 4 one-line probes (general-purpose ×2, Explore, Plan; model measurement) — used as-is. 4 `reviewer` calls (haiku ×2, sonnet ×2) on two planted defects — used as-is; both tiers found both plants. `/orchestrate` for ADR 0001: architect (opus) — used with three rulings overriding it; api, database, security, tester, reviewer (sonnet) — used, the database and api reports' interface-membership proposal overridden (R1). 6 `worker` (sonnet) implementer dispatches: tasks 10, 11, 12 (+1 fix round), 13, ADR 0001 stage 1 (+1 fix round), stage 2 (+1 fix round) — every one landed after review. 8 task reviews: sonnet ×5 (tasks 10, 11, 12, 13, stage-1 and stage-2 re-reviews), haiku ×1 (task 10 re-review), opus ×2 (ADR 0001 stages 1 and 2; both found real defects the plan carried). 2 haiku measurements (task 14 coverage sweep, task 13 benchmark rerun) — task 14 needed main-loop correction: its per-package skip columns were global counts, "macOS" was wrong, and it appended forbidden analysis; its core numbers were verified by an independent recomputation and stood.
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

## 1. TL;DR

The model-tier plan ran end to end: an unpinned `general-purpose` or `Plan` agent
was measured to inherit Fable, the Agent hook now refuses such calls, and every
code task of the queue shipped through sonnet implementers with reviews. Two
real defects came out of it: a default-configuration mmap adjacency bug (#575,
`CheckInvariants` caught it) and a plan defect in ADR 0001's enforcement (a
label typo at bootstrap would have disabled the guarantee in silence; ruling
R9). ADR 0001 is implemented in PR #578, open.

## 2. What is done this session

| PR | Merge | Title | Notes |
|---|---|---|---|
| #570 | `ae623eb` | docs(planning): mark the mmap CheckInvariants gap closed (#569) | Left open by the 04:38 session; merged as plan task 1. |
| #571 | `63fb92c` | docs: session handoff — 2026-09-07 04:38 UTC | Same. |
| #572 | `be4f2f1` | docs(skills): add a delegation line to the session-handoff template | The line above is its first use. |
| #573 | `90023c6` | docs(planning): mark the OOM release item and the enumeration half closed | Both had shipped (#523, #531) and still read as open. |
| #575 | `5fe7800` | fix(storage): keep the overlay uncompressed on an mmap-backed store | **Default-config defect.** `EnableEdgeCompression` and `UseMmapSnapshot` are both on by default; a `Snapshot()`/`CompactWAL()` on a reopened store compressed the overlay maps and the read path then returned the compressed entry without the mmap base, so a node with new edges lost its base edges from every adjacency read until reopen. Disk stayed correct. Fixed by never compressing an mmap store's overlay; `CompressEdgeLists` refuses loudly there. The read-path union (Option B) is the follow-up if the uncompressed overlay's memory ever matters. |
| #577 | `86d2649` | feat(storage): check the count chains and the sticky keys on the mmap path | Closes the last two gaps the doc comment listed. Five teeth tests seen red. The old comment named `GetAllEdgeTypes`, which does not exist. |
| #576 | `c0ea68c` | test(storage): run the metamorphic scripts on the mmap path too | All scripts green on mmap; compression off on that driver because of #575. |
| #574 | `00e31f1` | bench(query): measure the admissible load at the depth-cap frontier | No measurable filter cost (6.1 ms ± 16% vs 5.8 ms ± 26%, ten runs). The load at the cap frontier is unconditional, see §6. |

Outside the repo, in `~/.claude`: `ste-agent-hook.py` now denies a definition-less Agent call with no `model` (fork exempt), with `ste-agent-hook-selftest.sh` (11 fixtures; an allow-all stub makes 5 fail). Agent pins: reviewer, tester, api, database, risks, tech-stack haiku→sonnet; architect sonnet→opus. Measurement table appended to `~/.claude/ste-agents.md`.

## 3. Current state

- **`origin/main`**: `00e31f1` (#574).
- **Open PRs**: **#578** `feat: uniqueness-rules registry (ADR 0001)` — 13 commits on `feat/uniqueness-rules-registry`, rebased onto `00e31f1`, both stage reviews (opus) clean after one fix round each; CI in progress at write time. **#579** `docs(planning): close the mmap invariant residue and the coverage gap, record ADR 0001 shipping` — single file; CI in progress. Both left for the user to merge.
- **Branches**: `main`, `feat/uniqueness-rules-registry` (#578), `docs/planning-2026-09-08-closures` (#579), plus this handoff's branch.
- **Worktrees**: `.claude/worktrees/task9-uniqueness` on the #578 branch, clean. Remove after #578 merges. The four other task worktrees were removed before their merges.
- **Uncommitted changes**: none.
- **Gate state on `00e31f1`** (from the merged PRs' own runs and the branch gates): build, vet, `gofmt -s`, golangci-lint v2.13.2 (0 issues), `pkg/storage -short`, `-race` on the invariant, mmap and compression tests, `make contract-guard` (16 contracts, 24 guarding tests after #578).
- **Session ledger**: `.superpowers/sdd/plan-model-tiers/progress.md` (gitignored) holds every ruling, every review verdict, the specialist reports for ADR 0001 (`adr0001-*.md`), the task briefs and the implementer reports. It survives until someone deletes it; the handoff summarises it.

## 4. What is next

1. **Merge #578 and #579 when green** (read by SHA: `gh api repos/dd0wney/graphdb/commits/<sha>/check-runs`). Then remove the `task9-uniqueness` worktree.
2. **graphdb-coord side of ADR 0001** (sibling repo). Before the coord daemon takes a graphdb build with #578: (1) start it with `GRAPHDB_REQUIRED_UNIQUENESS_RULES=claim_for_task=Claim`; (2) `POST /admin/uniqueness-rules` with `{"name":"claim_for_task","label":"Claim","propertyKey":"for_task"}` in `coord-bootstrap.sh`. With an empty required list the new build accepts duplicate claims silently. `~/.graphdb-coord-data` is the live store the ADR names as the counter-example; test the upgrade against a copy of it first.
3. **Option B for #575** only if measured: keep compression on mmap by unioning a compressed overlay with the base at read time. Not queued.
4. **`CC15` consumer-contract row** (V1-spike D9) still waits on `oit-cyber/interrogate`. Note that #578 added **CC16** for the claim behaviour.
5. **Tier policy follow-ups**, outside the repo: run one Workflow `agent()` with `agentType` and no `opts.model` to learn whether the definition's pin applies (task 5 of the plan was skipped because the Workflow tool needs the user's own opt-in); decide whether the `reviewer` pin stays on sonnet, given §6 item 8.

### Surfaced this session, not yet on the planning doc

- The `admissible` frontier load is unconditional (see §6), so the 09-07 handoffs' "one extra load only when `Expand` is set" concern was mis-stated. Whether the unconditional load at the cap is needed when `Expand` is nil is unexamined; the benchmark shows it costs nothing measurable relative to the traversal.
- Stage-2 review minors deferred in #578: the REST/GraphQL 400 for a missing property forwards the storage text (it names the caller's own label and the rule's property key); `parseRequiredUniquenessRules` checks the name shape only, by design and commented.

## 5. Stale assumptions to retire

1. **`SESSION_HANDOFF_2026-09-07-0438Z.md` §4, "The default config leaves compression off and mmap on."** — **FALSE.** `pkg/storage/storage.go:36` sets `EnableEdgeCompression: true` in `DefaultStorageConfig`. Both were on, which is why #575 was a default-configuration defect.
2. **Both 09-07 handoffs, "`admissible` loads a node at the depth cap when a filter is set … one extra `GetNodeForTenant` per edge at the cap frontier, only when `Expand` is non-nil."** — **Wrong on the condition.** `match_path.go:250` (at `63fb92c`) loads before the `Expand` check at `:257`. The load is unconditional at the cap frontier. #574's benchmark therefore measured the filter call only.
3. **`pkg/storage/invariants.go` doc comment (pre-#577), "the sticky global label/type keys that back `GetAllLabels`/`GetAllEdgeTypes`."** — `GetAllEdgeTypes` never existed. `GetAllLabels` reads `gs.nodesByLabel`; on the mmap path `gs.edgesByType` has no reader except the snapshot writer, and on the shard path `FindEdgesByTypeAcrossTenants` reads it. Fixed in #577.
4. **`NEXT_STEPS_2026-06-18.md` open item 2, the ~4-point CI-vs-local coverage gap.** — Does not reproduce at `63fb92c`: identical per-package coverage from CI's own artifact and a local run (wal 83.3, wal/apply 92.9, lsm 86.3, graphql 83.8). #579 records it.
5. **`NEXT_STEPS_2026-06-18.md` items 4 (enumeration half) and 5 (release under OOM).** — Closed by #531 and #523 long before this session; #573 records it.
6. **ADR 0001's letter, "a list of required rule names" and the example `claim-for-task`.** — The implementation uses (name, label) pairs, because an unregistered rule cannot say which writes it covers, and rule names follow the property-key pattern, so the coord rule is `claim_for_task`. The ADR's "Implementation notes" section in #578 records both (rulings R2, R8, R9).
7. **Global `CLAUDE.md` "Model tiers" premise, that a subagent runs on the tier the call names.** — Measured: an unpinned `general-purpose` or `Plan` call ran on Fable, `Explore` on Opus. The hook now enforces `model` on definition-less calls; auto-memory `subagent-tier-measurement` records it.
8. **The plan's own assumption that a sonnet reviewer finds what a haiku reviewer misses.** — Two planted defects (a struct literal dropping `pathOpts`; a truncation no-op) were found by both tiers. Both plants sat one line from a comment naming the invariant. The re-tier rests on policy, not on this evidence.
9. **Auto-memory**: nothing invalidated. One memory added (`subagent-tier-measurement`). `auto-mode-git-blocks-in-graphdb` held again: worktree add/remove, branch delete, rebase, push and merge all ran without a refusal.

## 6. Open questions for the user

1. **Reviewer tier.** Keep `reviewer` on sonnet per policy, or return it to haiku given item 8 above? The cost difference is about 2× per review call (~44k tokens each); the quality difference is unmeasured.
2. **The library-API decision** (carried from 09-07 §6.1; unchanged). Recommendation stands: version the surface on the day `oit-cyber/interrogate` ships against it, with `CC15`.
3. **Workflow probe.** May the next session run one Workflow `agent()` call (typed `worker`, no `opts.model`, a one-line prompt) to settle whether the definition's pin applies? The Workflow tool needs that opt-in in your own words.

## 7. Next-session prompt

See `docs/internals/design/NEXT_SESSION_PROMPT.md`.

## 8. How to use this handoff

1. Read this first; §5 is the section that saves turns, as it was last time.
2. Then `docs/NEXT_STEPS_2026-06-18.md` as amended by #573 and #579.
3. If touching ADR 0001 or graphdb-coord, read the PR #578 body first (the deployment warning and the nine rulings), then `docs/adr/0001-uniqueness-rules-registry.md` § "Implementation notes".
4. The session ledger at `.superpowers/sdd/plan-model-tiers/progress.md` has the full ruling trail if a decision needs re-examination.
