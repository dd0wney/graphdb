# Session handoff — 2026-05-10 04:36 UTC

**Date**: 2026-05-10 (single-PR session — picked up multi-project implementation per the prior handoff's `NEXT_SESSION_PROMPT.md`; opened PR #89; nothing merged yet)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `docs/SESSION_HANDOFF_2026-05-10-0410Z.md`

## TL;DR

Multi-project coord schema (Option C) is implemented in PR #89 (open). Coord daemon now hosts a `:Project { id: graphdb }` node, all 15 `:Task.id`s are project-prefixed (`graphdb:H4-PR1-blite` etc.), 15 `:IN_PROJECT` edges link Tasks to their Project, and the existing `:Claim` is updated to match. Verified live including conflict-guard refusal and multi-project safety (a fake `syntopica:fake-task` was correctly left untouched). PR #89 is the next thing to merge; once it's in, B-lite resolver implementation is the next critical-path PR.

## What's done this session

| PR | Title | Notes |
|---|---|---|
| #89 | `feat(coord): multi-project schema — :Project + :IN_PROJECT edges (H4)` | **Open.** Implements `docs/MULTI_PROJECT_SPIKE_2026-05-10.md` §9. New `scripts/coord-migrate-add-projects.sh`, modified `scripts/coord-seed.sh`, new "Multi-project coord" section in `docs/COORD_SETUP.md`. Migration verified live against the daemon: 15 Tasks renamed, 15 :IN_PROJECT edges, 1 :Claim renamed, idempotent re-run produces 0 writes, conflict guard refuses on COORD_PROJECT mismatch, foreign-project Task left untouched in multi-project safety test. ~470 LOC added (mostly bash + python helpers + docs). |

No PRs merged this session. The session's work is the open #89.

## Current state

- **`origin/main` HEAD**: `2fd0ecc` (PR #88, prior session's handoff). Unchanged this session.
- **Open PRs**: #89 (this session's deliverable). CI started at 04:35Z; expected `UNSTABLE`-mergeable per the repo's normal infra pattern (Ubuntu test-race exit 143 + benchmark comment-step permission). Net-new failures would be unexpected because the diff is bash + docs only — no Go changed, `go build ./...` clean locally.
- **Open branches**: `main`, `feat/coord-multi-project-2026-05-10` (matches #89), and this handoff's branch `docs/session-handoff-2026-05-10-0436Z`.
- **Uncommitted changes**: none.
- **Coord daemon**: still running on `:8090` (uptime ~28 min at session-end), now in **multi-project state** — `:Project{graphdb}` node 20 + 15 prefixed `:Task` ids + 15 `:IN_PROJECT` edges. Verifiable via:
  ```bash
  curl -sS -X POST -H "X-API-Key: $(cat ~/.graphdb-coord-key)" -H 'Content-Type: application/json' \
    http://localhost:8090/graphql \
    -d '{"query":"{ projects { id properties } tasks { id properties } edges { id type fromNodeId toNodeId } }"}'
  ```
  Stop via `kill $(lsof -t -i:8090)`. Restart via `bash scripts/coord-bootstrap.sh` (idempotent).

## What's next

**Critical-path queue (re-sequenced from prior handoff after multi-project lands):**

1. **B-lite resolver implementation** — PR 1 of `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §10. ~50-100 LOC in `pkg/graphql/edges_schema.go`: special-case `:Claim` creation with uniqueness check on `for_task`. Multi-project just landed (PR #89), so `for_task` values are now project-prefixed (`graphdb:H4-PR1-blite`); the resolver's uniqueness check operates on the prefixed string verbatim — no special multi-project handling needed at the resolver level.
2. **Skill rewrite** (`work-claim`, `worktree-spawn`, `merge-coordinator`). Bash blocks call non-existent `/v1/constraints/uniqueness` today; rewrite to use real REST + GraphQL surface with project-prefixed Task IDs and the new B-lite uniqueness behavior. Auto-detect `COORD_PROJECT` from git remote in each skill (consistent with the seed/migration scripts).
3. **Planning-doc close-out for H4** (small Shape-A `planning-doc-update`). Marks H4 done, references the implementation PRs (#86, #87, #89, B-lite, skill rewrite). The §10 rollout-sequencing note in `COORD_DEPLOY_SPIKE_2026-05-10.md` should also be reflected.

**Off-path parallel options (small PRs):**

- **H4.1**: REST `/nodes` GET base64-encodes string properties. Fix is type-aware decoding before `respondJSON` in `pkg/api/handlers_nodes.go:34`. Affects every REST consumer; the migration + seed scripts both currently work around it via Python base64-decode helpers — fixing the bug would let those scripts shed the workaround.
- **H4.2**: `cmd/server` uses `pkg/graphql/limits.go`'s queries-only schema generator; the Mutation type in `pkg/graphql/edges_schema.go` is unreachable. Worth a small spike before picking — schema-generation perf may differ. Tracking issue: blocks "native traversal queries" mentioned in `COORD_SETUP.md`'s multi-project section (e.g., `{ project(id: "graphdb") { tasks { ... } } }`).
- **H2** (`requireAdmin` consolidation, ~50-100 LOC, audit-track cleanup).

**Net-new follow-ups not yet on the planning doc** (worth Shape-B addition next planning checkpoint):

- **Resolve the H4.1 base64 bug** before B-lite, OR after — depends on whether B-lite's resolver code path encounters the same bug. If B-lite reads `for_task` via REST, the workaround needs replicating. If it reads via the GraphQL resolver (in-process, no JSON round-trip), the bug is invisible. Worth checking during B-lite's design.

## Stale assumptions to retire

For the user's auto-memory and the planning doc:

1. **`docs/NEXT_SESSION_PROMPT.md` (the singleton)** currently points at multi-project implementation as the next task (per the prior session's handoff). **This is now stale** — multi-project is shipped (in PR #89). This handoff overwrites it to point at B-lite.

2. **`docs/COORD_SETUP.md`** had a single-project schema and lacked any mention of `COORD_PROJECT`. PR #89 corrects this — the new "Multi-project coord" section + updated schema reference are now the source of truth. No further doc-update needed *until* the next session (B-lite) lands its operational changes.

3. **`docs/NEXT_STEPS_2026-05-10.md` §H4** still implies a single-project rollout sequence. After PR #89 lands, the next planning-doc-update should:
   - Mark the multi-project sub-track done, with `(#89)` reference.
   - Update the §H4 rollout to: ✓ multi-project (#89) → next: B-lite → skill rewrite → close-out.
   - The original PR 1/2/3/4 labels in the spike doc no longer match what shipped; sub-bullet correction is enough.

4. **`docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §10 rollout** lists "PR 1 (B-lite) → PR 2 (bootstrap) → PR 3 (skill rewrite) → PR 4 (planning-doc update)". After multi-project, the sequence is **multi-project (#89) → B-lite → skill rewrite → planning-doc close-out**. The spike doc itself doesn't strictly need rewriting (its rationale is intact); the planning-doc-update will reflect the new sequence.

5. **`docs/MULTI_PROJECT_SPIKE_2026-05-10.md` §9** describes the rollout as future work. After PR #89 merges, all five bullets in §9 are done. The spike doc remains useful as historical design rationale.

6. **The hardcoded `TASKS=(...)` list in `scripts/coord-seed.sh`** is graphdb-specific. The new docs (per advisor feedback) explicitly say: each project's repo carries its own coord-seed.sh with its own task list. Anyone adding syntopica or another project later does so by writing a parallel script in *that* repo, pointed at the same daemon via `GRAPHDB_COORD_URL`. This is the only honest read of the current code; don't let the single-script appearance in this repo mislead.

## Open questions for the user

1. **PR #89 merge timing**: CI is running at session-end. The diff is bash + docs only so net-new failures would be unexpected; expected `UNSTABLE`-mergeable. Default: merge after CI settles into the known-infra-tolerated pattern. The next session can either start by merging #89 (if still open) or by reading the planning-doc to confirm it landed.
2. **B-lite vs H4.1 ordering**: H4.1 (REST base64 bug) might be worth fixing *before* B-lite if B-lite's resolver code path needs to read string properties from `:Claim`. This is a small upfront investigation in B-lite's design phase, not a blocking question for this handoff.
3. **Should `coord-seed.sh` migrate to per-project task lists** (e.g., move TASKS to a separate `scripts/coord-seed-tasks.sh` sourced via shell, so multiple projects can share the seed framework but specify their own lists)? Or is the "each project carries its own copy" pattern simpler? Defer until syntopica or a second project actually needs it; speculative refactor today.

## Next-session prompt (paste-ready)

The same content is written to `docs/NEXT_SESSION_PROMPT.md`. Fresh sessions can grab it without finding this dated file first.

```
Pick up B-lite — PR 1 of docs/COORD_DEPLOY_SPIKE_2026-05-10.md §10 rollout. ~50-100 LOC in pkg/graphql/edges_schema.go: special-case :Claim creation with a uniqueness check on the for_task property.

Pre-flight before starting:

1. Confirm coord daemon healthy: `curl -sSf http://localhost:8090/health` returns 200. If not, `bash scripts/coord-bootstrap.sh` to restart.
2. Confirm PR #89 (multi-project schema) is merged. If still open, that's the first merge — coord state on disk has already been migrated, so the merge is just code-on-main alignment.
3. Read docs/COORD_DEPLOY_SPIKE_2026-05-10.md §5.2 and §10 PR 1 for design context.
4. Look at the existing edge-mutation resolver in pkg/graphql/edges_schema.go for the pattern.

Implementation scope (single PR):

- Special-case :Claim in the create resolver path so for_task uniqueness is checked atomically.
- Uniqueness predicate: query "any existing :Claim with for_task=X" before allowing the create. for_task values are now project-prefixed strings (graphdb:H4-PR1-blite) — operate on the prefixed string verbatim, no special multi-project handling needed at the resolver layer.
- Return a typed error on conflict; HTTP 409-equivalent in GraphQL.
- Test path: try to create two :Claims for the same for_task in quick succession; second must fail.
- During design, check whether B-lite's resolver code path encounters the H4.1 base64 bug (REST /nodes GET base64-encodes string properties). If yes, fix or work around in this PR; if no (because resolver is in-process), note in the PR body and leave H4.1 as a separate cleanup.

After B-lite, the skill rewrite (work-claim, worktree-spawn, merge-coordinator) is next, then planning-doc close-out for H4.

End the session via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/NEXT_STEPS_2026-05-10.md` (esp. §H4).
3. `CLAUDE.md` is auto-loaded for Claude Code agents — its "Orient first" section points to the canonical reading order.
4. If picking up B-lite: read `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §5.2 + §10 PR 1, then `pkg/graphql/edges_schema.go` to understand the existing resolver shape.
5. The previous session's handoff (`SESSION_HANDOFF_2026-05-10-0410Z.md`) recommended multi-project as the next task; that recommendation is now consumed by PR #89. Don't follow it.
