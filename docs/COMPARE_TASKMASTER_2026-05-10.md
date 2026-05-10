# Comparison: Taskmaster (Hamster) vs. graphdb-as-coord

**Date**: 2026-05-10
**Source**: `https://tryhamster.com/docs/taskmaster` and 27 doc sub-pages, fetched 2026-05-10.
**Scope**: After landing the H4 coord-deploy track (PRs #85–#94), the user surfaced Taskmaster as a comparable product. This doc is the side-by-side analysis: surface, atomicity, dependency model, AI integration, lock-in, fit. Conclusion in §6.

## 1. Executive summary

| | Taskmaster | graphdb-as-coord |
|---|---|---|
| **Surface** | CLI + MCP (36 tools) + 13+ editor integrations | REST + GraphQL + 3 bash skills |
| **Storage** | `tasks.json` local OR hosted (Hamster cloud) | graphdb instance (self-hosted) |
| **Atomic claim** | ❌ not addressed in docs | ✅ B-lite resolver (PR #91, verified 10-way concurrent) |
| **Dependency graph** | ✅ traversal, validate, fix, "clusters" parallel grouping | ⚠️ edges supported; traversal lives in `merge-coordinator` skill bash |
| **Parallel agents** | ✅ "clusters start" + tmux + Claude sub-agent teams | ⚠️ worktree-per-task; no orchestration layer |
| **AI providers** | 15+ (Anthropic, OpenAI, Google, Perplexity, local) | Claude Code only (skills are Claude-specific) |
| **Subtasks / hierarchy** | ✅ first-class | ❌ planning-doc only (flat) |
| **License** | MIT + Commons Clause (commercial use restricted) | MIT (your repo) |
| **Vendor lock-in** | ⚠️ "Together mode" pulls data to Hamster servers | ✅ self-hosted, your code |
| **Dogfood positioning** | n/a — they sell to others | ✅ "graphdb coordinates its own development" |

**Core finding**: Taskmaster is *productized* where we are *primitive*. They've solved the surface-area / IDE-integration / AI-provider problem and not solved the atomicity problem. We've solved atomicity and not packaged the surface. The two are strong in opposite places.

## 2. Atomicity — the load-bearing difference

This is the part where the products genuinely differ in capability, not just packaging.

### Taskmaster

The closest mention is the `start` command, which "launches your AI coding agent (Claude Code) with the full context of a specific task" and "the task status is automatically set to `in-progress`." From the docs:

> The documentation does not address atomic claiming or concurrent execution scenarios. There is no information about: whether the task is claimed exclusively when `start` runs; what happens if two agents invoke `start` simultaneously on the same task; lock mechanisms or race condition handling; claim semantics or conflict resolution.

The parallel-execution story is `clusters start` + tmux + Claude's experimental `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` flag. Coordination happens at the **process orchestration** layer (tmux panes managed by a parent agent), not the **storage** layer. Two independent Taskmaster CLIs on the same `tasks.json` would race on the file write; two agents using "Together mode" (cloud) presumably race on the cloud DB write — neither path mentions a CAS / unique-constraint primitive.

Of the 36 MCP tools listed in `capabilities/mcp`, **none** are claim/lock primitives. There's `set_task_status` (mutates) and `next_task` (read), but no `claim`, `acquire`, or "fail-if-already-running" primitive.

### graphdb-as-coord

PR #91's B-lite resolver enforces uniqueness on `:Claim.for_task` at the storage layer under `gs.mu.Lock` for the entire check + create. Two concurrent `createNode(labels: ["Claim"], properties: {for_task: X})` calls collapse into a single critical section: exactly one wins, the other gets a structured `*UniqueConstraintError` carrying the conflicting node ID.

Verified empirically (PR #91 description):

> 10-way concurrent: 1 success (id=26), 9 conflicts, all referencing the same conflicting node.

That's a property Taskmaster's docs don't claim and Taskmaster's surface doesn't expose.

**Why this matters strategically**: the dogfood story we've been building requires *real* atomic claim semantics ("graphdb coordinates its own development"). Advisory honor-system check-then-create wouldn't sell. Taskmaster's positioning ("permanent context, zero drift") doesn't depend on atomicity — they've correctly avoided promising what they don't have.

## 3. Dependency-graph surface — where Taskmaster has the lead

Taskmaster is meaningfully ahead on graph operations *as user-facing commands*.

| Operation | Taskmaster | graphdb-as-coord |
|---|---|---|
| Add dependency | `tm add-dependency --id=5 --depends-on=3` | REST `POST /edges` with type=DEPENDS_ON |
| List ready tasks | `tm list --ready` | manual GraphQL query |
| List blocking tasks | `tm list --blocking` | not built |
| Detect cycles | `tm validate-dependencies` | not built |
| Auto-fix invalid edges | `tm fix-dependencies` | not built |
| DAG-grouped parallel plan | `tm clusters` | not built |
| Recommend next task | `tm next` (walks full DAG) | not built |
| Cross-tag dependencies | ✅ ("task 5 in feature-auth can depend on task 3 in core") | ⚠️ via `IN_PROJECT` edges across `:Project` nodes; works but no helper |

The irony: **graphdb is a graph database**. The *primitives* for everything in the right column are present (we ship traversal, edge filtering, label scans, REST + GraphQL). What's missing is the *user-facing* "what should I work on next?" / "what's blocking?" layer. That's a small skill on top of existing primitives — closer to a weekend than a quarter.

Taskmaster builds this on a `tasks.json` file with hand-rolled DAG traversal in their CLI. We have the proper substrate; we haven't surfaced the queries.

## 4. MCP surface — Taskmaster's biggest unlock we don't have

Taskmaster exposes 36 MCP tools across six categories (task mgmt, info, analysis, dependency, project config, tags). Any MCP-aware editor (Cursor, VS Code with the right extension, Cline, the broader MCP ecosystem) can drive Taskmaster directly.

graphdb-as-coord exposes:

- 3 bash skills (Claude-Code-specific)
- Raw REST + GraphQL (any HTTP client)
- No MCP tools

The MCP wrapper would be roughly:

- `coord_claim_task(task_id)` → wraps work-claim's GraphQL mutation
- `coord_release_claim(claim_id, pr_number)` → wraps the release flow
- `coord_next_task()` → new query, but built on existing graph primitives
- `coord_list_blocking()` / `coord_list_ready()` → same
- `coord_add_dependency(from, to)` → REST POST /edges
- `coord_status(task_id)` → REST + decode

~6-10 tools, ~200-400 LOC of Go (graphdb already has the underlying primitives), and we're MCP-reachable from every IDE Taskmaster integrates with — *with atomic claim semantics they don't have*.

This is the most actionable "what's worth borrowing" item in the comparison.

## 5. Storage / hosting model

| | Taskmaster | graphdb-as-coord |
|---|---|---|
| Default | local `tasks.json` | local graphdb daemon |
| Cross-machine | "Together mode" → Hamster cloud | run daemon on a shared host (Cloudflare Tunnel, VPS) |
| Multi-project | Tags within one tasks.json | `:Project` nodes (PR #89) — one daemon, many repos |
| Backup story | unclear — presumably their cloud has it | local snapshot; user-controlled |
| Data residency | "Tasks live on Hamster's servers" (cloud mode) | wherever you run the daemon |

For the kind of users who self-host their AI coding stack (the user's own profile here, per memory), Hamster cloud is a non-starter and the `tasks.json` local mode lacks the multi-machine story. graphdb-as-coord's self-hosted-but-shareable shape splits the difference correctly.

For the kind of users who'd happily put their task list in a SaaS, Taskmaster wins on convenience. Different segments.

## 6. Conclusion + recommendation

**They are different layers, not direct competitors.**

- **Taskmaster** is a productized AI-task-coordinator: nice CLI, broad MCP surface, multiple editors, freemium hosted backend. Strong on UX, weak on atomicity.
- **graphdb-as-coord** is a primitive: real atomic claim semantics, self-hosted, your-product-eating-its-own-cooking. Strong on correctness and dogfood positioning, weak on UX and IDE breadth.

A company in our position has three rational moves:

### Option 1 — Stay primitive, build the MCP wrapper

~200-400 LOC of Go to expose 6-10 MCP tools. Closes the IDE-integration gap. Keeps the dogfood story. Doesn't try to compete on AI-provider breadth or polished CLI ergonomics — those are Taskmaster's moat and not in our positioning.

**Trigger to do this**: another agent (or a user) wants to drive coord from outside Claude Code. Or a customer demo where the IDE-side story matters. Until then, the bash skills are sufficient for our own use.

**Cost**: ~1 week of work. Zero risk to the existing surface.

### Option 2 — Stop here; let Taskmaster be Taskmaster

graphdb's positioning isn't "task coordinator" — it's "graph database." Coord is dogfood, not a product line. Adding MCP tooling expands scope into Taskmaster's territory, which we don't need to win.

**When to choose this**: if F1.1 (per-tenant LSA) and F3 (compliance API) — graphdb's actual feature work — is more valuable to ship than additional coord polish. Per the planning doc, F1.1-spike is now top of the queue and F3 follows. Coord is a means; F1.1 / F3 are the end.

### Option 3 — Pitch Taskmaster on graphdb as their backend

Taskmaster's storage choice (`tasks.json` or proprietary cloud) is a place they have real exposure: tasks.json doesn't scale to teams, and "trust our cloud" is a hard sell to anyone with security review. graphdb-as-the-coord-backend would give them atomicity (closing their docs gap) and self-hosted option (closing their data-residency gap).

**When to choose this**: if there's commercial appetite for graphdb-as-OEM-backend. Long sales cycle, uncertain payoff. Probably premature.

### Recommendation

**Option 1 (MCP wrapper) is the highest-leverage move IF coord-side polish becomes important.**
**Option 2 (move on to F1.1) is correct otherwise** — and it's the planning doc's current top of queue.

Don't do Option 3 unless a Taskmaster maintainer reaches out, in which case it's a conversation, not a roadmap.

The dogfood claim ("graphdb coordinates its own development with real atomic semantics") **already holds** as of PR #91 + #93. We don't need to do anything more to maintain it.

## 7. What I'd borrow if scope opens up

In rough priority order:

1. **`tm next` equivalent** — query the coord for "highest-priority unclaimed Task with all dependencies satisfied." ~30 LOC of GraphQL + a skill wrapper. Tiny win, big agent-experience improvement.
2. **MCP wrapper** (Option 1 above) — IDE integration. Bigger scope, only worth it for non-Claude-Code workflows.
3. **Subtask convention** — currently the planning doc is the only place sub-scope decomposition lives. Adding a `:SUBTASK_OF` edge convention + skill helper would let coord track sub-scopes natively. ~15 LOC.
4. **Status enum richer than open/done** — adopt Taskmaster's `pending`/`in-progress`/`blocked`/`done`/`deferred`/`cancelled`. Schema-only change to the seed script + worktree-spawn. Cosmetic but useful.
5. **`tm clusters` equivalent** — DAG-grouped parallel-execution plan. Closes a gap we don't currently feel because we're a single-agent shop most days.

Each of these is independent and small. Pick zero, pick all five — the planning doc has F1.1 / F3 ahead either way.

## 8. What I would NOT borrow

- **Hosted cloud sync** — wrong for our positioning. Self-hosted is the moat.
- **AI-provider abstraction (15+ models)** — our skills are deliberately Claude Code-shaped. Generalizing dilutes the dogfood story.
- **`parse_prd` / `expand_task`** — depends on hosted AI inference. Conceptually possible, off-mission.
- **Tags as a parallel organizational axis to dependencies** — our `:Project` model already covers cross-cutting workspaces.

These would expand surface area in directions that don't compound with graphdb's value.

## 9. Caveats

- This analysis is built from public docs. Taskmaster's source is MIT (with Commons Clause), so a full code-side audit is possible if we ever needed certainty on a specific behavior.
- Taskmaster is actively developed; specific docs pages may shift. Refetch if the gap analysis is more than ~30 days old.
- The atomicity finding is the most consequential claim; it's based on the absence of any claim/lock primitive in the published MCP tool list and the absence of concurrency discussion in the `start` / `loop` / `clusters start` docs. Confirmed but not exhaustively — if a maintainer later points to a primitive we missed, revisit.

## Appendix A — pages fetched

- `/docs/taskmaster` (overview)
- `/docs/taskmaster/capabilities/task-structure`
- `/docs/taskmaster/capabilities/mcp`
- `/docs/taskmaster/capabilities/clusters`
- `/docs/taskmaster/task-workflow/dependencies`
- `/docs/taskmaster/automation/start`
- `/docs/taskmaster/automation/loop`
- `/docs/taskmaster/team/overview`

Cached responses in this analysis; no source code inspected.
