# Parallel-agent coordination instance setup

This document is the **operator-facing how-to** for running a graphdb coord instance that backs the parallel-agent coordination skills (`work-claim`, `worktree-spawn`, `merge-coordinator`). For the analysis behind the design, see `docs/COORD_DEPLOY_SPIKE_2026-05-10.md`.

The coord instance is **a separate graphdb process from your dev work**. It runs `cmd/server` from a known-good commit so bugs in your in-flight code don't take down coord. Same binary tree though — when this repo's `main` is green, that's the coord build.

This is dogfooding: graphdb coordinates its own development with graphdb. The coord workload is small but real (typed `:Task`/`:Agent`/`:Claim`/`:PR` nodes, `:HOLDS`/`:FOR`/`:DEPENDS_ON`/`:CLOSED_BY` edges) — exactly the workload graphdb's traversal-first design is for.

> **Replaces the aspirational version of this doc that pre-dated implementation.** The previous version referenced `POST /v1/constraints/uniqueness`, `POST /v1/property-indexes`, `POST /v1/batch`, `GET /v1/nodes/by-property`, and a `license-server issue` subcommand — none of which exist. The findings are documented in `docs/COORD_GAP_2026-05-10.md`; this rewrite uses only what `cmd/server` actually exposes.

## Quick start (single-developer, localhost)

Two scripts in `scripts/` codify the full setup:

```bash
# From the graphdb repo root
bash scripts/coord-bootstrap.sh    # builds cmd/server, starts daemon on :8090, mints API key
bash scripts/coord-seed.sh         # seeds :Project + :Task nodes from the planning doc

# Persist the env vars in your shell rc:
export GRAPHDB_COORD_URL="http://localhost:8090"
export GRAPHDB_COORD_TOKEN="$(cat ~/.graphdb-coord-key)"
```

Both scripts are **idempotent** — safe to re-run after a daemon restart, machine reboot, or just to verify state.

`coord-seed.sh` requires `COORD_PROJECT` (auto-detected from `git remote get-url origin`; override via env var). See **Multi-project coord** below for the schema and cross-project semantics.

To stop the daemon: `kill $(lsof -t -i:8090)`. Data persists in `~/.graphdb-coord-data`.

## What `coord-bootstrap.sh` does

1. Builds `cmd/server` to `/tmp/graphdb-coord` if missing or stale (compares mtime against `cmd/server/*.go`).
2. Creates the data dir (`~/.graphdb-coord-data` by default).
3. Starts the daemon on `:8090` if nothing's already listening. Daemon runs in `nohup`, logs to `~/.graphdb-coord-data/server.log`.
4. Waits up to 20s for the daemon to be reachable.
5. If `~/.graphdb-coord-key` exists and authenticates, reuses it. Otherwise:
   - Reads the auto-generated admin password from `~/.graphdb-coord-data/.graphdb_admin_password` (mode 0600, written by `cmd/server` on first start).
   - POSTs `/auth/login` to get an admin JWT.
   - POSTs `/api/v1/apikeys` to mint a never-expiring API key with `read+write` permissions.
   - Writes the key to `~/.graphdb-coord-key` (mode 0600).

The API key is the long-lived credential. JWT default lifetime is 15 min (`pkg/auth/handlers.go:13` `DefaultTokenDuration`) — too short for agent sessions, so we mint an API key off the admin JWT and use it for ongoing operations.

## What `coord-seed.sh` does

1. Auto-detects `COORD_PROJECT` from `git remote get-url origin` if not set (override via env var).
2. Ensures a `:Project` node exists for the current project (idempotent create).
3. Reads existing `:Task` ids that match the `<COORD_PROJECT>:` prefix.
4. For each task in its hardcoded list (derived from `docs/NEXT_STEPS_2026-05-10.md`), prefixes the id with `<COORD_PROJECT>:` and creates a `:Task` node via `POST /nodes` if not already present.
5. For each newly-created `:Task`, creates an `:IN_PROJECT` edge to the project node.
6. If any new nodes were created, calls `POST /api/v1/schema/regenerate` to invalidate the GraphQL schema cache so `{ projects { ... } }` and `{ tasks { ... } }` queries see the new labels.

The task list is intentionally hardcoded — the seed script is a one-shot bootstrap, not a continuous sync. New tasks go into the planning doc + into the coord instance via `work-claim` (lazy-create on first claim) or a manual `POST /nodes` call.

Closed tasks (H1, H3, A4, A4-edges, A8.2) are seeded with `closing_prs` properties so audit-history queries work from day one.

## Verify it's working

```bash
# How many tasks, by status?
curl -sS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8090/graphql \
  -d '{"query":"{ tasks { id properties } }"}' | jq '[.data.tasks[] | (.properties | fromjson | .status)] | group_by(.) | map({status: .[0], count: length})'

# Who's holding what right now?
curl -sS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8090/graphql \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}' | jq '.data.edges'
```

The second query returns the typed-edge wiring — `Agent → Claim → Task` chains tell you who's working on what without a single recursive CTE.

## Schema reference

```
:Project  { id: string!, name: string, repo_url?: string, description?: string }
:Task     { id: string!, track: string, status: "open"|"in-progress"|"done", closing_prs?: string }
:Agent    { id: string!, host: string, started_at: timestamp, intent?: string }
:Claim    { id: uuid, started_at: timestamp, expected_duration: string, for_task: string }
:PR       { number: int!, title: string, merged_at: timestamp }   # not yet seeded; use closing_prs string until :CLOSED_BY edges land

(Task)  -[:IN_PROJECT]-> (Project)
(Agent) -[:HOLDS]-> (Claim) -[:FOR]-> (Task)
(Task)  -[:DEPENDS_ON]-> (Task)                     # cross-project deps work natively; seeding TBD
(Task)  -[:CLOSED_BY]-> (PR)                        # seeding TBD
```

`:Task.id` is **project-prefixed**: `graphdb:H4-PR1-blite`, `syntopica:auth-spike`. The prefix is canonical — operators can split on `:` for fast project disambiguation without traversing `:IN_PROJECT`. The edge exists for the dogfood case (cross-project queries are native graph walks, not property scans).

## Multi-project coord

A single coord instance can host multiple projects (e.g., `graphdb`, `syntopica`). Each project has its own `:Project` node; every `:Task` belongs to one project via an `:IN_PROJECT` edge. `:DEPENDS_ON` edges are unrestricted — a `:Task` in `syntopica` can depend on a `:Task` in `graphdb` and the dependency traversal "just works" because both live in the same single-tenant graph. See `docs/MULTI_PROJECT_SPIKE_2026-05-10.md` for the design rationale (and why per-tenant-per-project doesn't work — audit A6a's `*ForTenant` enforcement forbids cross-tenant edges).

### `COORD_PROJECT` env var

`coord-seed.sh` and `coord-migrate-add-projects.sh` both require `COORD_PROJECT`. Auto-detect:

```bash
$(git remote get-url origin | sed 's:.*/::; s:\.git$::')
# https://github.com/dd0wney/graphdb        → graphdb
# git@github.com:dd0wney/syntopica.git      → syntopica
```

Override when auto-detect is wrong (forks, monorepo subdirs, repo renames):

```bash
COORD_PROJECT=foo bash scripts/coord-seed.sh
```

### Migrating an existing single-project coord

If you bootstrapped coord before this schema change shipped, run the migration once:

```bash
bash scripts/coord-migrate-add-projects.sh
```

It (1) creates the `:Project` node for `COORD_PROJECT`, (2) prefixes every existing un-prefixed `:Task.id` in-place via `PUT /nodes/{id}`, (3) creates one `:IN_PROJECT` edge per `:Task → :Project`, (4) renames `:Claim.for_task` to match the prefixed id, and (5) regenerates the GraphQL schema cache. Idempotent: each step independently checks "already done?" and skips. Safe to re-run.

**Conflict guard**: if a `:Project` node already exists with a different id than `COORD_PROJECT`, the script refuses to proceed. Recovery: set `COORD_PROJECT=<existing-id>` explicitly to acknowledge, or wipe the data dir to start fresh. This catches "wrong directory" mistakes before they corrupt the graph.

### Adding a second project to an existing coord

The `coord-seed.sh` in *this* repo is graphdb-specific — its `TASKS=(...)` list is hardcoded from graphdb's planning doc. To add a second project (e.g., syntopica) to the same coord daemon, the second repo carries its own `coord-seed.sh` with its own task list, pointed at the same daemon via `GRAPHDB_COORD_URL`. Each repo's seed auto-detects its own `COORD_PROJECT` from `git remote get-url origin`, so the two seeds coexist on the daemon without manual config — each creates its own `:Project` node and tasks. Cross-project `:DEPENDS_ON` edges can then be created via `POST /edges` directly (the resolver doesn't care which project the endpoints belong to).

### Cross-project queries

These queries work today against a multi-project coord:

```bash
# All projects under coord
curl -sS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8090/graphql \
  -d '{"query":"{ projects { id properties } }"}'
```

```bash
# All tasks across all projects, with project prefix preserved on `id`
curl -sS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8090/graphql \
  -d '{"query":"{ tasks { id properties } }"}' \
  | jq '[.data.tasks[] | (.properties | fromjson) | {id, status, track}] | group_by(.id | split(":")[0]) | map({project: .[0].id | split(":")[0], count: length})'
```

```bash
# All :IN_PROJECT and :DEPENDS_ON edges — the cross-project structure
curl -sS -X POST -H "X-API-Key: $GRAPHDB_COORD_TOKEN" -H 'Content-Type: application/json' \
  http://localhost:8090/graphql \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}' \
  | jq '.data.edges[] | select(.type == "IN_PROJECT" or .type == "DEPENDS_ON")'
```

Native traversal queries (`{ project(id: "graphdb") { tasks { ... } } }` or `{ task(id: "syntopica:auth-spike") { dependencies { project { id } } } }`) need a richer GraphQL surface than `cmd/server` exposes today; they're tracked under planning-doc §H4.2 (merge the two schema generators or move `cmd/server` to the edges-aware one). Until then, the queries above + client-side traversal are the operator surface.

## Atomicity status (option A — advisory)

Currently `:Claim` uniqueness is **advisory**: skills (will) check "does any active Claim exist for this Task?" before creating their own. Two agents racing can both pass the check; collision manifests as duplicate `:Claim` nodes for the same `for_task`.

This is the documented temporary state. Resolution lands as **PR 1 of `docs/COORD_DEPLOY_SPIKE_2026-05-10.md`'s rollout** (resolver-side `:Claim` uniqueness in `pkg/graphql/edges_schema.go`, ~50-100 LOC). Until then, the operational model is "single-developer, ≤2 agents, race window is wall-clock seconds."

To check whether B-lite has landed (for any future reader of this doc):

```bash
grep -l "labels.*Claim" pkg/graphql/edges_schema.go && echo "B-lite likely landed" || echo "still on advisory uniqueness (option A)"
```

## Known limitations of the current coord wiring

These are real and documented as planning-doc §H4 follow-ups:

- **REST `/nodes` GET base64-encodes string properties.** `pkg/api/handlers_nodes.go:34` does `props[k] = v.Data` where `Value.Data` is `[]byte`. Go's `encoding/json` serializes `[]byte` as base64. Workaround: decode client-side (the seed script does this) or read via GraphQL (which serializes properties as a JSON string blob via the resolver, no base64 round-trip). The bug is real and fixable but out of scope for the coord-deploy MVP.
- **`cmd/server`'s GraphQL has no Mutation type.** `cmd/server` uses `pkg/graphql/limits.go`'s `GenerateSchemaWithLimitsForTenant` which is queries-only. The Mutation type defined in `pkg/graphql/edges_schema.go` is unreachable from the live server. So writes are REST-only; reads can be either. Resolution: either make `cmd/server` use the schema generator that has mutations, or merge the two generators. Out of scope for the coord-deploy MVP; tracked under H4 follow-ups.
- **GraphQL schema is per-tenant + cached + lazy.** Built from labels existing at first-request time. After bootstrapping new node types, `POST /api/v1/schema/regenerate` (admin-only) is required to make label-specific query fields appear. The seed script handles this.

## Daemon lifecycle

Reasonable to leave running. Memory footprint is small (a few MB); CPU at idle is near-zero. Restart on machine reboot via `coord-bootstrap.sh` (or wire it into your shell login if you want it always-on).

To stop:

```bash
kill $(lsof -t -i:8090)
```

To wipe state (rarely needed; the data dir survives restarts):

```bash
kill $(lsof -t -i:8090) 2>/dev/null
rm -rf ~/.graphdb-coord-data ~/.graphdb-coord-key
bash scripts/coord-bootstrap.sh    # fresh start; new admin password + API key
```

## Multi-machine deploy (deferred)

Single-developer is the supported configuration today. For a real multi-developer team, the coord instance moves to a small VM behind TLS, the JWT secret moves to a shared secret manager, and the `nng` replication binaries (`graphdb-nng-{primary,replica}`) provide HA. Out of scope for the current rollout; track via planning-doc §H4 follow-ups when an external agent operator needs it.

## See also

- `docs/COORD_DEPLOY_SPIKE_2026-05-10.md` — design analysis, atomicity options, rollout sequencing.
- `docs/COORD_GAP_2026-05-10.md` — original gap analysis that triggered the spike.
- `docs/NEXT_STEPS_2026-05-10.md` §H4 — planning-doc tracking for coord follow-ups.
- `scripts/coord-bootstrap.sh`, `scripts/coord-seed.sh` — the reference implementation.
