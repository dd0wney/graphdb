---
name: coord-dream
description: Run the coord dreaming layer — out-of-band synthesis that proposes graph mutations (`:Pattern` nodes + GENERALIZES_TO edges) by clustering related `:Lesson` content. Lifecycle is propose → review (via `list`/`show`) → decide (apply or reject).
when_to_use: The user says "run a dreaming pass," "dream over the lessons," "what patterns have we accumulated," "list proposed dreams," "apply dream X," "reject dream Y." The lesson corpus has grown ≥5–10 lessons since the last pass and cross-lesson clustering might find something. Reviewing what abstractions have been auto-suggested.
allowed-tools: Bash(../graphdb-coord/coord *) Bash(jq *) Bash(wc *)
---

# Coord dream

The dreaming layer is out-of-band synthesis over the accumulated `:Lesson` corpus. Inspired by sleep-consolidation: agents do hot-path work all day, then the dreaming pass clusters and generalises offline. It never runs in any agent's hot path; you invoke it deliberately.

## How the loop works

```
1. propose    →  runs analyzers (currently: cross_lesson_similarity)
                 over the live lessons. If clusters meet the threshold,
                 persists a :DreamDiff (status="proposed") containing
                 proposed mutations as items_json.
                 Empty=true means "ran cleanly, found nothing" — no
                 diff is persisted in that case.

2. list       →  enumerate :DreamDiff nodes. Default policy is
                 proposed-only (the actionable set). Pass
                 --include-applied / --include-rejected for history.

3. show       →  render one :DreamDiff with its parsed items.

4. apply      →  materialise items into the graph. For create_pattern
                 items, creates a :Pattern + GENERALIZES_TO edges to
                 each contributing :Lesson. Stops at the first failing
                 item; partial state is preserved for inspection.

5. reject     →  close a proposed diff with a required --reason.
                 The reason becomes audit-trail material for future
                 dreaming passes ("we tried this clustering, operator
                 said no, because...").
```

## When to invoke

- The user says: "run a dreaming pass," "dream over the lessons," "what's been proposed?", "apply dream X," "reject dream Y."
- Periodic synthesis after substantive lesson accumulation (rough heuristic: ≥5 new lessons since the last dreaming pass).
- A user is reviewing what abstractions have been auto-suggested: "what patterns has the system noticed?"
- After a lesson invalidation cascade or a content-hash dedup pass — the corpus shape may have shifted enough to surface new clusters.

## What this skill does NOT do

- **Doesn't auto-apply.** Every proposed diff needs an explicit `apply` or `reject` decision. The dreaming layer is a recommendation surface, not an autonomous mutation.
- **Doesn't re-run on a cron.** Invocation is deliberate. Coord's daemon runs no scheduled dreaming; the propose step is what initiates a pass.
- **Doesn't dedupe across passes.** Two passes with identical inputs and parameters will produce two `:DreamDiff` nodes. The cost is small (one row per pass) and the audit trail is useful, but be aware before chaining `propose` calls.
- **Doesn't operate on `:Insight` nodes.** The v1 analyzer (`cross_lesson_similarity`) clusters lessons only. Insights become dream-fuel only after promotion to lessons.

## Pre-flight

```bash
COORD_BIN="${COORD_BIN:-../graphdb-coord/coord}"
if [[ ! -x "$COORD_BIN" ]]; then
  echo "coord binary not found at $COORD_BIN — build it: (cd ../graphdb-coord && go build -o coord ./cmd/coord)" >&2
  exit 1
fi
"$COORD_BIN" status >/dev/null || { echo "coord daemon not reachable" >&2; exit 1; }

# Worth knowing the lesson count before dreaming: thin corpus means
# nothing to cluster. <5 lessons → analyzer almost always returns empty.
"$COORD_BIN" lesson list --limit 200 --json | jq '.count' 2>/dev/null \
  || "$COORD_BIN" lesson list --limit 200 | wc -l
```

## Process

### Propose

```bash
# Defaults: threshold ~0.30, shingle-size 3, min-cluster 2, analyzer=cross_lesson_similarity.
"$COORD_BIN" dream propose

# Tune the clustering knobs:
"$COORD_BIN" dream propose --threshold 0.25 --min-cluster 3
```

Output identifies the new `:DreamDiff` id (or `empty=true` if nothing was proposed). Save the id; you'll need it for show/apply/reject.

### List

```bash
# Default: proposed-only (the actionable set).
"$COORD_BIN" dream list

# Include the audit trail:
"$COORD_BIN" dream list --include-applied --include-rejected --limit 50

# JSON for scripting:
"$COORD_BIN" dream list --json
```

The default policy (proposed-only) is encoded in `defaultDreamDiffListFilter` in `internal/coord/dream_operations.go` (graphdb-coord). Rationale: `dream` lifecycle is action-oriented; `list` without args means "what's pending review?" The audit path is opt-in.

### Show

```bash
"$COORD_BIN" dream show "dream-abc123"
```

Renders the diff metadata + parsed items. Always show before apply/reject — the agent should understand what's being proposed before deciding.

### Apply

```bash
"$COORD_BIN" dream apply "dream-abc123"
```

Exits 0 on full success (`:DreamDiff.status` → `applied`). Exits 3 on partial apply (the diff stays `proposed`; per-item results indicate what failed). Recovery from partial is operator-mediated: inspect, clean up, retry or reject.

### Reject

```bash
"$COORD_BIN" dream reject "dream-abc123" --reason "Cluster groups lessons that are surface-similar but operationally unrelated — applying would create a noisy :Pattern."
```

Reason is required (audit-trail discipline — future dreaming passes inform from past rejections, but only if the reason is recoverable from the graph).

## Choosing apply vs. reject

A proposed diff is worth applying when:

- The proposed `:Pattern.description` accurately generalises across the contributing lessons.
- The contributing lessons share *operational* similarity (when-X-do-Y triggers align), not just lexical similarity.
- The resulting pattern would be useful as a surfacing-target — e.g. a future `:Insight` could be wired to a pattern, or a `coord claim` could optionally surface patterns matching the claimed task's scope (future work).

Reject when:

- The cluster is a textual coincidence — lessons share words but address different problems.
- The proposed description glosses over the meaningful differences between contributing lessons.
- The cluster size is too small to be load-bearing (e.g. a 2-lesson cluster on a coincidence).

When in doubt, reject with a clear reason. Applied `:Pattern` nodes are harder to retract than rejected diffs are to re-propose with tighter parameters.

## Edge cases

- **Partial apply on a multi-item diff.** Status stays `proposed`. Some items wrote to the graph; others didn't. Use `show` to see per-item results, then either:
  - Manually clean up the partial state (`DELETE /nodes/<pattern-id>` against the daemon) and retry `apply`.
  - Reject the remaining items with a reason naming the partial state.
  - There is no "apply just the remaining items" verb in v1.
- **Re-proposing identical clusters.** Allowed; creates a second `:DreamDiff`. Either reject the older duplicate or both, with reason referencing the dup.
- **`mark_lesson_stale` / `mark_pattern_superseded` items.** Recognised but not implemented in v1 (no analyzer emits them yet). `apply` will report "not yet implemented" for these items and treat them as a failure. Reject diffs containing them until an analyzer that emits them ships.
- **Dreaming against a thin corpus (<5 lessons).** Almost always returns empty. Not a failure — just no signal yet.

## Honest framing

The dreaming layer earns its weight when:

- The lesson corpus has grown beyond what one agent (or human) can mentally cluster.
- The user wants to surface the implicit themes in recent work without having to re-read every lesson.
- A `:Pattern` would have downstream value (auto-surfacing on claims, dashboards, retrospective material).

The dreaming layer does NOT pull weight when:

- Thin corpus. `propose` returns empty cleanly; cost is bounded but the value is zero.
- Pre-extraction: if you're still trying to figure out what the lessons should even look like, dreaming over noise produces noise patterns. Curate lessons first; dream second.
- One-off taxonomy work. If you want a one-time grouping of lessons, just read them; the `:DreamDiff` audit trail is overhead unless you expect repeat passes.
