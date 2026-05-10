---
name: work-claim
description: Atomically claim a planning-doc task ID via the graphdb coord instance so multiple parallel agents don't start the same work. Operates on a separate graphdb instance running coord schema (Task/Agent/Claim nodes); see docs/COORD_SETUP.md. Use when picking up any task from NEXT_STEPS_<DATE>.md before substantive work begins, or when the user says "claim X," "I'm taking X," "start X." Returns success (you own the task) or failure (someone else does — pick a different task or coordinate). Atomic POST against /v1/nodes + /v1/edges; no PR overhead per claim.
---

# Work claim

Claim a task before starting it. Prevents parallel agents from racing on the same scope. Backed by a separate graphdb coord instance (the project dogfoods its own product for this).

## When to invoke

- Before starting any planning-doc task (`A8.2`, `H2`, `F1.1-spike`, etc.) when ≥2 agents may be active on this repo.
- The user explicitly says "claim X" or "I'm taking X."
- After `worktree-spawn` (which calls this skill internally) when adopting a task ID for the new worktree.

## How the claim works

A separate graphdb instance (the **coord instance**) runs the coord schema:

- `:Task { id, track, status }` — one per planning-doc task ID
- `:Agent { id, host, started_at }` — one per active agent session
- `:Claim { id, started_at, expected_duration }` — connecting the two

Edges:

- `(Agent)-[:HOLDS]->(Claim)`
- `(Claim)-[:FOR]->(Task)`
- `(Task)-[:DEPENDS_ON]->(Task)` — for `merge-coordinator` traversal
- `(Task)-[:CLOSED_BY]->(PR { number })` — written by the work PR's merge

A claim is **a single atomic POST** that creates the Claim node + the HOLDS and FOR edges in one transaction. Concurrent claims on the same Task fail at the constraint layer (the coord instance enforces "at most one active Claim per Task" — see `pkg/constraints` for the uniqueness primitive).

See `docs/COORD_SETUP.md` for instance setup, schema bootstrap, and authentication.

## Process

1. **Verify the coord instance is reachable**:
   ```bash
   curl -fsS "${GRAPHDB_COORD_URL}/health" || (echo "coord instance not reachable; see docs/COORD_SETUP.md" && exit 1)
   ```
2. **Authenticate** with the coord-agent JWT (`GRAPHDB_COORD_TOKEN` env var; setup in `docs/COORD_SETUP.md`).
3. **Generate or load agent ID**. Format: `agent-<user>-<short-uuid>` or `agent-<host>-<pid>`. Stable for the life of the agent's session. Cache in a local file under `~/.claude/agent-id` so successive sessions on the same machine reuse it.
4. **Ensure Task node exists**:
   ```bash
   curl -fsS -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
     "${GRAPHDB_COORD_URL}/v1/nodes/by-property?label=Task&id=${TASK_ID}" \
     | jq -e '.[0]' > /dev/null || \
     # Create it from the planning doc
     curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
       "${GRAPHDB_COORD_URL}/v1/nodes" -d "{\"label\":\"Task\",\"properties\":{\"id\":\"${TASK_ID}\",\"track\":\"${TRACK}\",\"status\":\"open\"}}"
   ```
5. **Attempt to create the Claim** (atomic with the HOLDS and FOR edges via batch):
   ```bash
   curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
     "${GRAPHDB_COORD_URL}/v1/batch" -d @- <<EOF
   {
     "operations": [
       {"op":"create_node","label":"Claim","properties":{"started_at":"$(date -u +%FT%TZ)","expected_duration":"${DURATION}"}},
       {"op":"create_edge","from_label":"Agent","from_properties":{"id":"${AGENT_ID}"},"to_node":"<claim>","type":"HOLDS"},
       {"op":"create_edge","from_node":"<claim>","to_label":"Task","to_properties":{"id":"${TASK_ID}"},"type":"FOR"}
     ]
   }
   EOF
   ```
   The coord instance enforces "at most one active Claim per Task" via a uniqueness constraint. If another agent already has an active claim, this returns 409 Conflict.
6. **On 409 Conflict**: query for the existing claim, report it to the user including the holding agent ID. Abort.
7. **On success**: report claim ID + task + worktree path. Proceed.

## Releasing the claim

When the work PR merges, mark the Claim as released by adding the `:CLOSED_BY` edge from the Task to the PR:

```bash
curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "${GRAPHDB_COORD_URL}/v1/edges" -d "{
    \"from_label\":\"Task\",\"from_properties\":{\"id\":\"${TASK_ID}\"},
    \"to_label\":\"PR\",\"to_properties\":{\"number\":${PR_NUMBER}},
    \"type\":\"CLOSED_BY\"
  }"
```

Then delete the Claim node (cascades the HOLDS + FOR edges):

```bash
curl -fsS -X DELETE -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "${GRAPHDB_COORD_URL}/v1/nodes/${CLAIM_ID}"
```

For abandoned work: skip the CLOSED_BY edge, just delete the Claim node with a note in the deletion request.

## Stale claim cleanup (separate skill or manual)

Claims with `started_at` older than ~24h with no associated PR are stale. A periodic sweep:

```cypher
MATCH (a:Agent)-[:HOLDS]->(c:Claim)-[:FOR]->(t:Task)
WHERE c.started_at < datetime() - duration({hours: 24})
  AND NOT (t)-[:CLOSED_BY]->(:PR)
RETURN a.id, t.id, c.started_at
```

Cleanup itself is out of scope for this skill — file `coord-stale-sweep` as a follow-up if it becomes routine.

## What this skill does NOT do

- **Doesn't enforce that agents respect the claim.** Coordination, not access control. An agent that ignores the coord instance and starts the task anyway will produce a conflicting PR; the human resolves.
- **Doesn't claim sub-scopes.** Whole tasks only. Decomposition is a planning-doc task (`/plan`), not a claim split.
- **Doesn't expire claims automatically.** See "Stale claim cleanup."
- **Doesn't replace coordination conventions** from `~/.claude/CLAUDE.md` ("Never modify shared interfaces without explicit coordination," etc.). It's the mechanism; the rules are still the rules.

## Edge cases

- **Coord instance unreachable.** Hard-fail with a pointer to `docs/COORD_SETUP.md`. Don't degrade silently to "no coordination" — that defeats the purpose.
- **Task node doesn't exist for the requested ID.** Either the planning doc is stale or the task hasn't been seeded into coord yet. Skill creates it (step 4 above) so the first agent for any task seeds it; subsequent agents find it.
- **Claim succeeds but worktree-spawn fails.** Release the claim immediately (delete the Claim node) so the task is available again.
- **Agent crashes mid-task.** The Claim node lingers. Stale-sweep handles it.

## Pre-flight checks

- [ ] `GRAPHDB_COORD_URL` and `GRAPHDB_COORD_TOKEN` env vars set (see `docs/COORD_SETUP.md`).
- [ ] Coord instance health-check returns 200.
- [ ] Task ID exists in `docs/NEXT_STEPS_<DATE>.md` (don't claim things that aren't on the planning doc — surface a planning-doc update instead).
- [ ] You're on `main` with a clean working tree.
