# Session handoff — 2026-05-13 06:15 UTC

**Date**: 2026-05-13 (single continuous session, ~1h, 3 PRs opened, 0 merged; picked up from `SESSION_HANDOFF_2026-05-13-0533Z.md`'s "merge #168, then start C5" critical path).
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `SESSION_HANDOFF_2026-05-13-0533Z.md` (PR #169, still open at this handoff time). That doc's §3–§8 stay accurate for its window; this doc extends through C5.0 + C5.1 + the C6 discovery.

## 1. TL;DR

This session opened C5.0 (CALL/YIELD parser additions, PR #170), C5.1 (parser tests, PR #171 stacked on #170), and a planning-doc reconciliation (PR #172). C6 was sized during the session and explicitly **NOT** started — three load-bearing facts surfaced (gnn/intelligence packages absent on OSS; algorithm signature mismatch with the narrowed Storage interface) that change the planning-doc framing, not just the implementation. C6 is now blocked on a new Decision 6 captured in #172.

## 2. What's done this session

| PR | Title | Notes |
|---|---|---|
| #170 | `feat(query): CALL clause parser additions (C5.0)` | **OPEN at handoff.** +86 LOC, surgical from `origin/archive/gemini-bulk-2026-05-13` (archive HEAD, not `^3` — parser additions live at the merge tip; `^3` only has the 4 query files for C3/C4/C6). Adds `Query.Call` + `CallClause` struct, `TokenCall` + `TokenYield`, `case TokenCall:` in `Parse()` main dispatch, `parseCall()` for dotted names + parenthesized args + optional YIELD. Build/vet/lint clean; existing pkg/query suite passes (~21s). Mergeable. Required CI green where it matters (lint + macOS). |
| #171 | `feat(query): parser tests for CALL clause (C5.1)` | **OPEN, stacked on #170 (base = `feat/c5-call-parser`).** +157 LOC, 9 happy-path + 4 error-path table-driven tests. 13/13 pass on first run under `-race -count=3`. **No correctness bugs surfaced** — unlike C1.1's `findKey`/`findChild` fix, this extraction was clean. Validates that surgical-extraction effort budget should be calibrated by file class (flat parser grammar carries less hidden-invariant risk than B+Tree navigation). |
| #172 | `docs: reconcile NEXT_STEPS Track C state + add C6 storage-type decision` | **OPEN at handoff.** Single-file diff per `planning-doc-update` skill convention. Marks C2/C3.0 done, C4.0/C5.0/C5.1 open, retires Decision 4 (`llm.generate`), adds Decision 6 (`algo.shortestPath` storage-type wiring, four options A/B/C/D). Sequencing graph + critical path updated for the C3/C4/C5 splits. |

**Session total**: 3 PRs opened, 0 merged. Combined with the previous session's #155–#167 + #168/#169 still-open, the broader 2026-05-12/13 arc has produced 21 PRs of which 16 have merged. This session is purely additive — the user has the merge call on all 5 currently-open this-arc PRs (#168 #169 #170 #171 #172).

## 3. Current state

- `origin/main` HEAD: `dc9a209 feat(query): Volcano physical operators (C3.0) — 16/17 lifted, CallOperator deferred (#167)`. Unchanged from session start.
- **Open PRs from this session** (all MERGEABLE):
  - **#170** (C5.0) — base `main`. Stack-bottom.
  - **#171** (C5.1) — base `feat/c5-call-parser` (NOT `main`). Stacked on #170. **See §6 for the merge-order gotcha**.
  - **#172** (planning-doc reconcile) — base `main`. Independent.
- **Open PRs from prior session** (NOT this session — inherited):
  - **#168** (C4.0 Planner) — MERGEABLE, opened by prior session. Disjoint from this session's files (planner.go vs. ast.go/parser.go/etc.).
  - **#169** (handoff 0533Z) — MERGEABLE, opened by prior session.
- **Open PRs predating the C-track arc** (carry-forward from previous handoff, disposition still unresolved): #108, #109, #110, #131, #134, #135, #136, #137, #138, #139, #140 (11 PRs, naming-collision warning still applies: #136/#137 use `A2`/`C1` tag-names that collide with the new planning doc's Track A/C semantics).
- **Open local branches**: many pre-existing (`docs/coord-learning-skills`, `feat/h4.3-*`, `feat/lsa-*`, etc.) — see prior handoff §3. This session added: `feat/c5-call-parser`, `feat/c5.1-parser-tests`, `docs/planning-update-c5-c6-reframe`, `docs/session-handoff-2026-05-13-0615Z` (this branch).
- **Uncommitted changes on main**: none (except `.claude/scheduled_tasks.lock`, harness state — not work-product).
- **Test/lint state**: `go build ./...` + `go vet ./...` + `golangci-lint run ./...` all clean on `main` and on each of this session's branches. Race + `count=3` passes for `pkg/query`.

## 4. Artifacts that survive this session

### `pkg/query/` CALL clause grammar (PRs #170 + #171)

Cypher engine now parses `CALL ... YIELD ...` end-to-end at the AST level. **No consumers** in this session — CallOperator (C3.1) + procedureRegistry (C6) wire it up later. The parser additions are inert until input contains `CALL`/`YIELD`.

### Planning-doc state reconciliation (PR #172)

`docs/NEXT_STEPS_2026-05-13.md` is up to date through this session's discoveries. **Critically**: Decision 6 captures the S1↔algorithms type tension as a user-shaping question, not a hidden implementation choice. The next agent reading the doc cold will see C6 as blocked, not "easy lift, just do it."

### C-track sub-PR discipline (cross-session pattern, now visible)

The "forward-reference deferral" idiom is now consistent across C-track work:

- C1.0 (extract) → C1.1 (tests + Delete contract + correctness fix)
- C3.0 (16/17 ops, `CallOperator` deferred + `valueEvaluator` workaround introduced) → C3.1 (CallOperator + EvalValue interface promotion + retire workaround)
- C4.0 (planner sans `q.Call` block) → C4.1 (un-strip `q.Call` block + planner tests)
- C5.0 (parser additions) → C5.1 (parser tests)

Each split keeps the lift PR's review surface bounded; each follow-up closes a deferred contract. The pattern is load-bearing — the next agent should adopt it for C3.1/C4.1/C6 without re-deriving the rationale.

## 5. What's next

The ranked queue from `docs/NEXT_STEPS_2026-05-13.md` post-#172 merge (assumes #170+#171+#172 land):

### Immediate user-action items

1. **Merge order for #170 + #171** (stacked). Two safe paths:
   - **(a)** Retarget #171's base to `main` **before** merging #170. `gh pr edit 171 --base main`. Then merge #170 with `--delete-branch`, then merge #171 with `--delete-branch`.
   - **(b)** Merge #170 **without** `--delete-branch`; merge #171 with `--delete-branch`; clean #170's branch later.
   - **Do NOT** `gh pr merge 170 --delete-branch` first — that auto-closes #171 with no reopen path (CLAUDE.md § "Known pitfalls" covers this).
2. **Merge #172** (independent, single-file).
3. **Merge #168** (inherited C4.0, prior session). Independent of this session's stack.
4. **Merge #169** (inherited handoff, prior session). Independent.
5. **Optionally merge this handoff PR**.

### Critical-path queue (after merges)

Per #172's updated sequencing graph, post-C5 the queue forks into **{ C3.1, C4.1, C6 }** — three follow-ups that can land in any order. Recommended order:

1. **C4.1** (smallest, ~5-line reinstate) — un-strip `q.Call` block at C4.0's marker comment + planner unit tests. Depends on #168 + #170 merged. Safe-to-pick-up first.
2. **C3.1** (interface promotion + CallOperator) — promote `EvalValue` to `Expression` interface; retire `valueEvaluator` workaround in `physical_plan.go:34-47`; reinstate `CallOperator` consuming the procedureRegistry. Depends on #170 merged (CallClause) + (ideally) C6 merged (procedureRegistry). Could land without C6 if CallOperator returns "no procedure registered" stub.
3. **C6** (procedure registry) — **BLOCKED on Decision 6** (see §7). Effective scope: 1 procedure (`algo.shortestPath`), not 3 (gnn/intelligence absent on OSS).

### Off-path candidates (parallel-eligible)

- **R1 (F4) implementation** — spike landed (#156); ready for implementation. Touches disjoint methods from R2; safe to run parallel via `git worktree` + graphdb-coord skills.
- **R2 (S11) implementation** — spike landed (#157); ready for implementation.
- **Linux CI infra escalation** — three candidates listed in `NEXT_STEPS_2026-05-13.md § Track H`: `concurrency: cancel-in-progress: true` on `test.yml`, matrix-breadth reduction (drop Go 1.23 + 1.24), move race to macOS-only. Single PR each. No user signal on which yet.

### New gaps not yet on planning doc

- **Linter String() inconsistency in `lexer_types.go`** — `TokenCall` / `TokenYield` lack `String()` cases (matches archive). Convention in the file is to have a `String()` case per keyword. Candidate for a C5.1.1 or H6 cleanup pass. Not load-bearing; cosmetic. Flagged in #170 PR description.

## 6. Stale assumptions to retire

This is the highest-leverage section. Use this list to update planning docs / refresh memory in the next session.

### `NEXT_STEPS_2026-05-13.md` C5/C6 scope framing → addressed in PR #172

The original framing was already in `NEXT_STEPS_2026-05-13.md`. **#172 retires it.** Once #172 merges, the planning doc reflects:

- C5 = CALL/YIELD only (not 6 verbs)
- C6 = 1 procedure (`algo.shortestPath`), not 3
- Decision 6 (S1↔algorithms wiring) is added; Decision 4 (`llm.generate`) is retired.

**Until #172 merges, anyone reading `NEXT_STEPS_2026-05-13.md` on `main` will see the old framing.** Next session: if #172 hasn't merged yet, prioritize that merge BEFORE acting on C-track follow-ups, otherwise the wrong shape gets carried forward.

### `CLAUDE.md` & `NEXT_STEPS_2026-05-13.md` re: `pkg/gnn` and `pkg/intelligence`

The C-track-relevant insight: **neither package exists on OSS main**. `pkg/gnn` is documented as "Subset 🟢 — bulk-stash spike-quality"; `pkg/intelligence` was assumed present (Decision 4 implied it was). Both are open-core gaps. CLAUDE.md doesn't currently flag this explicitly; consider a `Known pitfalls` bullet on "packages that the archive imports but OSS lacks" if it bites a third time. Not load-bearing this session because #172 captures it in the planning doc.

### Forward-reference deferral pattern (NEW insight worth surfacing in CLAUDE.md)

C1.0/C1.1, C3.0/C3.1, C4.0/C4.1, C5.0/C5.1 all use the same pattern: lift first (minimal, marker comments where references are stripped) → close gaps in a follow-up (tests, deferred contracts, reinstate stripped blocks). This works *because* the marker-comment discipline makes the reinstate diff trivial.

Suggestion for next session: consider a CLAUDE.md § "C-track surgical-extraction sub-PR pattern" bullet capturing this. It would prevent a future agent from collapsing C3.0+C3.1 (or similar) into a single oversized PR.

### Prior handoff's "default next action"

`SESSION_HANDOFF_2026-05-13-0533Z.md` (PR #169) said default next action is "merge #168, then start C5." This session did NOT merge #168 — per advisor's explicit gate (PR #169's body: "Stop before merging — handoffs are the literal close-out; the user's explicit merge is their session-end signal"). C5 started anyway because its files (ast.go/parser.go/etc.) are disjoint from #168's (planner.go). The directive "merge X first" wasn't load-bearing for forward progress; it was sequencing preference. Next session: take this lesson — disjoint-file work can land in either order, regardless of stated sequencing.

## 7. Open questions for the user

Carry-forward + new:

1. **Stack-merge order for #170 + #171** — see §5 immediate items. Pick (a) or (b); the unsafe path auto-closes #171.
2. **Decision 6 (NEW)** — C6 `algo.shortestPath` storage-type wiring. Four options A/B/C/D in PR #172 + the updated `NEXT_STEPS_2026-05-13.md § Decision points`. Recommendation: **D** is the principled answer (add `ShortestPathForTenant` as a method on `Storage` interface; Track-R-shaped). C is the pragmatic-now choice. Each unblocks C6.
3. **PRs #168 + #169** disposition — both opened by prior session, MERGEABLE, neither merged. Same merge call as #170 + #171 + #172.
4. **PRs #108–#140 disposition** (carried forward from previous handoff — still unanswered): A8.1 step-4 cleanup (#138, #139, #140), LSA improvements with name collisions vs Track C (#136, #137), B-lite REST mirror (#109), per-tenant label index fixes (#110, #108), and others. 11 PRs total. The previous handoff said "Not this session's responsibility" — still applies, but pile-up grows session-to-session. Eventually they need a decision.
5. **Worktree + branch cleanup** — multiple stale local branches; `branch-cleanup` skill applicable after the next merge wave.

## 8. Next-session prompt (paste-ready)

```
Read this first:
  docs/internals/design/SESSION_HANDOFF_2026-05-13-0615Z.md

Then read (in order, only if relevant to your task):
  docs/NEXT_STEPS_2026-05-13.md (especially § Track C "C3.1/C4.1/C6 ordering" + § Decision points 6)
  CLAUDE.md § "Orient first" + § "Known pitfalls" (auto-loaded)
  docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md § Subset 🟢 (only if extracting more from archive)

Default next action — TWO STEPS:

  STEP 1 (user-merge-gated): If PRs #170, #171, #172 (and optionally #168, #169) are still open, the
  user must resolve the stack-merge order for #170+#171 FIRST. The unsafe order auto-closes #171.
  Safe order: `gh pr edit 171 --base main` THEN merge #170 with --delete-branch THEN merge #171
  with --delete-branch. See §5 of the handoff for details. If #172 is still open, prioritize that
  merge so the planning doc reflects current state before acting on follow-ups.

  STEP 2 (post-merge default): C4.1 is the smallest follow-up — un-strip the q.Call block at
  C4.0's marker comment (now that #170 lands Query.Call field) + add planner unit tests. ~30 LOC
  net. Mirrors the C1.0/C1.1, C3.0/C3.1, C5.0/C5.1 split pattern. If user signals on Decision 6,
  C6 also becomes unblocked; otherwise start C4.1.

Validation angle: this session demonstrated the surgical-extraction-+-test-writing discipline scales
DIFFERENTLY across file classes (B+Tree found a bug; flat parser found none). For C3.1's EvalValue
interface promotion + valueEvaluator retirement, the file class is interface-driven type assertion —
expect bugs in the type-assert fallback paths if tests are written first. Plan accordingly.

Pre-flight:
  - confirm Decision 6 has user signal before starting C6 (or note it as still-blocked in your end-of-session handoff).
  - verify `gh pr list --state open` matches §3 of this handoff before assuming merge state.
  - if writing planner tests for C4.1, mirror the table-driven harness in pkg/query/parser_call_test.go (this session's #171 work).

End-of-session: write a session handoff per CLAUDE.md § "Preparing a new session (handoff convention)"
via the session-handoff skill. If the validation angle (file-class differential extraction risk) was
informative, fold a one-line observation into CLAUDE.md's surgical-extraction discipline section.
```

## 9. How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-05-13.md` (§ "Sequencing graph" + § "Decision points" — both updated post-#172).
3. Then `CLAUDE.md` § "Orient first" (auto-loaded; correct pointers after #155 + the H5 fold from PR #160).
4. If picking up C4.1: also read `pkg/query/planner.go` from `feat/c4-planner` (PR #168) for the marker-comment location, and `pkg/query/parser_call_test.go` for the table-driven test pattern.
5. If picking up C6: read § Decision points 6 in `NEXT_STEPS_2026-05-13.md` (post-#172) FIRST. Do not start extraction before Decision 6 has a user-blessed answer.
6. If the prior handoff `SESSION_HANDOFF_2026-05-13-0533Z.md` (PR #169) is still open, its §6 ("Stale assumptions to retire") is also relevant — cross-check before acting on Track H.
