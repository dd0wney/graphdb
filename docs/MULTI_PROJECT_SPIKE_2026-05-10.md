# Spike: multi-project coord — single tenant + `:Project` typed nodes — 2026-05-10

**Status**: Spike. Decision **already accepted** (Option C, user 2026-05-10) — this doc captures the design concretely so the implementation PR has a clear target.
**Companion**: `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` (the deploy spike that delivered single-project MVP).
**Tracked under**: planning doc §H4 — sub-track for multi-project, distinct from §H4.1/H4.2 which are bug fixes.

---

## 1. Problem

Single developer with multiple personal projects (graphdb, syntopica, future repos). Each project has its own planning doc, task ID namespace, and code repo. Coord needs to:

1. **Coordinate work within a project** (the existing single-project case).
2. **Express dependencies across projects** (e.g., `syntopica:auth-spike` blocks on `graphdb:F3` shipping). The user's stated framing: *"one project might depend on another project; or multiple projects might depend on another project."*
3. **Let one query traverse the full dependency graph** regardless of which projects the upstream tasks live in. (`:DEPENDS_ON*` reachability is the killer query — and the dogfood demo for graphdb's traversal-first design.)

The single-project MVP shipped in PR #86 doesn't do (2) or (3). This spike resolves how.

## 2. Why per-tenant-project doesn't work

The obvious first instinct is "each project = one tenant." graphdb already has multi-tenancy infrastructure; tenant `default`, `syntopica`, `graphdb` would be three coexisting graphs in one daemon.

**This conflicts with audit A6a.** `pkg/storage`'s `*ForTenant` enforcement (PR #20) returns `ErrNodeNotFound` for any cross-tenant edge — for security between distrusting tenants. So `syntopica:auth-spike -[:DEPENDS_ON]-> graphdb:F3` fails at the storage layer because the `from` and `to` nodes belong to different tenants.

Verified live: `curl -X POST -H "X-Tenant-ID: syntopica" /edges -d '{"from_node_id":<graphdb-task>,"to_node_id":<syntopica-task>,...}'` → `404 Not Found`.

Workaround patterns that *would* preserve per-tenant + cross-project deps:

- **A "meta" tenant with denormalized Task copies**: every Task is duplicated into `meta` tenant + cross-project `:DEPENDS_ON` edges live there. Sync layer maintains. **Engineering: high; correctness: hard (sync drift); dogfood story: weakened (the cool query lives in a contrived second graph).**
- **Cross-tenant edges via property pointers**: replace `:DEPENDS_ON` edges with a property `depends_on: ["graphdb:F3"]` on the Task node. **Loses graph-traversal power entirely; back to "implement reachability in app code."**

Both fall short for the same reason: the security primitive (tenant) is wrong-shaped for the organizational concern (project). Tenancy was designed for *isolation between distrusting tenants* — and that's not what multi-project coord needs for one developer.

## 3. Decision: Option C — single tenant + `:Project` typed nodes

Approved by user 2026-05-10.

```
:Project { id, name, repo_url?, description? }
:Task    { id, track, status, ... }                    # unchanged
:Agent   { id, host, started_at, intent? }             # unchanged
:Claim   { id, started_at, expected_duration, for_task } # unchanged

(:Task)  -[:IN_PROJECT]-> (:Project)
(:Task)  -[:DEPENDS_ON]-> (:Task)                      # works across projects natively
(:Agent) -[:HOLDS]->  (:Claim) -[:FOR]-> (:Task)       # unchanged
(:Task)  -[:CLOSED_BY]-> (:PR)                          # unchanged
```

Conventions:

- **Every `:Task` MUST have exactly one `:IN_PROJECT` edge to a `:Project`.** Enforced by the seed script + work-claim skill (later by the resolver if B-lite gets extended to Tasks).
- **Task `id` property is project-prefixed**: `graphdb:H4-PR1-blite`, `syntopica:auth-spike`. The prefix is canonical; queries can split on `:` for project disambiguation without the `:IN_PROJECT` traversal when speed matters.
- **Project `id` matches the source repo's slug**: `graphdb`, `syntopica`. The work-claim skill auto-detects from `git remote get-url origin` to avoid manual config.
- **Cross-project deps don't need any special syntax** — same `:DEPENDS_ON` edge as intra-project. The resolver doesn't care which project the endpoints live in.

## 4. Why C beats A and B

| Concern | A (per-tenant) | B (project property) | C (`:Project` node) |
|---|---|---|---|
| Cross-project `:DEPENDS_ON` | ❌ Forbidden | ✅ Native | ✅ Native |
| Project-scoped queries | ✅ Tenant header | ⚠️ Filter on property (no native traversal) | ✅ `:IN_PROJECT` traversal |
| Project-level metadata | ✅ Tenant store | ⚠️ Duplicated on every Task | ✅ Lives on `:Project` node |
| Strong isolation | ✅ Storage-enforced | ❌ None | ❌ None (by-convention) |
| Engineering cost | High (sync layer) | Tiny | Tiny (one-time `:Project` seeding) |
| Dogfood story | Weak (denormalized) | OK | **Strong** (every dep is a graph edge) |

The decisive factor is **the dogfood story**. Per the user's framing for §H4: graphdb's positioning rests on *"the critical path is exactly why graphdb is the perfect coordination tool for multiple Claude Code agents."* Making cross-project dependencies a property scan (B) or a denormalized second graph (A) walks back from that. C makes them first-class graph traversals — exactly the workload graphdb's design is for.

The cost of C is "no project-level access control." That's accepted: multi-project coord for one developer is *organization*, not isolation. When multi-team isolation becomes a real constraint (multiple developers, each owning specific projects), the migration is "convert each `:Project` node into a tenant" — well-defined, scoped, and explicitly tracked as a future sub-track here.

## 5. Migration: existing 15 `:Task` nodes from PR #86

PR #86's seed left 15 `:Task` nodes in the `default` tenant with un-prefixed IDs (`H4-PR1-blite`, `F1.1-spike`, etc.). Migration to the C schema:

1. Create one `:Project` node: `{id: "graphdb", name: "graphdb", repo_url: "https://github.com/dd0wney/graphdb"}`.
2. For each existing `:Task`: rename `id` from `H4-PR1-blite` → `graphdb:H4-PR1-blite` (in-place property update via `PUT /nodes/{id}`).
3. For each existing `:Task`: create one `:IN_PROJECT` edge from the Task to the `graphdb` `:Project` node.
4. Update `scripts/coord-seed.sh` to require `COORD_PROJECT` env var (or auto-detect from `git remote`); seed `:Project` first, then prefix all Task IDs, then create `:IN_PROJECT` edges as it goes.

Migration is **idempotent and one-shot**: re-running the migrated seed is the no-op shape PR #86 already established.

The Claim from PR #86 (`claim-h4-pr1-blite-2026-05-10`, `for_task: "H4-PR1-blite"`) needs its `for_task` property updated to `graphdb:H4-PR1-blite` for consistency. That's one `PUT /nodes/6`.

## 6. Skill implications

The four parallel-agent skills land changes:

- **`work-claim`**: auto-detect `COORD_PROJECT` from `git remote get-url origin` (parse repo name from the URL), prefix the Task ID with `<project>:` before creating/looking-up. Look-before-leap uniqueness check (option A) reads `:Claim` nodes filtered by `for_task` matching the project-prefixed ID.
- **`worktree-spawn`**: pass `COORD_PROJECT` to `work-claim`; otherwise unchanged.
- **`merge-coordinator`**: traverses `:DEPENDS_ON` regardless of project; the cross-project query naturally falls out of single-tenant traversal.
- **`integration-checkpoint`**: unaffected; doesn't touch coord at all.

Auto-detect heuristic for `COORD_PROJECT`:
```bash
PROJECT=$(git remote get-url origin 2>/dev/null \
  | sed -E 's|^.*[:/]([^/]+/[^/]+)(\.git)?$|\1|; s|.*/||')
# graphdb         (from https://github.com/dd0wney/graphdb)
# syntopica       (from git@github.com:dd0wney/syntopica.git)
```

Override via `COORD_PROJECT=foo` env var for cases where git-remote inference is wrong (forks, monorepo subdirs).

## 7. Cross-project query examples

The whole point of C is that these are single graph traversals, not multi-step app code:

**"What's blocking syntopica's `auth-spike`?"**
```graphql
{
  task(id: "syntopica:auth-spike") {
    id
    dependencies {           # via :DEPENDS_ON traversal
      id
      status
      project { id }         # via :IN_PROJECT
    }
  }
}
```
(Schema doesn't expose `dependencies`-as-resolver-field today; the GraphQL mutation/query surface needs extension. Tracked under H4.2's "merge the two schema generators" follow-up.)

**"All open tasks across all projects, grouped by project"**:
```graphql
{
  projects {
    id
    name
    tasks(status: "open") { id track }
  }
}
```

**"Stale claims older than 24h, regardless of project"**:
```graphql
{
  claims(startedBefore: "2026-05-09T04:00Z", notClosed: true) {
    id
    forTask { id project { id } }
    holder { id host }
  }
}
```

These queries don't exist today (see H4.2 + the schema-with-mutations/limits split). Implementing them is out of scope for this spike but the data model under C makes them **expressible** — that's the design contract.

## 8. Open questions

Lower-stakes than the deploy-spike's questions; defaults are likely fine:

1. **Multi-label vs `:Project` edge**: Could every Task be `:Task:graphdb` instead of `:Task -[:IN_PROJECT]-> :Project`. Multi-label is simpler but loses project-level metadata (no place for `repo_url`, `description`, `owner`) and produces awkward auto-generated GraphQL queries (`graphdbs` plural). **Decision**: edge-based. The metadata case wins.
2. **Migration timing**: Right after this spike's PR or later? **Recommend**: implementation PR is small enough (~80 LOC bash + maybe `scripts/coord-migrate-add-projects.sh`); land it next session as one PR alongside the schema change.
3. **Should `:Project` nodes have `:DEPENDS_ON` edges** (e.g., `syntopica :DEPENDS_ON graphdb` as a project-level dep)? **Recommend**: skip for now. Project deps are derivable from the union of Task-level deps; adding project-level adds a "two sources of truth" risk. Revisit if a real query needs it.
4. **What if a Task moves between projects** (rare — refactoring a feature into a different repo)? **Recommend**: delete + recreate. The new Task ID gets the new project prefix; old Task gets `:CLOSED_BY` edge to whatever PR moved it. No in-place re-projection.
5. **Coord schema regenerate after migration**: yes, required. Add to the migration script's last step.

## 9. Rollout sequence

Single PR is sufficient. ~80-150 LOC bash + ~50 LOC docs.

- `scripts/coord-bootstrap.sh`: unchanged.
- `scripts/coord-seed.sh`: 
  - Require `COORD_PROJECT` env var (auto-detect from git remote).
  - Seed `:Project` node for `COORD_PROJECT` if absent.
  - Prefix all Task IDs with `<project>:`.
  - Create `:IN_PROJECT` edge for each Task.
- `scripts/coord-migrate-add-projects.sh`: one-shot migration for the 15 existing un-prefixed Tasks. Detects "schema already migrated" and exits cleanly if re-run.
- `docs/COORD_SETUP.md`: add multi-project section explaining `COORD_PROJECT`, the schema, cross-project queries.
- `docs/NEXT_STEPS_2026-05-10.md` §H4: check off the multi-project sub-track.

Out of scope (separate follow-ups under H4):

- **GraphQL `dependencies` resolver field** for `:Task`. Needs schema-generator merge first (H4.2).
- **`work-claim` skill update**: depends on multi-project schema being in place; sequenced after this PR.
- **B-lite (`:Claim` uniqueness in resolver)**: independent; can land in any order relative to multi-project. Probably belongs after multi-project so the resolver knows about project-prefixed `for_task`.

## 10. Recommendation

**GO** — the design is settled (user accepted Option C 2026-05-10), implementation cost is small, and the dogfood story strengthens with each piece. Ship as a single PR next session.

**Sequencing note**: this PR should land **before** the skill rewrite (§H4 PR 3) and **before** B-lite (§H4 PR 1), because both depend on the project-prefixed Task ID convention. The earlier ordering in `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §10 implicitly assumed single-project — update the rollout in the next planning-doc-update to put multi-project first.

## 11. Open decisions for the user

Per §8, defaults are likely fine; spike recommends accepting them all:

1. Multi-label vs edge for project linkage? **Recommend: edge** (`:IN_PROJECT`).
2. Migration timing? **Recommend: next session, single PR with the schema change.**
3. Project-level `:DEPENDS_ON`? **Recommend: skip; revisit if a real query needs it.**
4. Cross-project Task moves? **Recommend: delete-and-recreate, no in-place re-project.**
5. Schema regenerate in migration script? **Recommend: yes, last step.**

If you accept all five recommendations, the implementation PR is fully scoped and unambiguous.

## 12. Future sub-tracks tracked here for visibility

These are *not* this spike's scope but worth naming so they land in the planning doc when promoted:

- **Multi-team isolation**: when a second developer joins or a project gains team-specific tasks, migration to per-tenant-per-project becomes the right move. Well-defined operation: convert each `:Project` node to a tenant, move its `:Task`/`:Agent`/`:Claim` into that tenant, replace cross-project `:DEPENDS_ON` with the meta-tenant denormalization (option A from §2). Rough scope: one spike + 2-3 implementation PRs. **Trigger**: second-developer use case appears.
- **Project visibility**: read-only project view shared with collaborators (e.g., a non-coord-owner peeking at "what's syntopica working on?"). Belongs to a permissions design that doesn't exist yet (graphdb has admin/user roles only). **Trigger**: external visibility request.
- **Cross-project dashboards**: "What's blocking the most downstream work right now?" needs traversal queries that aggregate across projects. Out of scope for the data model; in scope for whatever monitoring layer wraps coord. **Trigger**: more than ~10 projects under coord.

## See also

- `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` — the operational spike that shipped single-project MVP.
- `docs/COORD_SETUP.md` — operator-facing how-to (will be updated when this spike's PR lands).
- `docs/NEXT_STEPS_2026-05-10.md` §H4 — planning-doc tracking.
- `project_graphdb_dogfoods_coord.md` (memory) — strategic framing the user articulated for §H4.
