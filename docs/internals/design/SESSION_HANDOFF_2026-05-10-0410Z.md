# Session handoff — 2026-05-10 04:10 UTC

**Date**: 2026-05-10 (single user session, 7 PRs merged + 1 open; coord daemon now live)
**Outgoing model**: Claude Opus 4.7 (1M context)
**Format defined in**: `CLAUDE.md` § "Preparing a new session (handoff convention)"
**Supersedes**: `docs/SESSION_HANDOFF_2026-05-10-0310Z.md` (which itself superseded `0236Z` and `0208Z`).

## TL;DR

graphdb is now coordinating its own development end-to-end: coord daemon running on `localhost:8090` with 15 seeded `:Task` nodes (5 done with closing-PR refs, 10 open), 1 `:Agent`, 1 `:Claim` with full edge wiring. Two design spikes landed (B-lite for atomicity per PR #85, multi-project Option C per PR #87) — implementation is the next session's deliverable.

## What's done this session

In merge order:

| PR | Title | Notes |
|---|---|---|
| #81 | `fix(replica): remove unauth'd /nodes route on replica binaries (audit A8.2)` | Authored by an earlier parallel session; this session shepherded through merge after independent verification (advisor agreed with the diff before knowing it existed). Closes A8.2. |
| #82 | `docs(planning): capture coord-deploy gap findings` | Same parallel session; documents the `COORD_SETUP.md` aspirational vs. reality gap (`/v1/constraints/uniqueness` etc. don't exist). |
| #83 | `docs: session handoff — 2026-05-10 03:10 UTC` | Parallel session's handoff. Recommended F1.1-spike as next; **superseded by this handoff's reordering** — see §6. |
| #84 | `docs(planning): close A4-edges + A8.2; add H4 coord-deploy follow-up` | Reconciled stale planning-doc state for two tasks (A4-edges had been done since PR #70 but never marked). Added H4 sub-track tracking the coord-deploy gap with strategic framing folded in. |
| #85 | `docs(spike): coord-deploy spike — recommend B-lite + concrete rollout (H4)` | Design spike for the operational coord rollout. Surfaces the strategic-vs-pragmatic tension explicitly (§5.2). User accepted **B-lite** (resolver-side `:Claim` uniqueness, ~50-100 LOC, ships PR 1 of the rollout). |
| #86 | `feat(coord): operational MVP — daemon + bootstrap + seed scripts (H4)` | The dogfood lands. `scripts/coord-bootstrap.sh` + `scripts/coord-seed.sh` codify the working flow (admin password file → JWT → API key → REST writes → schema-regenerate → GraphQL reads). `docs/COORD_SETUP.md` rewritten with what actually exists. Surfaced two real bugs (H4.1: REST `/nodes` GET base64-encodes string properties; H4.2: `cmd/server`'s GraphQL has no Mutation type). |
| #87 | `docs(spike): multi-project coord — single tenant + :Project nodes (H4)` | **Open**. User accepted Option C (single tenant + `:Project` typed nodes + `:IN_PROJECT` edges + project-prefixed Task IDs). Per-tenant-per-project rejected because audit A6a forbids cross-tenant edges, breaking `:DEPENDS_ON` across projects. |

## Current state

- **`origin/main` HEAD**: `5ac5d4c` (PR #86). Once #87 merges, main will move to that commit.
- **Open PRs**: only #87 (multi-project spike). MERGEABLE; UNSTABLE per the repo's normal CI pattern.
- **Open branches**: `main`, `docs/multi-project-spike-2026-05-10` (matches #87), and this handoff's branch.
- **Uncommitted changes**: none.
- **Test/lint state**: `go build ./...` clean (verified during PR #86 work). No code changes after that — only docs.
- **Coord daemon**: **running on `localhost:8090`**. Data dir: `~/.graphdb-coord-data`. API key: `~/.graphdb-coord-key` (mode 0600). Long-lived (`expires_in: 0`). Stop via `kill $(lsof -t -i:8090)`. Restart via `bash scripts/coord-bootstrap.sh` (idempotent).
- **Live coord state**: 15 `:Task` nodes (5 done, 10 open), 1 `:Agent` (`agent-next-session-blite`), 1 `:Claim` for `H4-PR1-blite`. Verifiable via:
  ```
  curl -sS -X POST -H "X-API-Key: $(cat ~/.graphdb-coord-key)" -H 'Content-Type: application/json' \
    http://localhost:8090/graphql -d '{"query":"{ tasks { id properties } edges { id type fromNodeId toNodeId } }"}'
  ```

## What's next

**Critical-path queue (re-sequenced from PR #85's original; reflects PR #87's dependency):**

1. **Multi-project implementation PR** (`docs/MULTI_PROJECT_SPIKE_2026-05-10.md` §9 rollout). ~150 LOC bash + migration script + docs update. Adds `:Project` nodes, `:IN_PROJECT` edges, project-prefixed Task IDs. **Lands BEFORE B-lite and skill rewrite** because both depend on project-prefixed `for_task` semantics.
2. **B-lite resolver implementation** (`docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §10 PR 1). ~50-100 LOC in `pkg/graphql/edges_schema.go` — special-case `:Claim` creation with uniqueness check on `for_task`. Ships the dogfood demo's atomic semantics. Was originally PR 1 of the deploy rollout but multi-project preempts.
3. **Skill rewrite** (`work-claim`, `worktree-spawn`, `merge-coordinator`). Bash blocks call non-existent `/v1/constraints/uniqueness` today; rewrite to use real REST + GraphQL surface with project-prefixed Task IDs and the new B-lite uniqueness behavior.
4. **Planning-doc final close-out for H4** (small Shape-A planning-doc-update). Marks H4 done; references the implementation PRs.

**Off-path parallel options (small PRs):**

- **H4.1**: REST `/nodes` GET base64-encodes string properties (`pkg/api/handlers_nodes.go:34` returns `Value.Data` `[]byte`; Go's `json.Marshal` serializes as base64). Fix is type-aware decoding before `respondJSON`. Affects every REST consumer, single-PR cleanup.
- **H4.2**: `cmd/server` uses `pkg/graphql/limits.go`'s queries-only schema generator; the Mutation type in `pkg/graphql/edges_schema.go` is unreachable. Resolution: merge the two generators OR have `cmd/server` use the edges-aware one. Worth a small spike before picking — schema-generation perf may differ.
- **H2** (`requireAdmin` consolidation, ~50-100 LOC, audit-track cleanup).

**Net-new follow-ups not yet on the planning doc** (worth a Shape B addition next session):

- **Multi-team isolation** (per-tenant-per-project migration when 2nd developer joins). Trigger condition stated in `docs/MULTI_PROJECT_SPIKE_2026-05-10.md` §12.
- **Project visibility** (peeking permissions for collaborators). Requires permissions design that doesn't exist yet.
- **Cross-project dashboards** (>10 projects under coord). Belongs to monitoring layer.

## Stale assumptions to retire

For the user's auto-memory and the planning doc:

1. **`docs/NEXT_SESSION_PROMPT.md` (the singleton)** currently points at F1.1-spike as the next task (written by PR #83's session-handoff). **This is now stale.** This handoff overwrites it to point at the multi-project implementation PR (per the new sequencing in §5).

2. **`docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §10 rollout** lists the PR order as "PR 1 (B-lite) → PR 2 (bootstrap) → PR 3 (skill rewrite) → PR 4 (planning-doc update)". With PR 2 already shipped (#86) and the multi-project decision (#87) inserting itself before the rest, the corrected order is: **multi-project → B-lite → skill rewrite → planning-doc close-out**. The spike doc itself doesn't need rewriting (its rationale is intact); the planning doc should reflect the new sequence in the next planning-doc-update.

3. **`docs/COORD_SETUP.md`** describes single-tenant single-project semantics (correctly for now). Once the multi-project PR lands, COORD_SETUP gains a multi-project section explaining `COORD_PROJECT` env var, project-prefixed Task IDs, and cross-project query examples. Listed in `docs/MULTI_PROJECT_SPIKE_2026-05-10.md` §9.

4. **`docs/NEXT_STEPS_2026-05-10.md` §H4** — the original spike's `[ ]` checkboxes for "PR 1 / PR 2 / PR 3 / PR 4" implicitly assume single-project. The sub-tracks already added (H4.1, H4.2) for the bugs found are correct, but the rollout-sequencing line still implies single-project ordering. Single-line correction to make sequencing explicit; bundle into the next planning-doc-update.

5. **`project_graphdb_dogfoods_coord.md` memory** still reads: *"weight planning-doc §H4 toward option B (build the missing API surface)"*. The decision (B-lite, accepted) is now more specific than "option B" — it's *B-lite* (resolver-side `:Claim` uniqueness), with B-full (general `/v1/constraints/uniqueness` API) deferred to a future planning checkpoint. Memory entry could be updated to reflect the specificity, OR left as-is since the framing is still accurate at the level it's written. Low priority.

6. **Operational note (no doc invalidates this; just stating)**: the coord daemon process started by this session is running and will keep running until killed. Memory footprint is small; CPU at idle is near-zero. Leaving it running across sessions is the supported configuration. If the user prefers to stop it between sessions, `kill $(lsof -t -i:8090)` and restart via `bash scripts/coord-bootstrap.sh` next time. The daemon's state (data dir, admin password, API key) survives restarts.

## Open questions for the user

1. **Coord daemon lifecycle**: leave running 24/7 or stop between sessions? Spike recommends "leave running" (small footprint); user can override. If left running across machine reboots, `coord-bootstrap.sh` could be wired into shell login for always-on, but that's out of scope until a real friction signal appears.
2. **PR #87 (multi-project spike) merge timing**: implicit accept by user choosing Option C; merging is a formality. Default: this handoff PR + #87 both merge as part of session close-out.
3. **Should the next planning-doc-update happen before or after the multi-project implementation lands?** Argument for before: the re-sequencing in §6 is captured cleanly in the planning doc, and the implementation PR doesn't have to also do planning-doc reconciliation. Argument for after: the planning-doc-update would mark H4's MULTI sub-track done at the same time as it's checked off, smaller two-PR delta. **Recommend: after** (single planning-doc-update PR that captures both the re-sequencing and the closure).

## Next-session prompt (paste-ready)

The same content is written to `docs/NEXT_SESSION_PROMPT.md` for fresh sessions to grab without finding this dated handoff first.

```
Pick up the multi-project implementation per docs/MULTI_PROJECT_SPIKE_2026-05-10.md §9 (the user-accepted Option C rollout). This must land BEFORE B-lite (PR 1 of the coord-deploy spike) and BEFORE the skill rewrite — both depend on project-prefixed Task IDs.

Pre-flight before starting:

1. Confirm the coord daemon is still running: `curl -sSf http://localhost:8090/health` returns 200. If not, `bash scripts/coord-bootstrap.sh` to restart.
2. Confirm PR #87 (multi-project spike) is merged. If still open, that's the first merge.
3. Read `docs/MULTI_PROJECT_SPIKE_2026-05-10.md` end-to-end (it's the implementation target).

Implementation scope (single PR):

- Modify `scripts/coord-seed.sh`: require COORD_PROJECT env var (auto-detect from `git remote get-url origin`); seed :Project node first; prefix all Task IDs with <project>:; create :IN_PROJECT edge per Task.
- Add `scripts/coord-migrate-add-projects.sh`: one-shot migration for the 15 existing un-prefixed Tasks. Idempotent (detect "schema already migrated" and exit cleanly).
- Update `docs/COORD_SETUP.md`: add multi-project section explaining COORD_PROJECT, schema, cross-project queries.
- Update the existing :Claim node's for_task property: rename "H4-PR1-blite" → "graphdb:H4-PR1-blite".

After multi-project lands, B-lite (PR 1) is next, then skill rewrite, then planning-doc close-out for H4.

End the session via the session-handoff skill.
```

## How to use this handoff

1. Read this first.
2. Then `docs/MULTI_PROJECT_SPIKE_2026-05-10.md` (the implementation target).
3. `CLAUDE.md` is auto-loaded for Claude Code agents — its "Orient first" section points to the canonical reading order.
4. Verify coord daemon is up before starting any implementation work — the multi-project PR is operational, not just docs.
5. The previous session's handoff (`SESSION_HANDOFF_2026-05-10-0310Z.md`) recommended F1.1-spike; that recommendation is now stale per §6. Don't follow it.
