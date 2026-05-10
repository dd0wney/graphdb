---
name: coord-clusters
description: Compute DAG-grouped parallel-execution clusters from coord's :Task graph. Returns layered groups of pending Tasks where every Task in a layer can be claimed in parallel (no DEPENDS_ON between siblings within a layer; all dependencies are in earlier layers). Borrowed from Taskmaster's `tm clusters` per docs/COMPARE_TASKMASTER_2026-05-10.md §7. Use when planning multi-agent / multi-worktree work, when the user asks "what can run in parallel," "show me the dependency layers," or before spawning N parallel worktrees. Read-only; does NOT claim tasks. Output is a topologically-sorted execution plan; pipe layer N's IDs into N parallel `work-claim` invocations.
---

# Coord clusters

What can run in parallel right now? Walk the DAG, return a layered plan.

## When to invoke

- Multi-agent / multi-worktree session planning. Want to spawn 3 worktrees? `coord-clusters` tells you which 3 Tasks have no dependencies between them.
- The user asks "what can run in parallel," "show me the dependency layers," "is anything blocked but ready-to-go after X closes."
- After a big task closes — re-run to see whether new tasks became unblocked together.

If you only need ONE recommendation (single-agent flow), use `coord-next` — it's cheaper and returns a definite answer.

## What "cluster" means here

A *layer* (cluster) is a set of pending Tasks where:

1. None of them depend on each other.
2. All of their DEPENDS_ON edges target Tasks that are `done` or `cancelled`, OR target Tasks in *strictly earlier* layers.

Layer 0 is "fully unblocked right now." Layer 1 is "unblocked once layer 0 closes." And so on. The layered structure mirrors the standard topological-sort-with-levels algorithm.

A Task with circular dependencies never appears in any layer; the skill surfaces these separately as "cycle members" — a coordination problem that the user resolves.

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
NODES=$(curl -fsS -H "X-API-Key: $GRAPHDB_COORD_TOKEN" "$GRAPHDB_COORD_URL/nodes")
EDGES=$(curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/graphql" \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}')

NODES="$NODES" EDGES="$EDGES" python3 - <<'PYEOF'
import json, os, sys, base64

def decode(v):
    try: return base64.b64decode(v).decode("utf-8")
    except Exception: return v

nodes = json.loads(os.environ["NODES"])
edges = json.loads(os.environ["EDGES"]).get("data", {}).get("edges", [])

# Build Task lookups.
tasks = {}                    # node_id -> {id, status, track}
claimed_for_tasks = set()
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

# DEPENDS_ON edges. We work in node-id space.
deps_of = {}     # node_id -> [node_ids it depends on]
for e in edges:
    if e.get("type") != "DEPENDS_ON":
        continue
    f, t = str(e["fromNodeId"]), str(e["toNodeId"])
    deps_of.setdefault(f, []).append(t)

# A dependency is "satisfied" if it's done/cancelled OR it's been
# placed in an earlier layer.
def deps_satisfied(node_id, placed):
    for dep_id in deps_of.get(node_id, []):
        dep = tasks.get(dep_id)
        if not dep:
            continue
        if dep["status"] in ("done", "cancelled"):
            continue
        if dep_id in placed:
            continue
        return False
    return True

# Pending unclaimed tasks are the work pool.
pool = {nid for nid, t in tasks.items()
        if t["status"] == "pending" and t["id"] not in claimed_for_tasks}

layers = []
placed = set()
remaining = set(pool)

# Layered topological sort.
while remaining:
    layer = sorted(
        [nid for nid in remaining if deps_satisfied(nid, placed)],
        key=lambda n: int(n),
    )
    if not layer:
        break   # everything left is a cycle or blocked-by-non-pending
    layers.append(layer)
    placed.update(layer)
    remaining.difference_update(layer)

# Report.
if not layers:
    print("no pending unclaimed Tasks; nothing to schedule.")
    sys.exit(0)

print(f"parallel-execution plan ({len(layers)} layer(s), "
      f"{sum(len(l) for l in layers)} pending Task(s)):")
print()
for i, layer in enumerate(layers):
    print(f"  layer {i} ({len(layer)} parallel):")
    for nid in layer:
        t = tasks[nid]
        deps = deps_of.get(nid, [])
        dep_strs = []
        for d in deps:
            dt = tasks.get(d)
            if dt and dt["status"] not in ("done", "cancelled"):
                dep_strs.append(dt["id"])
        suffix = f"  (waits on: {', '.join(dep_strs)})" if dep_strs else ""
        tid = t["id"]; track = t["track"]
        print(f"    {tid:50s} (track={track}){suffix}")
    print()

if remaining:
    print(f"NOT scheduled ({len(remaining)} Task(s) — likely cycle members or blocked-by-blocked):")
    for nid in sorted(remaining, key=int):
        t = tasks[nid]
        tid = t["id"]; track = t["track"]
        deps = deps_of.get(nid, [])
        dep_str = ", ".join(tasks[d]["id"] for d in deps if d in tasks) or "(no deps — bug?)"
        print(f"    {tid:50s} (track={track})  deps: {dep_str}")
PYEOF
```

## Examples

**With no DEPENDS_ON edges (current state):**

```
parallel-execution plan (1 layer(s), 6 pending Task(s)):

  layer 0 (6 parallel):
    graphdb:F1.1-spike                                 (track=F)
    graphdb:H2                                         (track=H)
    graphdb:F3                                         (track=F)
    graphdb:F1.1-impl                                  (track=F)
    graphdb:A8.1                                       (track=A)
    graphdb:S1                                         (track=S)
```

All 6 pending Tasks are layer 0 because no DEPENDS_ON edges exist. In parallel-agent terms: any 6 agents could each take one. (For our purposes: spawn 6 worktrees; each is independent.)

**After seeding DEPENDS_ON (hypothetical):**

```
parallel-execution plan (2 layer(s), 6 pending Task(s)):

  layer 0 (3 parallel):
    graphdb:F1.1-spike                                 (track=F)
    graphdb:H2                                         (track=H)
    graphdb:S1                                         (track=S)

  layer 1 (3 parallel):
    graphdb:F1.1-impl                                  (track=F)  (waits on: graphdb:F1.1-spike)
    graphdb:F3                                         (track=F)  (waits on: graphdb:F1.1-spike)
    graphdb:A8.1                                       (track=A)  (waits on: graphdb:H2)
```

3 agents work in parallel on layer 0; once F1.1-spike + H2 close, layer 1's 3 Tasks unlock.

## Mapping clusters to worktrees

Common pattern after running this:

```bash
# Take all of layer 0 in parallel via N worktrees.
LAYER0_IDS=(graphdb:F1.1-spike graphdb:H2 graphdb:S1)   # from the report
for tid in "${LAYER0_IDS[@]}"; do
  # bare task id without project prefix for worktree-spawn
  bare=${tid#*:}
  # spawn each via worktree-spawn (which calls work-claim internally)
  echo "spawn worktree for $bare"
  # ... (manual or scripted invocation of worktree-spawn skill)
done
```

In a single human session you typically don't want 6 simultaneous worktrees — disk and cognitive load. The skill is most valuable when you're choosing *between* layer 0 candidates ("show me what's parallelizable, I'll pick 2-3").

## What this skill does NOT do

- **Doesn't claim anything.** Read-only. Pipe layer-0 IDs into separate `work-claim` invocations to take them.
- **Doesn't enforce that claims happen in order.** The layering is informational. Two agents can both take Layer 0 Tasks simultaneously — that's the point. But if an agent ignores the layering and takes a Layer 1 Task whose Layer 0 dependency is still pending, B-lite uniqueness won't stop them — they'll succeed at the claim and find the Task is logically blocked.
- **Doesn't auto-detect cycles** beyond surfacing them as "NOT scheduled." Cycle resolution is a human concern.
- **Doesn't filter by track.** Want only Track-F? `coord-clusters | grep '(track=F)'`.
- **Doesn't account for `:SUBTASK_OF`.** A parent and its subtasks both appear as independent Tasks in the layering. Future enhancement: hierarchy-aware mode that groups parent's subtasks before promoting the parent.

## Coordination with other skills

- **`coord-next`**: returns one Task; `coord-clusters` returns the full layered plan. Use `coord-next` for single-agent flow, `coord-clusters` for multi-agent.
- **`work-claim`**: each Task in layer 0 is independently claimable. Atomic uniqueness on each.
- **`merge-coordinator`**: also walks DEPENDS_ON, but for *open PRs* not pending Tasks. The two skills cover different time windows: clusters is "what should I start now," merge-coordinator is "what should I merge now."

## Pre-flight checks

- [ ] Coord daemon reachable.
- [ ] At least one Task with `status=pending`.
- [ ] If you're going to spawn parallel worktrees, you have the disk + working memory for it (most people don't run >3 simultaneous worktrees comfortably).

## Limitations

- Reads the entire `/nodes` and `/edges` surface every invocation. Fine at coord scale; would be slow at >1000 Tasks. A dedicated GraphQL `clusters` resolver could pre-compute the layering server-side; not built.
- Layering is a static snapshot. If new Tasks are added or status changes mid-execution, re-run.
- The "NOT scheduled" bucket is currently a catch-all for both true cycles and "blocked-by-something-not-pending" (e.g., a Task whose dep is in `blocked` or `deferred` state). Distinguishing these is straightforward (check the dep's status) but not currently surfaced separately.
