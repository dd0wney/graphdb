---
name: coord-lesson
description: Record, list, show, or inform a `:Lesson` in coord — the curated learning layer (non-obvious findings tied to a Task). Covers the explicit lifecycle outside the auto-surfacing that happens on `coord claim`.
when_to_use: An agent finishes a task with a real lesson to record. The user says "save this as a lesson," "record a lesson," "what lessons apply to X," "surface lessons for task Y." Reviewing what prior work has taught us on a topic. Wiring an existing lesson to a new task via `inform`.
allowed-tools: Bash(../graphdb-coord/coord *) Bash(jq *)
---

# Coord lesson

Curated learnings tied to graphdb tasks via `LEARNED_FROM`/`INFORMED_BY` edges. Distinct from raw `:Insight` observations (use `coord-insight` for those — promotion to a lesson is a deliberate step).

## Current state

Live snapshot of the lesson corpus at skill-load time (zero-cost if daemon is up; fallback string if not — see Pre-flight to recover):

- Corpus: !`../graphdb-coord/coord lesson list --limit 5 --json 2>/dev/null | jq -ce '{count, recent_ids: [.lessons[0:3][].id]}' 2>/dev/null || echo '(coord daemon unreachable — see Pre-flight)'`

## When to invoke

- After substantive work on a task, the agent identifies a finding that:
  - Was NOT obvious from the code at the start of the task.
  - Will likely save a future agent (or the user) a wrong-turn iteration.
  - Has a concrete trigger ("when you do X, watch out for Y").
- The user says "save this as a lesson," "record a lesson," "what lessons apply to X."
- A new task is being claimed and the agent wants to recall lessons beyond the auto-surfaced set (broader scope, or `INFORMED_BY` vs `LEARNED_FROM`).

## What this skill does NOT do

- **Doesn't auto-record on every PR merge.** Lessons need a real finding. Recording fluff degrades scoring (recency × linked_task_count) and crowds out signal.
- **Doesn't replace `coord-insight`'s observation flow.** If you're not sure the finding generalizes, record an insight first; promote later when evidence accumulates.
- **Doesn't release the claim.** `coord release --lesson "..."` does both. Use that on PR merge; use `coord-lesson` for ad-hoc recording outside the release flow.

## Pre-flight

```bash
COORD_BIN="${COORD_BIN:-../graphdb-coord/coord}"
if [[ ! -x "$COORD_BIN" ]]; then
  echo "coord binary not found at $COORD_BIN — build it: (cd ../graphdb-coord && go build -o coord ./cmd/coord)" >&2
  exit 1
fi
"$COORD_BIN" status >/dev/null || { echo "coord daemon not reachable" >&2; exit 1; }
```

Env vars (auto-discovered if defaults work):

- `GRAPHDB_COORD_URL` — default `http://localhost:8090`.
- `GRAPHDB_COORD_TOKEN` — falls back to `~/.graphdb-coord-key`.
- `COORD_PROJECT` — default auto-detected; in graphdb's working tree this should resolve to `graphdb`. Set explicitly if running from a worktree where auto-detect drifts.

## Process

### Record a lesson tied to a task

Lessons MUST cite a task — that's what makes them recallable on future claims.

```bash
TASK_ID="graphdb:H4.3-followup"           # the task the lesson came from
"$COORD_BIN" release --no-task-done \
  --lesson "Tenant-index rebuild only triggers on label match, so persistence_replay_test must seed nodes with the exact label, not a substring." \
  --evidence-ref "https://github.com/dd0wney/graphdb/pull/109" \
  "$TASK_ID"
```

`--no-task-done` keeps the task in progress while still appending the lesson + closing the active claim if there is one. Drop it if the lesson coincides with task completion.

To record a lesson WITHOUT touching any claim (e.g. retroactive):

```bash
# Stand-alone via MCP — no CLI equivalent today. Use coord_record_lesson if
# you have an MCP session, otherwise the release path above is the supported
# CLI surface.
```

### List lessons

```bash
# All lessons, newest first.
"$COORD_BIN" lesson list --limit 20

# Lessons linked to a specific task (default edge: INFORMED_BY).
"$COORD_BIN" lesson list --task "graphdb:F3" --limit 10

# Just the ones the task LEARNED_FROM (recorded during work on it).
"$COORD_BIN" lesson list --task "graphdb:F3" --edge-type LEARNED_FROM

# Include soft-deleted (deprecated) entries — for audit.
"$COORD_BIN" lesson list --include-deprecated
```

### Show one lesson

```bash
"$COORD_BIN" lesson show "lesson-abc123"
```

### Wire a lesson to a new task (`inform`)

When starting work on task B and you know lesson L (recorded against task A) is directly relevant, attach it explicitly. This drives lesson scoring up (linked_task_count++).

```bash
"$COORD_BIN" lesson inform "lesson-abc123" "graphdb:F4"
```

## What a good lesson looks like

- **Concrete and conditional**: "When X, do Y." vs. "Be careful with X."
- **Evidence-linked**: `--evidence-ref` to a PR / commit / line. Future agents verify before applying.
- **Specific noun phrases**: `pkg/storage/persistence_replay.go` beats "the replay code."
- **Single-clause findings**: One lesson per finding. Multi-clause lessons fragment under content-hash dedup and resurface twice.

## What this skill does NOT do (continued)

- **Doesn't deduplicate manually.** Content-hash dedup is server-side (Phase 2 item 8). Re-recording an identical lesson is silently merged — but slight rewordings create separate lessons. Aim for consistent phrasing across sessions.
- **Doesn't validate evidence URLs.** They're stored as strings; a 404 lesson is still a lesson.

## Edge cases

- **Recording a lesson against a task that doesn't exist yet.** Either create the task first (manual `POST /nodes` against the daemon, or the `coord-subtask` skill in graphdb-coord), or pick the nearest enclosing parent task. Don't invent task IDs — they won't pass the foreign-key resolve.
- **Lesson contradicts a prior lesson.** Record the new one. The dreaming layer (`coord-dream`) can later cluster contradictions; soft-deprecation via lesson invalidation (Phase 2 item 7) is the curated fix.
- **Lesson author is "the user," not the agent.** Record it normally — `agent_source=cli` is recorded but the lesson content is what matters.

## Honest framing

Lessons earn their weight when:

- ≥2 agents work in the repo and surfacing prior findings prevents re-walking the same path.
- A task's claim takes >1 session, and the next session needs to recall what the prior session learned.
- The user revisits an area weeks later and needs to remember what the constraints are.

Lessons do NOT pull weight when:

- Solo, single-session work on isolated scope.
- The finding is already documented in CLAUDE.md or a planning doc.
- The finding is a one-off (won't recur).

When in doubt, prefer `coord-insight` (cheap, deferred curation) over `coord-lesson` (curated, surfaces on every related claim).
