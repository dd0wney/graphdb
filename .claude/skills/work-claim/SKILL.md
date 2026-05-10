---
name: work-claim
description: Atomically claim a planning-doc task ID via the graphdb coord instance so multiple parallel agents don't start the same work. Operates on a separate graphdb instance running the coord schema (Project/Task/Agent/Claim nodes); see docs/COORD_SETUP.md. Use when picking up any task from NEXT_STEPS_<DATE>.md before substantive work begins, or when the user says "claim X," "I'm taking X," "start X." Returns success (you own the task) or failure (someone else does — pick a different task or coordinate). Atomicity is enforced server-side by the B-lite uniqueness rule on :Claim.for_task; concurrent claims for the same task fail with a structured error.
---

# Work claim

Claim a task before starting it. Prevents parallel agents from racing on the same scope. Backed by a separate graphdb coord instance — the project dogfoods its own product.

## When to invoke

- Before starting any planning-doc task (e.g. `H4-PR3-skill-rewrite`, `F1.1-spike`) when ≥2 agents may be active on this repo.
- The user explicitly says "claim X" or "I'm taking X."
- After `worktree-spawn` (which calls this skill internally) when adopting a task ID for the new worktree.

## What the coord schema looks like

Multi-project schema (PR #89). Per-project task IDs are stored as `<project>:<task-id>`, e.g. `graphdb:H4-PR3-skill-rewrite`.

- `:Project { id, name, repo_url }` — one per repo using this coord daemon.
- `:Task { id, track, status, created_at, closing_prs? }` — one per planning-doc task. `id` is project-prefixed.
- `:Agent { id, host, started_at }` — one per active agent session.
- `:Claim { for_task, started_at, expected_duration }` — links an Agent to a Task. **`for_task` is the project-prefixed Task id, NOT the Task node's numeric ID.** B-lite enforces "at most one :Claim per `for_task` per tenant" at the storage layer.

Edges:

- `(Agent)-[:HOLDS]->(Claim)`
- `(Claim)-[:FOR]->(Task)`
- `(Task)-[:IN_PROJECT]->(Project)` — written by `coord-seed.sh`.
- `(Task)-[:DEPENDS_ON]->(Task)` — used by `merge-coordinator`.
- `(Task)-[:CLOSED_BY]->(PR { number })` — written when the work PR merges.

See `docs/COORD_SETUP.md` for instance setup and `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` §5.2 for the atomicity design.

## Pre-flight

Before running any of the bash blocks below, ensure:

```bash
# Bootstrap if not running. Idempotent.
bash scripts/coord-bootstrap.sh

# These two come from coord-bootstrap.sh and are required for every call.
export GRAPHDB_COORD_URL="${GRAPHDB_COORD_URL:-http://localhost:8090}"
export GRAPHDB_COORD_TOKEN="${GRAPHDB_COORD_TOKEN:-$(cat ~/.graphdb-coord-key)}"

# Sanity: daemon is up.
curl -fsS "$GRAPHDB_COORD_URL/health" >/dev/null || {
  echo "coord daemon not reachable on $GRAPHDB_COORD_URL — re-run scripts/coord-bootstrap.sh" >&2
  exit 1
}
```

## Process

### 1. Auto-detect `COORD_PROJECT` from the git remote

Mirrors what `scripts/coord-seed.sh` does — same logic so a Task seeded by the seed script can be claimed without manual configuration.

```bash
detect_project() {
  local url
  url=$(git remote get-url origin 2>/dev/null) || return 1
  echo "${url##*/}" | sed 's/\.git$//'
}
COORD_PROJECT="${COORD_PROJECT:-$(detect_project)}"
[[ -z "$COORD_PROJECT" ]] && { echo "set COORD_PROJECT — git remote auto-detect failed" >&2; exit 1; }
```

### 2. Compose the prefixed Task ID

Bare task ID from the planning doc (e.g. `H4-PR3-skill-rewrite`); the prefixed form is what the coord stores.

```bash
TASK_ID_BARE="H4-PR3-skill-rewrite"   # whatever the user requested
TASK_ID="${COORD_PROJECT}:${TASK_ID_BARE}"
```

### 3. Generate or load a stable agent ID

Cached so successive sessions on the same machine reuse it; rotates only if the file is deleted.

```bash
AGENT_ID_FILE="$HOME/.claude/agent-id"
mkdir -p "$(dirname "$AGENT_ID_FILE")"
if [[ ! -s "$AGENT_ID_FILE" ]]; then
  # `agent-<short-host>-<random-hex>` — readable in the coord query output
  printf 'agent-%s-%s\n' \
    "$(hostname -s | tr '[:upper:]' '[:lower:]')" \
    "$(openssl rand -hex 4)" > "$AGENT_ID_FILE"
  chmod 600 "$AGENT_ID_FILE"
fi
AGENT_ID=$(cat "$AGENT_ID_FILE")
```

### 4. Find or create the :Agent node

REST `/nodes` GET returns string properties as base64 (H4.1 bug — open follow-up). Decode in a Python one-liner.

```bash
NODES_JSON=$(curl -fsS -H "X-API-Key: $GRAPHDB_COORD_TOKEN" "$GRAPHDB_COORD_URL/nodes")

AGENT_NODE_ID=$(echo "$NODES_JSON" | python3 -c "
import json, sys, base64
target = '$AGENT_ID'
for n in json.load(sys.stdin):
    if 'Agent' in n.get('labels', []):
        raw = n['properties'].get('id', '')
        try:
            decoded = base64.b64decode(raw).decode('utf-8')
        except Exception:
            decoded = raw
        if decoded == target:
            print(n['id']); break
")

if [[ -z "$AGENT_NODE_ID" ]]; then
  AGENT_PROPS=$(printf '{"id":"%s","host":"%s","started_at":"%s"}' \
    "$AGENT_ID" "$(hostname -s)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)")
  AGENT_NODE_ID=$(curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
    -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/nodes" \
    -d "$(printf '{"labels":["Agent"],"properties":%s}' "$AGENT_PROPS")" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
fi
```

### 5. Confirm the :Task exists for `$TASK_ID`

If not present, the planning doc and coord are out of sync — refuse to claim. Fix is to re-run `scripts/coord-seed.sh` or to surface the planning-doc gap.

```bash
TASK_NODE_ID=$(echo "$NODES_JSON" | python3 -c "
import json, sys, base64
target = '$TASK_ID'
for n in json.load(sys.stdin):
    if 'Task' in n.get('labels', []):
        raw = n['properties'].get('id', '')
        try:
            decoded = base64.b64decode(raw).decode('utf-8')
        except Exception:
            decoded = raw
        if decoded == target:
            print(n['id']); break
")

if [[ -z "$TASK_NODE_ID" ]]; then
  echo "no :Task node for $TASK_ID — re-seed via 'bash scripts/coord-seed.sh' or fix the planning doc" >&2
  exit 1
fi
```

### 6. Attempt to atomically create the :Claim

This is the load-bearing call. The B-lite resolver enforces uniqueness on `:Claim.for_task` server-side, so two concurrent claims for the same task can't both succeed. Use the GraphQL `createNode` mutation — REST `POST /nodes` does NOT route through the B-lite check.

The GraphQL request has three levels of JSON nesting (request body → `query` string → `properties` JSON-string). Construct the body in Python; bash printf can't track the escaping correctly and silently produces malformed payloads.

```bash
PAYLOAD=$(TASK_ID="$TASK_ID" EXPECTED_DURATION="${EXPECTED_DURATION:-4h}" python3 -c "
import json, os, datetime
props = {
    'for_task': os.environ['TASK_ID'],
    'started_at': datetime.datetime.now(datetime.UTC).strftime('%Y-%m-%dT%H:%M:%SZ'),
    'expected_duration': os.environ['EXPECTED_DURATION'],
}
query = 'mutation { createNode(labels: [\"Claim\"], properties: ' + json.dumps(json.dumps(props)) + ') { id } }'
print(json.dumps({'query': query}))
")

CLAIM_RESP=$(curl -sS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/graphql" -d "$PAYLOAD")

# Parse the response: success → Claim node id; conflict → conflicting node id; other error → surface verbatim.
PARSED=$(echo "$CLAIM_RESP" | python3 -c "
import json, sys, re
d = json.loads(sys.stdin.read())
errs = d.get('errors') or []
if errs:
    msg = errs[0].get('message', '')
    if 'unique constraint' in msg:
        m = re.search(r'node (\\d+)', msg)
        print('CONFLICT', m.group(1) if m else '?')
    else:
        print('ERROR', msg)
else:
    print('OK', d['data']['createNode']['id'])
")

case "$PARSED" in
  CONFLICT*)
    CONFLICT_NODE_ID=$(echo "$PARSED" | awk '{print $2}')
    echo "task $TASK_ID is already claimed (by :Claim node $CONFLICT_NODE_ID)" >&2
    echo "investigate via the edges query — trace the HOLDS edge from this Claim back to the holding Agent." >&2
    exit 1
    ;;
  ERROR*)
    echo "claim failed: ${PARSED#ERROR }" >&2
    exit 1
    ;;
  OK*)
    CLAIM_NODE_ID=$(echo "$PARSED" | awk '{print $2}')
    ;;
esac
```

### 7. Wire the edges (HOLDS and FOR)

Both via REST `/edges` (mirrors `coord-seed.sh`). These are not atomic with the Claim creation — if the daemon dies between steps 6 and 7, the Claim node lingers without edges and looks orphaned. The "stale claim cleanup" sweep handles this; it's a tolerated rare case for now.

```bash
curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/edges" \
  -d "$(printf '{"type":"HOLDS","from_node_id":%s,"to_node_id":%s}' "$AGENT_NODE_ID" "$CLAIM_NODE_ID")" >/dev/null

curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/edges" \
  -d "$(printf '{"type":"FOR","from_node_id":%s,"to_node_id":%s}' "$CLAIM_NODE_ID" "$TASK_NODE_ID")" >/dev/null
```

### 8. Report success

Surface the IDs to the user so they can investigate later if needed:

```
claimed: $TASK_ID
  agent node: $AGENT_NODE_ID  ($AGENT_ID)
  claim node: $CLAIM_NODE_ID
  task node:  $TASK_NODE_ID
proceed.
```

## Releasing the claim

When the work PR merges, mark the Task closed by adding a `:CLOSED_BY` edge from Task → PR, then delete the Claim (cascades HOLDS + FOR via storage's edge-cleanup).

```bash
PR_NUMBER=91   # whatever the merging PR is

# 1. Create the PR node if it doesn't exist (idempotent — skips if found).
PR_NODE_ID=$(curl -fsS -H "X-API-Key: $GRAPHDB_COORD_TOKEN" "$GRAPHDB_COORD_URL/nodes" | python3 -c "
import json, sys, base64
target = $PR_NUMBER
for n in json.load(sys.stdin):
    if 'PR' in n.get('labels', []):
        raw = n['properties'].get('number', '')
        try:
            decoded = int(base64.b64decode(raw))   # PR.number is int — encoded differently than strings
        except Exception:
            try: decoded = int(raw)
            except Exception: continue
        if decoded == target: print(n['id']); break
")
if [[ -z "$PR_NODE_ID" ]]; then
  PR_NODE_ID=$(curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
    -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/nodes" \
    -d "$(printf '{"labels":["PR"],"properties":{"number":%d}}' "$PR_NUMBER")" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
fi

# 2. Add the :CLOSED_BY edge.
curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/edges" \
  -d "$(printf '{"type":"CLOSED_BY","from_node_id":%s,"to_node_id":%s}' "$TASK_NODE_ID" "$PR_NODE_ID")" >/dev/null

# 3. Delete the Claim node (HOLDS + FOR edges go with it).
curl -fsS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" \
  -H 'Content-Type: application/json' "$GRAPHDB_COORD_URL/graphql" \
  -d "$(printf '{"query":"mutation { deleteNode(id: \\"%s\\") { success } }"}' "$CLAIM_NODE_ID")" >/dev/null
```

For abandoned work (no PR ever merged): skip the `:CLOSED_BY` edge, just delete the Claim with a note in the user-visible report.

## Stale claim cleanup

Claims with `started_at` older than ~24h whose Task has no `:CLOSED_BY` edge are stale. Out of scope for this skill, but the query shape:

```graphql
{
  claims { id properties }
}
```

Filter client-side on `started_at` (still base64-encoded due to H4.1) and check whether the Task has a `:CLOSED_BY` edge. File `coord-stale-sweep` as a follow-up if it becomes routine.

## What this skill does NOT do

- **Doesn't enforce that agents respect the claim.** Coordination, not access control. An agent that ignores the coord and starts the task anyway will produce a conflicting PR; the human resolves.
- **Doesn't claim sub-scopes.** Whole tasks only. Decomposition is a planning-doc concern (`/plan`), not a claim split.
- **Doesn't expire claims automatically.** See "Stale claim cleanup."
- **Doesn't seed missing :Tasks.** If the Task node isn't in coord, the user must run `scripts/coord-seed.sh` (or update the planning doc) — surfacing the gap is the right behavior, not papering over it.
- **Doesn't create the :Project node.** That's `coord-seed.sh`'s job; if it's missing, coord hasn't been seeded for this repo at all.

## Edge cases

- **Coord daemon unreachable.** Hard-fail with a pointer to `scripts/coord-bootstrap.sh`. Don't degrade silently to "no coordination" — that defeats the purpose.
- **Task node doesn't exist for the requested ID.** Refuse and surface the planning-doc gap. Don't auto-create — that masks coord drift.
- **Claim succeeds but `worktree-spawn` fails.** Release the claim immediately (delete the Claim node — see "Releasing the claim") so the task is available again.
- **Agent crashes mid-task.** The Claim lingers. Stale-sweep is the future fix.
- **API key has rotated.** `~/.graphdb-coord-key` is stale; re-run `scripts/coord-bootstrap.sh` to mint a new one.

## Pre-flight checks

- [ ] `scripts/coord-bootstrap.sh` has run and the daemon is healthy.
- [ ] `GRAPHDB_COORD_URL` and `GRAPHDB_COORD_TOKEN` are exported.
- [ ] `git remote get-url origin` succeeds (or `COORD_PROJECT` is set explicitly).
- [ ] Task ID exists in `docs/NEXT_STEPS_<DATE>.md` AND has been seeded into coord.
- [ ] You're on a clean working tree (don't claim a task you can't immediately start).

## Why GraphQL for the Claim and REST for everything else

Two endpoints, one project. Reasoning:

- **B-lite uniqueness lives in the GraphQL `createNode` resolver.** REST `POST /nodes` bypasses it. So Claim creation MUST go through GraphQL to get atomicity.
- **REST `POST /nodes` is simpler for non-uniqueness writes** (Agent, PR). No JSON-string-in-JSON-arg gymnastics.
- **REST `GET /nodes`** is the only way to list nodes by label without chasing the schema-cache regenerate dance — cheap to scan client-side at coord scale (≤100 tasks).
- **GraphQL `{ edges { ... } }`** is the only way to read edges (REST `GET /edges` returns 405).

This split is documented here so future maintainers don't try to unify it; both endpoints have legitimate jobs.
