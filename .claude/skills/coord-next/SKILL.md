---
name: coord-next
description: Recommend the next task to claim from the coord daemon. Returns the highest-priority pending Task with all DEPENDS_ON dependencies satisfied (i.e. all upstream Tasks are done). Borrowed from Taskmaster's `tm next` per docs/COMPARE_TASKMASTER_2026-05-10.md §7. Use when starting a session, when the user asks "what's next," "what should I work on," or after closing a task. Output is the bare task ID — pipe directly into `work-claim` to atomically take it. Read-only; doesn't mutate coord state.
---

# Coord next

What should you work on next? Asks the coord daemon, walks the dependency graph, returns the answer.

## When to invoke

- Session start. Before doing anything else, check what coord recommends.
- After closing a task. Releasing a Claim opens a slot — the next-task recommendation may have shifted.
- The user asks "what's next," "what should I work on," "any unblocked work."
- `worktree-spawn` is going to need a target Task ID and the user hasn't named one.

## What it returns

The highest-priority pending Task whose DEPENDS_ON dependencies are all satisfied. "Satisfied" means the dependency Task is `done` (or `cancelled`, per Taskmaster's convention — both close the dependency edge from the planner's perspective).

If multiple Tasks tie for "highest priority," the tie-break is:

1. Tasks blocking the most other Tasks first — unblock the queue.
2. Older Tasks first (lowest numeric Task node ID) — FIFO discipline.

If nothing is unblocked, the skill reports the candidates and what's blocking each. Don't return a Task that's already claimed (has an active `:Claim` with `for_task` matching).

## Pre-flight

```bash
export GRAPHDB_COORD_URL="${GRAPHDB_COORD_URL:-http://localhost:8090}"
export GRAPHDB_COORD_TOKEN="${GRAPHDB_COORD_TOKEN:-$(cat ~/.graphdb-coord-key)}"
curl -fsS "$GRAPHDB_COORD_URL/health" >/dev/null || {
  echo "coord daemon not reachable — re-run scripts/coord-bootstrap.sh" >&2
  exit 1
}
```

## Process

```bash
# 1. Pull all nodes and edges (two requests; pass to Python via env vars
#    so heredoc-as-script and stdin-as-data don't fight over fd 0).
NODES=$(curl -fsS -H "X-API-Key: $GRAPHDB_COORD_TOKEN" "$GRAPHDB_COORD_URL/nodes")
EDGES=$(curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/graphql" \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}')

# 2. Compute the recommendation. Python because the dependency-graph
#    walk is awkward in pure bash, and the H4.1 base64 workaround is
#    a one-liner here. Heredoc avoids the bash/python quoting tangle.
NODES="$NODES" EDGES="$EDGES" python3 - <<'PYEOF'
import json, os, sys, base64

def decode(v):
    try:
        return base64.b64decode(v).decode("utf-8")
    except Exception:
        return v

nodes = json.loads(os.environ["NODES"])
edges = json.loads(os.environ["EDGES"]).get("data", {}).get("edges", [])

# Build quick lookups.
tasks = {}                  # node_id -> {id, status, track}
claimed_for_tasks = set()   # for_task strings with an active :Claim
for n in nodes:
    nid = str(n["id"])
    if "Task" in n.get("labels", []):
        tasks[nid] = {
            "id": decode(n["properties"].get("id", "")),
            "status": decode(n["properties"].get("status", "")),
            "track": decode(n["properties"].get("track", "")),
        }
    elif "Claim" in n.get("labels", []):
        claimed_for_tasks.add(decode(n["properties"].get("for_task", "")))

# DEPENDS_ON edges: from_task → to_task (you depend on to_task closing).
deps_of = {}     # task_node_id -> [dependency_task_node_ids]
blocks = {}      # task_node_id -> count of tasks that depend on it
for e in edges:
    if e.get("type") != "DEPENDS_ON":
        continue
    f, t = str(e["fromNodeId"]), str(e["toNodeId"])
    deps_of.setdefault(f, []).append(t)
    blocks[t] = blocks.get(t, 0) + 1

def is_unblocked(node_id):
    for dep_id in deps_of.get(node_id, []):
        dep = tasks.get(dep_id)
        if not dep:
            continue   # missing dep node — treat as not blocking
        if dep["status"] not in ("done", "cancelled"):
            return False, dep
    return True, None

# Candidates: pending Tasks not already claimed.
candidates = []
blocked_candidates = []
for nid, t in tasks.items():
    if t["status"] != "pending":
        continue
    if t["id"] in claimed_for_tasks:
        continue
    ok, blocker = is_unblocked(nid)
    if ok:
        candidates.append((nid, t))
    else:
        blocked_candidates.append((nid, t, blocker))

if not candidates:
    print("no unblocked pending Tasks.")
    if blocked_candidates:
        print("blocked candidates:")
        for nid, t, blocker in blocked_candidates:
            b_id = blocker["id"] if blocker else "?"
            b_status = blocker["status"] if blocker else "?"
            tid = t["id"]; track = t["track"]
            print(f"  {tid} (track={track}) blocked by {b_id} (status={b_status})")
    sys.exit(2)

# Tie-break: most-blocking first, then lowest-id (FIFO).
candidates.sort(key=lambda c: (-blocks.get(c[0], 0), int(c[0])))

best_nid, best = candidates[0]
b_id = best["id"]; b_track = best["track"]
print(f"NEXT: {b_id}")
print(f"  track:    {b_track}")
print(f"  node id:  {best_nid}")
print(f"  blocks:   {blocks.get(best_nid, 0)} downstream task(s)")
print(f"  alternatives ({len(candidates) - 1}):")
for nid, t in candidates[1:6]:   # show up to 5 alternatives
    tid = t["id"]; track = t["track"]; bcount = blocks.get(nid, 0)
    print(f"    {tid:50s} (track={track}, blocks={bcount})")
PYEOF
```

## Returning the answer to a script (vs. a human)

The skill prints a human-readable block by default. To extract just the bare task ID for piping into `work-claim`:

```bash
TASK_ID=$(curl ... | python3 -c '... print(f"{best[\"id\"]}")')   # only the NEXT line
```

A future iteration could split into two output modes (`--bare` vs. default), but for now the human form is the primary use and an `awk '/^NEXT:/ { print $2 }'` extracts the ID for scripting.

## Examples

**Healthy state, multiple unblocked candidates:**

```
NEXT: graphdb:F1.1-spike
  track:    F
  node id:  2
  blocks:   1 downstream task(s)
  alternatives (4):
    graphdb:H2                                          (track=H, blocks=0)
    graphdb:F3                                          (track=F, blocks=0)
    graphdb:A8.1                                        (track=A, blocks=0)
    graphdb:S1                                          (track=S, blocks=0)
```

**Everything blocked or claimed:**

```
no unblocked pending Tasks.
blocked candidates:
  graphdb:F1.1-impl (track=F) blocked by graphdb:F1.1-spike (status=pending)
```

In the blocked case, the skill exits with status 2 — the caller can detect that and surface "the user has nothing actionable to start; finish in-progress work or unblock the queue."

## What this skill does NOT do

- **Doesn't claim anything.** Read-only; the recommendation is advisory. Pipe the result into `work-claim` to actually take the task.
- **Doesn't filter by track.** If you want only Track-F tasks, post-filter in your shell (`coord-next | grep 'track=F'`). Adding a track filter to the skill itself is a future enhancement; current scope is "give me the single best next task."
- **Doesn't handle cycles in DEPENDS_ON.** If A depends on B and B depends on A, both are "blocked" forever. Surface as "blocked," not as a cycle. The `merge-coordinator` skill is where cycle detection lives; cross-reference it if needed.
- **Doesn't know about `:SUBTASK_OF`.** A Task with subtasks is treated as a single unit. Future enhancement: recurse into subtasks when the parent is "in-progress."

## Coordination with other skills

- **`work-claim`**: typical pipeline is `coord-next` → user inspects → `work-claim <task-id>`.
- **`worktree-spawn`**: in fully-automated parallel-agent setups, each spawn calls `coord-next` to pick its task, then `work-claim`.
- **`coord-clusters`**: `coord-next` returns one Task; `coord-clusters` returns a parallel-execution group. Use `coord-clusters` if you have multiple agents available; `coord-next` for single-agent.

## Pre-flight checks

- [ ] Coord daemon reachable.
- [ ] At least one Task with `status=pending` in coord (else there's nothing to recommend).
- [ ] Status enum migration has run (`scripts/coord-migrate-status-enum.sh`) — pre-migration `open` Tasks are invisible to this skill's `pending` filter.

## Limitations

- The query reads the entire `/nodes` and `edges` surface every invocation. Fine at coord scale (≤100 Tasks); a dedicated GraphQL "ready tasks" resolver would be faster. Don't optimize prematurely — surface the latency only if it actually becomes a problem.
- Tasks with `status=blocked` (the explicit human-resolvable blocker) are excluded from candidates. If you want to surface them too, run `coord-clusters` which shows the full blocked-list. This skill returns one actionable answer.
