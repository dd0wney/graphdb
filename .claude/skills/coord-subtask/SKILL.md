---
name: coord-subtask
description: Decompose a Task into subtasks via :SUBTASK_OF edges. Use when a planning-doc task is too big to claim atomically and needs sub-scope breakdown without forcing it into the planning doc itself. The skill creates a child :Task with project-prefixed id `<parent-id>.<n>` (e.g. graphdb:F1.1-spike.1, .2, .3) and a :SUBTASK_OF edge from child to parent. Borrowed from Taskmaster's subtask convention per docs/COMPARE_TASKMASTER_2026-05-10.md §7. Subtasks are themselves first-class :Tasks — claimable, statusable, dependency-able.
---

# Coord subtask

Add a subtask under an existing parent Task. The parent stays a single planning-doc entry; subtasks are how an agent decomposes the work without bloating the planning doc.

## When to invoke

- A pending Task is too big to claim atomically — break it into 2-5 subtasks first, then claim each.
- The user says "split X into N subtasks" or "decompose F1.1-spike."
- Mid-task, an agent realizes the scope has multiple separable threads — pause, file subtasks, claim them sequentially.
- After running `coord-next` and getting back a Task you'd rather break down than claim whole.

## Convention

- Subtask `id` is `<parent-id>.<n>` where `<n>` is the next free integer for that parent. So `graphdb:F1.1-spike.1`, `.2`, `.3` — readable, sortable, parent-implicit.
- Subtask carries the parent's `track` by default (override only if the subtask logically belongs to a different track, which is rare — surface to the user before deviating).
- A `:SUBTASK_OF` edge connects child Task → parent Task. **One-way**, child to parent — mirrors how the planning doc reads ("F1.1-spike has subtasks 1, 2, 3").
- Subtasks default to `status=pending` and inherit nothing else from the parent. Dependencies, claims, blockers are per-subtask.
- The parent Task's status is unchanged when subtasks are added. The parent is "done" when the user explicitly marks it so; aggregate-from-subtasks logic is intentionally not automatic (Taskmaster does this; we're not adopting it because it leaks subtask state into parent semantics).

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
# Inputs
PARENT_TASK_ID="graphdb:F1.1-spike"   # full prefixed id
SUBTASK_TITLE="design API surface"     # human-readable; stored on the subtask

# 1. Look up the parent and its existing subtasks (to compute next .n).
NODES=$(curl -fsS -H "X-API-Key: $GRAPHDB_COORD_TOKEN" "$GRAPHDB_COORD_URL/nodes")
EDGES=$(curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/graphql" \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}')

PLAN=$(NODES="$NODES" EDGES="$EDGES" PARENT="$PARENT_TASK_ID" python3 - <<'PYEOF'
import json, os, sys, base64
def decode(v):
    try: return base64.b64decode(v).decode("utf-8")
    except Exception: return v

target = os.environ["PARENT"]
nodes = json.loads(os.environ["NODES"])
edges = json.loads(os.environ["EDGES"]).get("data", {}).get("edges", [])

# Find parent node id + track.
parent_nid = None; parent_track = ""
all_tasks_by_id = {}   # task_string_id -> node_id
for n in nodes:
    if "Task" not in n.get("labels", []):
        continue
    tid = decode(n["properties"].get("id", ""))
    all_tasks_by_id[tid] = str(n["id"])
    if tid == target:
        parent_nid = str(n["id"])
        parent_track = decode(n["properties"].get("track", ""))

if not parent_nid:
    print(f"ERROR no parent Task with id={target}", file=sys.stderr)
    sys.exit(1)

# Walk SUBTASK_OF edges pointing at the parent; find existing subtask ids.
existing_n = set()
for e in edges:
    if e.get("type") != "SUBTASK_OF":
        continue
    if str(e["toNodeId"]) != parent_nid:
        continue
    # The from-task's id should be `<parent>.<n>` — extract n.
    from_nid = str(e["fromNodeId"])
    for tid, nid in all_tasks_by_id.items():
        if nid == from_nid and tid.startswith(target + "."):
            try:
                existing_n.add(int(tid.rsplit(".", 1)[1]))
            except ValueError:
                pass
            break

next_n = max(existing_n) + 1 if existing_n else 1
print(f"PARENT_NID={parent_nid}")
print(f"PARENT_TRACK={parent_track}")
print(f"NEXT_N={next_n}")
PYEOF
)

# Parse plan output into shell vars.
PARENT_NID=$(echo "$PLAN" | grep '^PARENT_NID=' | cut -d= -f2)
PARENT_TRACK=$(echo "$PLAN" | grep '^PARENT_TRACK=' | cut -d= -f2)
NEXT_N=$(echo "$PLAN" | grep '^NEXT_N=' | cut -d= -f2)
SUBTASK_ID="${PARENT_TASK_ID}.${NEXT_N}"

# 2. Create the subtask :Task via REST POST /nodes (no uniqueness needed —
#    .n suffix is computed from existing subtasks, so collisions are an
#    on-machine race, not the typical case).
SUBTASK_PROPS=$(SUBTASK_ID="$SUBTASK_ID" PARENT_TRACK="$PARENT_TRACK" SUBTASK_TITLE="$SUBTASK_TITLE" python3 -c "
import json, os
print(json.dumps({
    'id': os.environ['SUBTASK_ID'],
    'track': os.environ['PARENT_TRACK'],
    'status': 'pending',
    'title': os.environ['SUBTASK_TITLE'],
    'created_at': '$(date -u +%Y-%m-%dT%H:%M:%SZ)',
}))
")
SUBTASK_NID=$(curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/nodes" \
  -d "$(printf '{"labels":["Task"],"properties":%s}' "$SUBTASK_PROPS")" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

# 3. Wire :SUBTASK_OF edge child → parent.
curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/edges" \
  -d "$(printf '{"type":"SUBTASK_OF","from_node_id":%s,"to_node_id":%s}' "$SUBTASK_NID" "$PARENT_NID")" >/dev/null

# 4. Link to the parent's :Project (mirrors coord-seed.sh's IN_PROJECT pattern).
PROJECT_NID=$(echo "$NODES" | python3 -c "
import json, sys, base64
target_id = '$(echo "$PARENT_TASK_ID" | cut -d: -f1)'   # 'graphdb' from 'graphdb:F1.1-spike'
def decode(v):
    try: return base64.b64decode(v).decode('utf-8')
    except: return v
for n in json.load(sys.stdin):
    if 'Project' in n.get('labels', []):
        if decode(n['properties'].get('id', '')) == target_id:
            print(n['id']); break
")
if [[ -n "$PROJECT_NID" ]]; then
  curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
    -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/edges" \
    -d "$(printf '{"type":"IN_PROJECT","from_node_id":%s,"to_node_id":%s}' "$SUBTASK_NID" "$PROJECT_NID")" >/dev/null
fi

echo "subtask created: $SUBTASK_ID"
echo "  task node:   $SUBTASK_NID"
echo "  parent node: $PARENT_NID"
echo "  track:       $PARENT_TRACK (inherited from parent)"
echo "  title:       $SUBTASK_TITLE"
echo "claim it via:  bash work-claim.sh   # or via the work-claim skill"
```

## What this skill does NOT do

- **Doesn't aggregate subtask status into parent.** Parent's `status` is independent. If you want a "all subtasks done → parent done" rule, that's a follow-up skill (or a small storage trigger), not this one's job.
- **Doesn't create DEPENDS_ON edges between siblings.** If subtask `.2` depends on `.1`, file the edge separately. Most decompositions are already-DAG-flat.
- **Doesn't claim the subtask atomically.** Pipe into `work-claim` to take it.
- **Doesn't recurse.** Subtasks of subtasks would chain `.1.1`, `.1.2` etc. The skill *would* work for that (it computes `<parent-id>.<n>` whatever the parent's id is), but the cognitive cost of nested decomposition is usually higher than the value. Don't go past one level without a real reason.
- **Doesn't update the planning doc.** The planning doc lists *parent* tasks; subtasks are a coord-side concern. If a subtask becomes a planning-doc-worthy entry on its own, file a separate planning-doc-update PR.

## Coordination with other skills

- **`work-claim`**: subtasks are first-class Tasks. Claim each one atomically; the B-lite uniqueness rule applies on the `for_task` value (which is the subtask's full id, e.g. `graphdb:F1.1-spike.2`).
- **`coord-next`**: returns subtasks alongside top-level Tasks. The naming convention (`<parent>.<n>`) is sortable, so subtasks group naturally near their parent in the alternatives list. Future enhancement: `coord-next --hierarchy` to surface parent context with each subtask.
- **`merge-coordinator`**: traverses DEPENDS_ON edges, not SUBTASK_OF. So merge-coordinator treats subtasks as flat for ordering purposes. If you need parent-grouped merge order, that's a follow-up.

## Pre-flight checks

- [ ] Coord daemon reachable.
- [ ] Parent Task exists in coord (use `coord-next` or `curl /nodes` to confirm).
- [ ] You actually need decomposition. If the work fits in one PR, claim the parent — don't over-decompose.

## Examples

**Decomposing F1.1-spike into 3 subtasks:**

```
$ PARENT_TASK_ID=graphdb:F1.1-spike SUBTASK_TITLE="design per-tenant LSA index" bash <skill>
subtask created: graphdb:F1.1-spike.1

$ PARENT_TASK_ID=graphdb:F1.1-spike SUBTASK_TITLE="implement build path" bash <skill>
subtask created: graphdb:F1.1-spike.2

$ PARENT_TASK_ID=graphdb:F1.1-spike SUBTASK_TITLE="benchmark vs current" bash <skill>
subtask created: graphdb:F1.1-spike.3
```

After this, `coord-next` may recommend `graphdb:F1.1-spike.1` first (lowest node ID among new pending tasks). Each subtask is independently claimable.
