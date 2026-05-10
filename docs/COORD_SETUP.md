# Parallel-agent coordination instance setup

This document covers how to set up the **graphdb coord instance** that backs the parallel-agent coordination skills (`work-claim`, `worktree-spawn`, `merge-coordinator`).

The coord instance is **a separate graphdb process from your dev work**. It runs from a stable build (typically the latest tagged release or `main` from a recent known-good commit), not from the version-under-development. This isolation means a bad PR touching `pkg/storage` affects your dev instance, not coordination.

This is dogfooding: graphdb coordinates its own development with graphdb. The coord workload is a small but realistic multi-tenant graph workload — typed entities (Tasks, Agents), typed relationships (HOLDS, FOR, DEPENDS_ON, CLOSED_BY), tenant isolation, audit history via timestamped edges. Performance numbers from the coord instance are real signal for capacity planning.

## Prerequisites

- A `graphdb` binary built from a stable commit (don't use the version under active development).
- A persistent data directory for the coord instance.
- Network reachability between agent machines and the coord instance (localhost works for single-machine multi-agent; private network or VPN for multi-machine).

## Step 1: Run the coord instance

From the directory containing the `graphdb` binary:

```bash
# Single-user N-agent on localhost (simplest)
./graphdb \
  --data-dir ~/.graphdb-coord \
  --port 8090 \
  --jwt-secret "$(openssl rand -hex 32)" \
  --tenant default

# Or via Docker (any reasonably recent image)
docker run -d --name graphdb-coord \
  -v graphdb-coord-data:/data \
  -p 8090:8090 \
  -e GRAPHDB_JWT_SECRET="$(openssl rand -hex 32)" \
  graphdb:latest
```

For multi-machine: deploy on a small VM, behind TLS, with the JWT secret stored in a shared secret manager. The replication binaries (`graphdb-nng-primary` / `graphdb-nng-replica`) give HA if the coord instance becomes load-bearing for a larger team.

## Step 2: Generate the coord JWT

```bash
# Using the license-server or any JWT-issuing tool
# Token must include: sub=coord-agent, tenant=default, exp=<long expiration>
./license-server issue \
  --jwt-secret "$JWT_SECRET" \
  --subject coord-agent \
  --tenant default \
  --expires-in 90d \
  > ~/.graphdb-coord-token
```

Set environment variables for the agents:

```bash
export GRAPHDB_COORD_URL="http://localhost:8090"
export GRAPHDB_COORD_TOKEN="$(cat ~/.graphdb-coord-token)"
```

The skills read these env vars; persist them in `~/.bashrc` / `~/.zshrc` / equivalent.

## Step 3: Bootstrap the schema

The coord schema is enforced at the constraint layer. From any machine with `curl` and the env vars set:

```bash
# Uniqueness constraint: at most one active Claim per Task
curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "$GRAPHDB_COORD_URL/v1/constraints/uniqueness" -d '{
    "label": "Claim",
    "via_edge": "FOR",
    "to_label": "Task",
    "scope": "active"
  }'

# Property index for fast Task lookup by ID
curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "$GRAPHDB_COORD_URL/v1/property-indexes" -d '{
    "label": "Task",
    "property": "id"
  }'

# Property index for fast Agent lookup
curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "$GRAPHDB_COORD_URL/v1/property-indexes" -d '{
    "label": "Agent",
    "property": "id"
  }'
```

## Step 4: Seed Task nodes from the planning doc

A one-time bootstrap that reads `docs/NEXT_STEPS_<DATE>.md` and creates a `:Task` node per planning-doc task. Optional — `work-claim` lazy-creates Task nodes on first claim, but seeding upfront makes querying the queue from the coord instance work without waiting for first-claim.

```bash
# Sketch — adapt to your planning-doc structure
for task in A8.2 F1.1-spike F1.1-impl F3 A8.1 S1 H2; do
  curl -fsS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
    "$GRAPHDB_COORD_URL/v1/nodes" -d "{
      \"label\": \"Task\",
      \"properties\": {
        \"id\": \"$task\",
        \"track\": \"${task%%-*}\",
        \"status\": \"open\"
      }
    }"
done
```

If the planning doc captures dependencies (e.g., "F1.1-impl depends on F1.1-spike"), seed the `:DEPENDS_ON` edges too — `merge-coordinator` traverses them for ordering.

## Schema reference

```
:Task     { id: string!, track: string, status: "open" | "in-progress" | "done" }
:Agent    { id: string!, host: string, started_at: timestamp }
:Claim    { id: uuid, started_at: timestamp, expected_duration: duration }
:PR       { number: int!, title: string, merged_at: timestamp }

(Agent) -[:HOLDS]-> (Claim) -[:FOR]-> (Task)
(Task)  -[:DEPENDS_ON]-> (Task)
(Task)  -[:CLOSED_BY]-> (PR)
```

The constraint "at most one active Claim per Task" is enforced via the uniqueness constraint set in Step 3. "Active" here means a Claim node whose Task has no `:CLOSED_BY` edge yet.

## Common queries

```cypher
# Who's working on what right now?
MATCH (a:Agent)-[:HOLDS]->(:Claim)-[:FOR]->(t:Task)
WHERE NOT (t)-[:CLOSED_BY]->(:PR)
RETURN a.id, t.id

# Show stale claims (>24h, no closing PR)
MATCH (a:Agent)-[:HOLDS]->(c:Claim)-[:FOR]->(t:Task)
WHERE c.started_at < datetime() - duration({hours: 24})
  AND NOT (t)-[:CLOSED_BY]->(:PR)
RETURN a.id, t.id, c.started_at

# What's blocking task F1.1-impl?
MATCH (t:Task {id: "F1.1-impl"})-[:DEPENDS_ON]->(d:Task)
WHERE NOT (d)-[:CLOSED_BY]->(:PR)
RETURN d.id

# Recent closure history
MATCH (t:Task)-[:CLOSED_BY]->(p:PR)
RETURN t.id, p.number, p.merged_at
ORDER BY p.merged_at DESC
LIMIT 20
```

## Operating notes

- **Backups**: the coord instance's data is small (~few KB per task) but losing it means losing claim state. Snapshot daily; the in-built snapshot mechanism is sufficient.
- **Upgrades**: only update the coord-instance graphdb binary deliberately. Test on a copy of the data dir first. A coord-instance crash mid-claim shouldn't lose data thanks to WAL, but the running agents may need to retry their last claim attempt.
- **Single-developer mode**: if you're the only developer using parallel agents, `localhost:8090` is sufficient; no need for the full deployment. The skills work the same way against localhost.
- **Health check**: `curl $GRAPHDB_COORD_URL/health` should return 200. Wire this into any agent-startup discipline if you want fail-fast behavior.

## When to scale up

The coord workload is tiny by graphdb's standards (a few writes per day, queries proportional to task count). Single-node graphdb handles it indefinitely. Scale up only if:

- You add real-time UI (web dashboard for current claim state) and need read-replica scaling.
- The coord instance starts being treated as production telemetry source by other tooling.
- Multi-machine coordination grows to >100 active claims simultaneously (very unlikely for sane parallel-agent volumes).

## Recovery

If the coord instance dies and stays dead, parallel-agent coordination falls back to "ask the user." The skills hard-fail when the coord instance is unreachable rather than silently degrading — this is intentional. Restart the instance, point `GRAPHDB_COORD_URL` at the new endpoint, agents resume.

If the coord database is corrupted: restore from snapshot, accept loss of claims since last snapshot, re-claim affected tasks.
