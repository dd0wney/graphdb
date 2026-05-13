# Project guide for Claude Code agents

Repo-specific instructions to make agent iteration cheap. Loaded automatically by Claude Code when working in this directory. Pairs with the user's global `~/.claude/CLAUDE.md`; this file should only contain things that don't apply elsewhere.

Keep this file under ~200 lines. If something only matters once a quarter, it doesn't belong here.

## Orient first — read these before doing substantive work

In this order:

1. **`docs/CAPABILITIES_2026-05-10.md`** — what exists in `pkg/` + `cmd/` + the enterprise repo, with maturity tags. Read this before claiming anything is "missing" or "scaffolding only" — coarse grep is misleading because the codebase is large.
2. **`docs/NEXT_STEPS_2026-05-13.md`** — current planning checkpoint. Critical-path queue + already-tracked work. The header date is the source of truth; if a newer `NEXT_STEPS_<DATE>.md` exists, that supersedes. (Predecessor: `NEXT_STEPS_2026-05-10.md`, historical only.)
3. **`docs/internals/design/AUDIT_*_2026-05-06.md`** — multi-specialist audits (architecture, security, performance, code-quality). Most of the current work derives from these. Skim only if your task touches the named area.

If the user names a task by track letter (`A4-edges`, `H2`, `F1.1`, `S1`, etc.) or audit-finding ID (`CRIT-1`, `HIGH-2`), that resolves via `NEXT_STEPS_<DATE>.md` or the audit docs. Don't guess what these mean — look them up.

## Open-core: a sibling private repo exists

graphdb is dual-repo:

- `dd0wney/graphdb` (this repo, public): OSS core + plugin/license framework.
- `dd0wney/graphdb-enterprise` (private): `.so` plugin implementations.

Before claiming a feature is "missing" or that a `pkg/plugins`-style interface is "scaffolding," check what's in the enterprise repo via `gh api repos/dd0wney/graphdb-enterprise/contents`. The OSS-only inspection lies — `prometheus-metrics` (advanced monitoring) and `r2-backup` already ship there; 4 more named in `docs/ENTERPRISE_PLUGINS.md` are unbuilt but documented.

This caught the productization-gaps PR (#71) — corrected in `docs/CAPABILITIES_2026-05-10.md`. Don't repeat the pattern.

## Repo layout quick reference

- `pkg/` — 37 packages. `storage` and `query` are the largest (~50/30 test files); see `CAPABILITIES_2026-05-10.md` for the per-package map.
- `cmd/` — 29 binaries. `graphdb` is the main server; `cmd/benchmark*` are 13 separate exercisers (proliferation; consolidation might come later).
- `workers/graphdb-client/` — first-party TypeScript client for Cloudflare Workers. Only non-Go SDK that ships.
- `docs/` — heavy on `AUDIT_*.md` and `NEXT_STEPS_*.md`; sparse on customer-facing onboarding (a productization-gap, see `CAPABILITIES_2026-05-10.md`).

## Common workflows

### Build, test, lint at CI's surface

```bash
go build ./...
go vet ./...
go test ./pkg/<area>/ -short -timeout 90s -count=1
go test -race ./pkg/storage/ -count=3 -timeout 300s   # for storage/concurrency changes
golangci-lint run ./...                                # MUST pass before PR; CI cap is "same issue × 3"
```

`/preflight` runs an equivalent set; `/review` checks the diff before commit.

### Pre-PR

Per the user's global `CLAUDE.md`, run `/review` then `/preflight` before opening a PR.

Always pass `--delete-branch` to `gh pr merge` so squash-merged branches don't accumulate as stale local references — that debt was the H3 task this repo just closed (#69).

### Atomic-commit convention

PRs are squash-merged with `(#NN)` suffix on main (see `git log --oneline | head`). Multi-commit PRs are fine while in flight — squash collapses them. Within a PR, prefer the structural / lock-grain / bench split that A4 (#67) and A4-edges (#70) used: each commit self-consistent, last commit captures the verification numbers.

## Idioms specific to this repo

### Tenant scoping (`*ForTenant` convention)

Every public storage method has a `Foo` (tenant-blind, legacy) and `FooForTenant` (tenant-strict) pair. New code goes through `FooForTenant`. Cross-tenant lookups return `ErrNodeNotFound` / `ErrEdgeNotFound` (NOT a distinct error) to avoid existence-leak side channels. See `pkg/storage/node_operations.go:GetNodeForTenant` for the canonical example.

### Partitioned shard maps + per-shard locks (A4 / A4-edges idiom)

`gs.nodeShards [256]map[uint64]*Node` and `gs.edgeShards [256]map[uint64]*Edge` are partitioned by `id & shardMask`. Helpers in `pkg/storage/storage_helpers.go`: `lookupNodeShard` / `storeNodeInShard` / `deleteNodeShardEntry` / `nodeCount` / `forEachNodeUnlocked` (and edge variants). Rules:

- **Readers** (`GetNode`, `GetEdge`): take `rlockShard(id)` only.
- **Writers** (`Create*`, `Update*`, `Delete*`, cascade helpers): take `gs.mu.Lock` for global indexes (`nodesByLabel`, `edgesByType`, `outgoingEdges`, `incomingEdges`, tenant indexes) PLUS `lockShard(id)` for the shardmap mutation.
- **Cross-shard ops** (DeleteNode cascading edges): collect IDs, sort by shard index, acquire in order. (Currently moot because `gs.mu.Lock` serializes writers, but the discipline matters if that ever changes.)

If you add a third partitioned structure, mirror this exactly. Don't re-invent.

### Atomic + lock-free counters

`closed atomic.Bool`, `nextNodeID` / `nextEdgeID` use `atomic.AddUint64`. Per-tenant counters use atomics with underflow protection (`atomicDecrementWithUnderflowProtection`).

### Snapshot format stability

Snapshot on-disk format is a flat `map[uint64]*Node` / `map[uint64]*Edge` even though in-memory storage is partitioned. `flattenNodesForSnapshot` / `rebucketSnapshotNodes` (and edge variants) handle the conversion. **Do not change the on-disk format without a snapshot version bump** — the snapshot file is customer-data-equivalent.

### `//nolint:` per-site convention

`//nolint:` directives MUST include a reason after the lint name. Plain `//nolint` is rejected by lint policy. See `docs/internals/design/AUDIT_code_quality_2026-05-06.md` and PR #63 for the rationale.

### `getEdge` / `getNode` benchmarks: prevent Clone elision

When writing benchmarks that mirror production paths (e.g., comparing a per-shard-RLock variant to a `gs.mu.RLock` Legacy baseline), the compiler will dead-code-eliminate `Clone()` if the result is unused. Use `var benchSink atomic.Pointer[T]` and `benchSink.Store(...)` to force the allocation. See `pkg/storage/bench_concurrent_read_test.go` for the pattern.

## Known infra patterns

- **CI Ubuntu jobs (both `test-verbose` AND `test-race`) consistently exit 143 with `runner has received a shutdown signal`.** The kill is **external SIGTERM to the runner agent** — NOT internal `go test -timeout`, NOT race-detector OOM (PR #159 capped `-p 2` with no observed change, ruling out OOM). Likely cause: account-level concurrent-job contention or runner-pool eviction; even **docs-only PRs** (e.g. #146) hit the same fast-fail, confirming the cause is infra not workload. Some runs die at 2:42, others at exactly 2821s (47:01) — a hard cap, not random preemption. macOS runs pass. Tolerated; escalation candidates are documented in `docs/NEXT_STEPS_2026-05-13.md` § Track H "Linux CI infra tax." PR descriptions can flag it as "known exit-143 infra pattern."
- **CI benchmark workflow consistently fails on the comment step.** Permission-scope issue, not a benchmark regression. Same toleration as exit-143.
- **`mergeStateStatus: UNSTABLE`** is the normal state for green PRs in this repo (because of the two known-infra failures above). Verify the failure set matches the expected pattern before merging; net-new failures need investigation.

## Known pitfalls

- **`git branch -d <squash-merged>` refuses** because the squashed commit on main is content-equivalent but not ancestor-equivalent to the branch tip. Use `-D` after verifying the PR is merged via `gh pr list --head <branch> --state merged`. Or just `--delete-branch` at merge time.
- **`gh pr merge` deletes the LOCAL branch when `--delete-branch` is passed** (in addition to the remote). A subsequent `git branch -D <name>` will error with "branch not found"; that's expected, not a problem.
- **`gh pr merge --delete-branch` on a stack-bottom PR auto-CLOSES dependent PRs (it does NOT retarget them).** When you delete the branch a dependent PR is based on, GitHub closes the dependent and refuses to reopen it (`gh pr reopen` fails with `GraphQL: Could not open the pull request`; `gh pr edit --base main` is rejected on closed PRs). When merging a stack: either retarget the dependent first (`gh pr edit <dep> --base main` *before* merging the parent — cleanest), or merge the parent without `--delete-branch` and clean the stale branch later. Recovery if it happens: rebase the orphan locally (`git rebase --onto origin/main <old-base-tip> <orphan-branch>`), force-push, open a fresh PR. Costs ~5 min + one extra PR number per orphan.
- **The cluster code (`pkg/cluster/`, ~2,800 LOC) is real but its production wiring is unverified.** The planning doc says "single-node assumption baked in" — the honest interpretation is "no sharded write path." Don't claim the cluster is unwired without checking; don't claim it's production-ready without checking either.
- **`pkg/compliance` exists with GDPR/SOC2 controls.** F3 ("Compliance API not started") is about the HTTP-API surface, not the framework itself. Scope F3 narrowly.

## When in doubt, ask the user

This codebase has had multiple audits and the planning doc sometimes misframes things (the cluster + compliance examples above). When the planning doc and the code disagree, **trust the code, then surface the discrepancy to the user** — don't silently work around the planning doc.

## Tooling notes

- Serena MCP is configured (`.serena/`) for symbol-level navigation — useful for locating cross-package symbols faster than ad-hoc grep.
- `golangci-lint` is configured with `max-same-issues: 3` (per `.golangci.yml`); cleanup PRs that touch many files often need 1-2 follow-up runs because new findings surface as originals clear. Plan the lint sweep accordingly.
- The repo's bench harness is reusable across data types — see `bench_concurrent_read_test.go` (nodes) and `bench_concurrent_edge_read_test.go` (edges) for the template if you partition a third data type.

## Preparing a new session (handoff convention)

When a session is about to end with substantive multi-PR work (≥3 merged PRs, or any work that left non-obvious state), write a session handoff at `docs/internals/design/SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md` before stopping. The next session opens by reading the most recent one.

**Skill**: `session-handoff` (`.claude/skills/session-handoff/SKILL.md`) automates this — it gathers state, fills the 7-section template, and opens a PR. Use it instead of writing the doc by hand. The convention below is what the skill follows; if you're inspecting why the skill produces what it does, this is the spec.

**Filename format**: `SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md` — UTC date + 4-digit 24-hour time (no separator), explicit `Z` suffix for UTC. Example: `SESSION_HANDOFF_2026-05-10-0208Z.md`. The time component is required because multiple handoffs can land in the same calendar day; UTC avoids timezone ambiguity. Pull the time from `git log -1 --format="%aI"` on the previous commit (or `date -u +"%Y-%m-%dT%H:%MZ"` if writing pre-commit).

**Distinct from `docs/HANDOFF_<DATE>_<TOPIC>.md`** — those are task-focused (one deliverable, full implementation brief, see e.g. `HANDOFF_2026-05-06_VECTOR_SEARCH_PROPERTY_FILTER.md`). `SESSION_HANDOFF_<YYYY-MM-DD>-<HHMM>Z.md` is session-state-focused (what shifted, what's queued, what to retire).

A session handoff should contain, in order:

1. **TL;DR** — one or two sentences on what changed at the project level.
2. **What's done** — this session's merged PRs, one line each. Include PR numbers.
3. **Current state** — `main` HEAD commit, open PRs (if any), open branches (if any), uncommitted changes (should be none — flag if not).
4. **What's next** — the ranked queue from the planning doc, with notes for any items the session moved (e.g., "A4-edges is now done, removed from the queue").
5. **Stale assumptions to retire** — anything in the user's auto-memory or in `NEXT_STEPS_<DATE>.md` that this session's work invalidated. Be specific: name the file, the line range, the corrected claim. The next session should be able to update the planning doc / refresh memory from this list alone.
6. **Open questions for the user** — decisions that came up but weren't resolved. The next session opens by either resolving these or by acknowledging them and proceeding.
7. **How to use this handoff** — a one-line pointer (e.g., "Read this first, then `NEXT_STEPS_<DATE>.md`, then act").

**Don't bundle a handoff into a task PR.** The handoff is its own commit/PR (single-file diff, fast review, doesn't churn alongside code). If the session also produced a planning-doc update, those can land in the same PR.

**Don't rely on the auto-memory system to substitute** — memory is per-user, the handoff is per-repo. A different agent (or a different model) reading this repo cold should be able to pick up where the previous session left off using only the handoff + the planning doc.

## Project-level skills available

Single-agent / session-lifecycle skills live in `.claude/skills/<name>/SKILL.md` in this repo:

| Skill | When | Output |
|---|---|---|
| `session-handoff` | At session-end with substantive multi-PR work, or "prepare a handoff." | `docs/internals/design/SESSION_HANDOFF_<...>.md` + PR. Stops before merge. |
| `planning-doc-update` | After a tracked task closes, or "mark X done in the planning doc." | Targeted edits to `docs/NEXT_STEPS_<DATE>.md` + PR. Single-file diff. |
| `ci-status-triage` | Before any merge (this repo's normal state is `UNSTABLE`-but-mergeable; manual classification required). | Categorised failure list + merge/hold/investigate recommendation. Doesn't modify anything. |
| `branch-cleanup` | After multi-PR work, or "clean up stale branches." | Local `git branch -D` of confirmed-merged branches. Asks user before bulk delete. |
| `integration-checkpoint` | Long-running branch (>4h) before merging, after high-leverage main changes, when the user says "sync against main." | Clean rebase + re-run tests + advisor confirmation that original framing still holds. |

**Parallel-agent coordination tooling lives in a sibling repo: [`dd0wney/graphdb-coord`](https://github.com/dd0wney/graphdb-coord)**. Extracted on 2026-05-10. Includes `work-claim`, `worktree-spawn`, `merge-coordinator`, `coord-next`, `coord-subtask`, `coord-clusters` skills + `cmd/coord-mcp` MCP server + `scripts/coord-*.sh` operational tooling. The atomic uniqueness primitive that makes the claim semantics correct (`pkg/storage.CreateNodeWithUniquePropertyForTenant` + `:Claim`/`for_task` resolver special-case) stayed here in graphdb because it's a useful generic primitive — graphdb-coord is the consumer.

To use the parallel-agent skills, clone graphdb-coord alongside this repo and follow its README.

## Parallel-agent coordination workflow (cross-repo)

When ≥2 Claude Code agents are or might be active on this repo simultaneously, the discipline lives in graphdb-coord; the *primitives* it relies on live here:

- **B-lite atomic uniqueness** (graphdb's `pkg/storage.CreateNodeWithUniquePropertyForTenant`) is what makes concurrent claims for the same task race-free.
- **The `:Claim`/`for_task` resolver special-case** (graphdb's `pkg/graphql/mutations_resolvers.go`) is currently the one place graphdb still hardcodes coord-domain knowledge. There's a TODO at that site to replace it with a generic uniqueness-rules registry — at that point graphdb-coord configures the rule and graphdb has zero coord-specific knowledge.

The user's global `~/.claude/CLAUDE.md` parallel-agent rules ("Never modify shared interfaces without explicit coordination," "Own your directory," "If you need an interface change, stop and propose it — don't implement," "Run full tests before marking any task complete," "Commit frequently with small atomic changes") are the discipline; graphdb-coord's skills + MCP tools are the mechanism.

## What this file is NOT for

- Today's TODO list. Use the planning doc.
- General Go advice. Use the user's global `CLAUDE.md`.
- Long-form architecture narrative. Use `docs/ARCHITECTURE*` (if it exists) or the audit docs.
- Cross-conversation memory. The agent's memory system handles that.
