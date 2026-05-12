# Session handoff — 2026-05-12 02:02 UTC

**Date**: 2026-05-12 (single session, ~2h — F3 close-out + A8.1 spike open)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"

---

## TL;DR

F3 milestone fully closed (PR #124 merged, coord:F3.3 → done, planning-doc updated via PR #126). A8.1 spike opened as PR #127 — trilemma framing (rebuild on `cmd/server` / delete + rewrite docs / delete + retain library), awaiting user A/B/C decision before implementation.

---

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #124 (merged) | `docs(compliance): COMPLIANCE.md + F3 audit-regression row (F3 PR-4)` | Inherited in-flight from the 00:10Z handoff. CI classified per textbook UNSTABLE-known-infra (Ubuntu exit-143 + 47-min hang variant + benchmark comment-step 403). Merged with `--delete-branch` at 00:17Z. F3 milestone closed. |
| #123 (closed) | `docs: session handoff — 2026-05-11 11:15 UTC` | Closed as redundant per user direction. Same shape as #115's resolution — superseded by #125 (the 00:10Z handoff that merged first). |
| #126 (merged) | `docs(planning): mark F3 fully done (PR-3a/3b/4 closed)` | Single-file diff (15+/15-) against `docs/NEXT_STEPS_2026-05-10.md` via `planning-doc-update` skill. Track F header flipped to ✅ CLOSED; sequencing graph re-bolded **A8.1** as head of queue; productization-gaps + carry-forward decision points updated. Merged 01:23Z with 2 Ubuntu Go 1.24/1.25 jobs still in the documented hang-tail state — user pre-approved classify-and-merge on docs-only PRs (memory updated; see §6). |
| #127 (open) | `docs(spike): A8.1 — replication standalone-binary disposition` | A8.1 spike output. 189 lines, trilemma framing per advisor pre-write guidance. **Awaiting user decision on Options A/B/C.** Survey surfaced two pivotal findings (see §5). |

Coord state catch-up (via `~/.graphdb-coord-key` API key + `POST /graphql` `updateNode` mutation):

- `graphdb:F3.3` (nid 91) → done, `closing_prs="124"`, `finished_at=2026-05-12T00:17:22Z`. F3 fully closed in coord.

---

## Current state

- `origin/main` HEAD: `4fc4535` (planning-doc update merged).
- Open PRs:
  - **#127** (this session) — A8.1 spike. CI in UNSTABLE/known-infra. Awaiting **user decision** (A/B/C) before merging; the merge captures the decision. Open question §6 also offers a tone-tightening to §6 of the spike doc itself (advisor noted but didn't block).
  - **#108, #109, #110** — parallel agent's H4.x fixes; review-blocked with documented findings. Not touched this session.
- Open local branches: 13 (see `git branch`). Most are squash-merged and ripe for `branch-cleanup` skill cleanup. Out of session scope; flag for next.
- Uncommitted: only `.claude/scheduled_tasks.lock` (system file, untracked since prior sessions).
- Test/lint state: no code changed this session, so no fresh test/lint signal. Last green: `pkg/api/` PASS 37s on the 00:10Z session's PR #124.

---

## What's next

### Critical path (resume here)

1. **PR #127 disposition**. Read `docs/A8_1_SPIKE_2026-05-12.md` (or PR #127's body) and pick **A / B / C**:
   - **A**: Rebuild standalone replication on `cmd/server`. Multi-quarter scope; re-opens A8's deferred items.
   - **B**: Delete legacy binaries + `pkg/admin/upgrade*` orphan + shrink `pkg/replication` + rewrite docs to single-node honest. ~2,000 LOC removal.
   - **C**: Delete binaries + retain `pkg/replication` library. Compromise; carries dead-but-documented infra.
   The spike doc's §7 has the four open questions that block the call. Resolve those before picking.
2. **After A/B/C**: append a "Decision: <pick> 2026-05-XX" line at the top of the spike doc + a one-paragraph rationale in a follow-up commit on `spike/a8.1-replication-go-no-go`. Then merge #127.
3. **A8.1 implementation PR(s)** follow, in the shape the chosen option specifies. Update coord (graphdb:A8.1 node) at implementation close.
4. **After A8.1 closes**: next critical-path item is **S1 spike** (storage interface extraction) per `NEXT_STEPS_2026-05-10.md` §"Sequencing graph".

### Off-path parallel options

- **PR #108 / #109 / #110** (parallel agent). Owned by another agent; review findings already left on each. Wait or escalate to user — don't touch unilaterally.
- **Local branch cleanup**. 13 local branches; most are squash-merged and force-deletable. Run the `branch-cleanup` skill if the next session has slack.
- **`PRODUCTION_QUICKSTART.md` reconciliation**. Surfaced by the A8.1 spike (§2.4): the production-quickstart documents legacy-binary deployment with zero `GRAPHDB_LEGACY_BINARY` / audit-A8 mention. Whichever A/B/C option lands, this doc has to be reconciled. Could be standalone PR pre-decision (add the A8 disclaimer + gate mention as a holding action) or bundle into the implementation PR(s). User's call.

### New gaps surfaced this session

- **`pkg/admin/upgrade*` is a 1,295-LOC orphan**. Imports `pkg/replication` but has zero external consumers (`grep -rn "admin.NewUpgradeManager"` empty). Either scaffolded for a never-built operator-API or removed-from-call-path without deletion. Not gating anything now; will be addressed by whichever A8.1 option lands.
- **Long-tail Ubuntu hang variant** is now documented in memory (`project_ci_red_state_tolerated.md`). The 47-minute Go 1.24 hang on PR #124 + the equivalent on PR #126's Go 1.24/1.25 are the same root-cause as exit-143 (runner can't reap `make test-race` child process) with a different tail shape. The `gh pr checks` `0s` duration is a UI quirk — actual job status is `in_progress` until force-kill.

---

## Stale assumptions to retire

1. **`docs/NEXT_STEPS_2026-05-10.md` Track F (lines 48-55, 121-132)** still showed F3 as 🟡 in-progress at session start. **Corrected** by PR #126: F3 closed (PRs #104/#107/#111/#114/#122/#124); Track F ✅ CLOSED; A8.1 promoted to head of queue. No follow-up needed — the corrections are merged.

2. **`docs/SESSION_HANDOFF_2026-05-12-0010Z.md` §4 "Critical path"** named F3 as the head with A8.1 next. **Corrected** by this session: F3 closed; A8.1 is now the active spike (PR #127). The next-session prompt below reflects this.

3. **User auto-memory** does not yet record the `:Spike → Decision → Implementation` convention this repo uses (A8, A9, F2, F3 all followed it; A8.1 follows it via this session's PR #127). Worth a feedback-type memory note if the user wants the next agent to use the same shape without re-deriving it — but not session-handoff scope.

4. **`docs/PRODUCTION_QUICKSTART.md`** is treated as authoritative production deployment guidance for the legacy binaries by external users, but contradicts the A8 audit's "not for production" verdict. Surfaced by the A8.1 spike survey (§2.4). Listed as an open question for the user; not yet resolved.

---

## Open questions for the user

1. **PR #127 A/B/C disposition**. The spike's whole point. Reading the doc surfaces the four sub-questions in §7 that may need to be answered before the top-level pick (external-user exposure, library retention, NNG-vs-HTTP, `PRODUCTION_QUICKSTART.md` provenance).
2. **§6 tone of the spike doc**. Advisor flagged that the spike's §6 says "A recommendation pre-judged by the spike would be Option B... But the spike defers to the user." That's honest but could read as "picked B, laundered as defer." Two options:
   - **Leave it.** Reasoning is transparent and §3-5 present options on equal footing.
   - **Tighten** §6 last paragraph to: "The spike does not recommend; risks (1) and (2) are inputs only the user can weigh."
   User's call; the advisor said both are defensible.
3. **`PRODUCTION_QUICKSTART.md` reconciliation timing**. Standalone PR pre-decision (add A8 disclaimer + gate mention) or bundle into A8.1 implementation PR(s)?

---

## Next-session prompt (paste-ready)

```
Resume by closing out PR #127's A/B/C decision, then start A8.1
implementation:

1. Read docs/A8_1_SPIKE_2026-05-12.md (PR #127). Resolve the four
   open questions in §7 (external-user exposure, library retention,
   NNG-vs-HTTP, PRODUCTION_QUICKSTART.md provenance) BEFORE picking
   A/B/C. The spike does not pre-pick; the user does.

2. After deciding: append a "Decision: <A|B|C> picked <DATE>" line at
   the top of the spike doc + a one-paragraph rationale in a follow-up
   commit on spike/a8.1-replication-go-no-go. Merge #127 with
   --delete-branch (UNSTABLE per known-infra pattern is fine — same
   classify-and-merge precedent set by #126 for docs-only PRs).

3. Then mark graphdb:A8.1 in coord with the decision (use the
   ~/.graphdb-coord-key + POST /graphql updateNode mutation; node ID
   surfaceable via `task` query). Run planning-doc-update skill
   against docs/NEXT_STEPS_2026-05-10.md Track A to record A8.1
   started/decided.

4. Open A8.1 implementation PR(s) in the shape the chosen option
   specifies. Reference docs/A8_1_SPIKE_2026-05-12.md in the PR body
   the way A4-edges referenced A4, A8.2 referenced A8, and F3 PR-3a/
   3b/4 referenced F3_COMPLIANCE_API_DESIGN.md.

5. Off-path opportunistic work: branch-cleanup (13 stale local
   branches; most are squash-merged). PR #108/#109/#110 are owned by
   another agent — don't touch unilaterally.

Pre-flight:
1. Read docs/SESSION_HANDOFF_2026-05-12-0202Z.md (this file).
2. Read docs/A8_1_SPIKE_2026-05-12.md §6-7 (decision criteria + open
   questions). Then docs/A8_REPLICATION_TENANCY_DESIGN.md §5 (Q5/Q7)
   for the holding-action gate that A8.1 is now retiring.
3. Read docs/NEXT_STEPS_2026-05-10.md §"Sequencing graph" + §"Track
   A" — A8.1 is now the bolded head of the critical path, S1 next.
4. Coord daemon on :8090. Token at ~/.graphdb-coord-key (X-API-Key
   header for REST + GraphQL). Schema is generic — Task type exposes
   id/labels/properties only, all semantic fields live in
   properties as JSON. updateNode replaces properties wholesale,
   not merges.

Validation angle: the A8.1 decision is the first real test of the
docs-only PR classify-and-merge precedent set by #126 — the
deletion option (B) is mostly docs + dead-code removal, so the
same merge discipline applies. Watch for code-touching commits and
switch back to wait-for-full-settle on those.

End the session via the session-handoff skill.
```

---

## How to use this handoff

1. Read this file first.
2. Then read `docs/NEXT_SESSION_PROMPT.md` (singleton; this handoff overwrites it — content matches §8 above).
3. Then read `docs/NEXT_STEPS_2026-05-10.md` §"Track A" + §"Sequencing graph".
4. If picking up A8.1 (which is the natural next): also read `docs/A8_1_SPIKE_2026-05-12.md` (the PR #127 doc) and `docs/A8_REPLICATION_TENANCY_DESIGN.md` §5.
