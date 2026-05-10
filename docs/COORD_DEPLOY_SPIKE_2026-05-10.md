# Spike: deploy graphdb to coordinate parallel agents — 2026-05-10

**Status**: Spike. Concludes with a go/no-go recommendation in §10.
**Supersedes**: parts of `docs/COORD_SETUP.md` (the original was written aspirationally — referenced API surface that doesn't exist; verified by `docs/COORD_GAP_2026-05-10.md`).
**Companion**: `docs/COORD_GAP_2026-05-10.md` (the gap analysis that triggered this spike).
**Tracked under**: planning doc §H4 (`docs/NEXT_STEPS_2026-05-10.md`).

---

## 1. Problem statement

Multiple Claude Code agents working on the same repo collide on tasks. The 2026-05-10 02:36Z session shipped 4 PRs of skill scaffolding (`work-claim`, `worktree-spawn`, `merge-coordinator`, `integration-checkpoint`) backed by graphdb-as-coord-store — but the deploy never happened because `COORD_SETUP.md` referenced API surface that doesn't exist. Then on 2026-05-10 ~03:08Z, two sessions independently picked up A8.2 (PR #81) — exactly the collision class the skills were meant to prevent. The dogfood story is real but the deploy is not.

This spike defines the *minimum* path to "graphdb is actually coordinating agent work." Concrete commands, no aspirational pseudocode.

## 2. What the original COORD_SETUP.md got wrong

The original assumed:

| Surface | Reality |
|---|---|
| `./graphdb --port 8090 --jwt-secret X --tenant default` | `cmd/graphdb` is a demo program, not a server. `cmd/server` is the real binary; flags are `--port` + `--data` only; JWT secret comes from `JWT_SECRET` env var, not a flag. |
| `license-server issue --jwt-secret … --subject …` | `cmd/license-server` is a Stripe-backed license issuance service, not a JWT-issuance CLI. No `issue` subcommand exists. |
| `POST /v1/constraints/uniqueness`, `POST /v1/property-indexes`, `POST /v1/batch`, `GET /v1/nodes/by-property` | None of these endpoints exist. `pkg/storage.PropertyIndex` and `pkg/constraints.UniquenessViolation` exist as primitives but aren't wired to HTTP. |
| `/v1/nodes` | Actual route is `/nodes` (no `/v1/` prefix). `/v1/embeddings` and `/v1/retrieve` are the only `/v1/`-prefixed routes. |

The deploy commands were written from architectural intuition before checking what `cmd/server` actually exposes. This spike corrects that.

## 3. Reality: minimum-viable local deploy

Tested on `cmd/server` build of `a9ec5df` (the doc commit; the binary is independent of the planning doc but version-stamping helps later debugging).

### 3.1. Run the server

```bash
# From the graphdb repo root
go build -o ./bin/graphdb-coord ./cmd/server/

# Minimum env vars for local single-developer use
export JWT_SECRET="dev-only-graphdb-coord-not-for-production"
mkdir -p ~/.graphdb-coord

./bin/graphdb-coord --port 8090 --data ~/.graphdb-coord
```

What happens on first start:

- License: fails-open to community tier (no `GRAPHDB_LICENSE_KEY` needed). Logged at startup as `license_tier=community license_valid=true`.
- Encryption: disabled by default (no `ENCRYPTION_ENABLED=1`). For coord — small, low-sensitivity workload — that's fine.
- Multi-tenancy: enabled by default (default tenant is `default`).
- Admin user: auto-created. Random 16-byte password written to `.graphdb_admin_password` (mode 0600) in the working directory. **The cwd matters** — that's where the password file lands.

> **Operational pin**: always start the coord daemon from the same directory (e.g., always `cd ~/.graphdb-coord-runtime && ./graphdb-coord ...` or pin it inside `coord-bootstrap.sh`). If the daemon is started from a different cwd each time, the auto-generated password rotates silently and stale `.graphdb_admin_password` files accumulate elsewhere on disk. Once you have an API key (per §6 decision 2), this matters less because the API key persists in `~/.graphdb-coord-key` independent of the daemon's cwd — but for the initial admin login, cwd-discipline is the only thing standing between you and a confused-deputy moment.

### 3.2. Get an admin JWT

```bash
# Read the auto-generated admin password
ADMIN_PW=$(cat .graphdb_admin_password)

# Login → access + refresh tokens
LOGIN_RESPONSE=$(curl -sS -X POST http://localhost:8090/auth/login \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"username":"admin","password":"%s"}' "$ADMIN_PW")")

# Extract access token (jq is the obvious tool but grep+cut works too)
COORD_TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r .access_token)
export GRAPHDB_COORD_URL="http://localhost:8090"
export GRAPHDB_COORD_TOKEN="$COORD_TOKEN"
```

JWT expiration: **15 minutes** (`pkg/auth/handlers.go:13` `DefaultTokenDuration`). Too short for most agent sessions — see §6 decision 2; recommended path is to mint a long-lived API key off this admin JWT and use the API key for ongoing coord operations.

### 3.3. Verify

```bash
curl -sS -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "$GRAPHDB_COORD_URL/auth/me" | jq .
# Expected: {"id":"...","username":"admin","role":"admin"}
```

If that returns a user object, auth works end-to-end.

## 4. Schema bootstrap via GraphQL

The `/graphql` endpoint at `pkg/graphql/edges_schema.go:117` exposes:

```graphql
type Mutation {
  createNode(labels: [String!]!, properties: String!): MutationNode
  updateNode(id: ID!, properties: String!): MutationNode
  deleteNode(id: ID!): DeleteResult
  createEdge(fromNodeId: ID!, toNodeId: ID!, type: String!,
             properties: String, weight: Float): Edge
  updateEdge(id: ID!, properties: String, weight: Float): Edge
}
```

`properties` is a JSON string, not a typed object. That's a quirk of the resolver — properties are passed as a serialized JSON blob and parsed server-side.

### 4.1. Coord schema as GraphQL mutations

```graphql
# Task node — the unit of work
mutation {
  createNode(
    labels: ["Task"]
    properties: "{\"id\":\"F1.1-spike\",\"track\":\"F\",\"status\":\"open\",\"created_at\":\"2026-05-10T03:30Z\"}"
  ) {
    id
  }
}

# Agent node — represents an active session
mutation {
  createNode(
    labels: ["Agent"]
    properties: "{\"id\":\"agent-darragh-laptop-2026-05-10T03:30Z\",\"host\":\"darragh-mbp\",\"started_at\":\"2026-05-10T03:30Z\"}"
  ) {
    id
  }
}

# Claim node — atomically claimed work
mutation {
  createNode(
    labels: ["Claim"]
    properties: "{\"id\":\"claim-f1.1-spike-2026-05-10\",\"started_at\":\"2026-05-10T03:30Z\",\"expected_duration\":\"4h\"}"
  ) {
    id
  }
}

# :HOLDS edge: Agent -> Claim (after both nodes exist; need their numeric IDs)
mutation {
  createEdge(
    fromNodeId: "<agent-numeric-id>"
    toNodeId: "<claim-numeric-id>"
    type: "HOLDS"
    properties: "{}"
  ) {
    id
  }
}

# :FOR edge: Claim -> Task
mutation {
  createEdge(
    fromNodeId: "<claim-numeric-id>"
    toNodeId: "<task-numeric-id>"
    type: "FOR"
    properties: "{}"
  ) {
    id
  }
}
```

Convention: the Task `id` field is the planning-doc identifier (`F1.1-spike`, `H4`, etc.). The Agent `id` is `agent-<short-host>-<iso8601>`. Claim `id` includes the task it's for, for grep-ability.

### 4.2. Reading the queue — and the schema-cache gotcha

The schema is **generated per tenant from labels that exist at first-request time** (`pkg/graphql/edges_schema.go:71`, `pkg/api/server_handlers.go:215`). After build, it's cached in `s.graphqlHandlers` (sync.Map) deduplicated by singleflight. So:

1. Server starts. No nodes exist for the default tenant. No `:Task`/`:Agent`/`:Claim` labels.
2. First GraphQL request arrives. Schema is built **with empty label-list**. The schema has `health`, `edge`, `edges` queries plus `createNode`/`createEdge` mutations — but **no `task`/`tasks`, `agent`/`agents`, or `claim`/`claims` query fields**.
3. Schema is now cached. Subsequent requests reuse it.

**Bootstrap implication**: `createNode`/`createEdge` are label-agnostic mutations and work without the labels existing in the schema. You can write all the Task/Agent/Claim/PR nodes via mutation. But before the first read query like `{ tasks { id } }`, the cache must be invalidated.

The endpoint to invalidate is `POST /api/v1/schema/regenerate` (admin-only — see `pkg/api/server.go:116`). Call it once after seeding to make label-specific query fields appear:

```bash
curl -sS -X POST -H "Authorization: Bearer $GRAPHDB_COORD_TOKEN" \
  "$GRAPHDB_COORD_URL/api/v1/schema/regenerate"
# Response: 200 OK; the next /graphql request rebuilds the schema with current labels.
```

After regeneration, query fields exist for each label that has at least one node. The naming is `strings.ToLower(label)` for the singular query and `strings.ToLower(label) + "s"` for the plural. So:

```graphql
query {
  tasks { id }      # NOT "Tasks" — lowercase
  agents { id }
  claims { id }
}
```

(Earlier draft of this spike got the casing wrong — verified against `pkg/graphql/edges_schema.go:75,87`.)

**Alternative read path**: `GET /nodes` returns all nodes for the caller's tenant via REST (`pkg/api/handlers_nodes.go:19`). No label filter currently exists, but for a coord workload of ≤100 tasks total, client-side filtering by `node.labels[0] == "Task"` is fine. This avoids the schema-cache invalidation step entirely. **`coord-bootstrap.sh` should use REST for reads** until the regenerate dance is verified end-to-end.

To find all open tasks not yet claimed via GraphQL, the cleanest query needs an inverse-edge traversal — verify with introspection (`{ __schema { queryType { fields { name } } } }`) post-regenerate before committing skill bash blocks to a specific query shape. The skill rewrite (PR 3 in §10's rollout) is the right place to nail this down, not the spike.

## 5. Atomicity — the unresolved gap

The coord schema needs **at most one active `:Claim` per `:Task`**. With current GraphQL surface, this is multi-mutation:

1. Create `:Claim` node
2. Create `:HOLDS` edge (Agent → Claim)
3. Create `:FOR` edge (Claim → Task)

Two agents claiming F1.1-spike simultaneously can both reach step (1) before either reaches (3). Both think they own the task.

### 5.1. Three options

#### A. Advisory uniqueness (look-before-leap, race window accepted)

Skill checks "does any active Claim exist for this Task?" before creating its own. Race window: two agents check, both see "no active Claim," both create one. Race manifests as both writing PRs that close the same task — caught at PR-creation time, not at claim time. Cost: occasional human-mediated cleanup; no engineering. **Honest cost for small parallelism (≤3 agents)**.

#### B-lite. Resolver-side uniqueness for `:Claim` only

In `pkg/graphql/edges_schema.go`'s `createNodeMutationResolver`, special-case `labels = ["Claim"]`: before creating the Claim node, check whether another active Claim exists for the same Task ID (read from the new Claim's `for_task` property; require this property on Claim creation). Reject with a structured error if so. Engineering: ~50-100 LOC in one file. Atomicity holds because the resolver runs under a single storage lock acquisition. Limitation: only `:Claim` is unique-checked; if other coord types need uniqueness later, this approach doesn't scale.

#### B-full. General uniqueness-constraint API surface

Implement `POST /v1/constraints/uniqueness` (or equivalent GraphQL surface) that lets clients declare uniqueness rules at schema-bootstrap time. Storage layer enforces. Engineering: 3-5 PRs (`pkg/constraints` already has the primitive — `pkg/constraints/uniqueness.go` — but isn't wired to HTTP). Long-term: this is the right shape; aligns with the original COORD_SETUP.md vision and is generically useful.

### 5.2. Recommendation — and the tension behind it

There are two coherent recommendations here and they pull in opposite directions:

**Engineering-pragmatic**: start with A. Zero engineering today, single developer, ≤2 agents typical, race window is wall-clock seconds. Promote to B-lite the first time a double-claim actually happens.

**Strategic / positioning** (per memory `project_graphdb_dogfoods_coord.md` and planning-doc §H4): start with B-lite. The dogfood story needs *real* atomic claim semantics to land — "graphdb provides atomic uniqueness via a typed resolver" is a stronger demo than "graphdb stores the same look-before-leap check you'd write against Redis." Half a day of engineering buys a unique-claim invariant that actually holds.

**This spike recommends B-lite** for three reasons:

1. The strategic framing was the user's stated lens for H4 (per memory). Picking A walks back from that without surfacing the trade-off.
2. The engineering cost is genuinely small (~50-100 LOC, one file, one PR). It's not a multi-day budget hit.
3. The fallback if B-lite turns out harder than expected is *known* — drop to A, document the race window, ship anyway. So B-lite is a low-risk attempt with a known landing pad.

**Trigger to promote B-lite → B-full**: a third coord-type needs uniqueness (right now only `:Claim` does), OR commercial positioning needs the full general-purpose constraint API for an external customer demo.

**Trigger to fall back to A**: B-lite implementation surfaces something non-obvious in the resolver path that turns the half-day into a multi-day. Document the surprise, ship A, file a follow-up to revisit B-lite with the new constraint understood.

If the user prefers A over B-lite (e.g., wants the deploy this week, treats positioning as a future-quarter concern), say so explicitly and §10 below should be re-read as "GO with A." The spike is structured so either decision is shippable from this point.

## 6. Open decisions for the user

These need explicit answers before the deploy lands:

1. **Where does the daemon run?** Localhost (only useful while the laptop is awake) or a small VM / Cloudflare Tunnel-fronted box (keeps coord state available 24/7 across sessions). For a single developer, localhost is fine — the cost of restart is "lose claims since last snapshot." Multi-developer needs the shared instance.

2. **JWT vs. API key for the coord-agent identity.** JWT default lifetime is **15 minutes** (`pkg/auth/handlers.go:13` `DefaultTokenDuration = 15 * time.Minute`). Most agent sessions run longer than 15 min, so JWT *will* expire mid-session. Two options:
   - (a) Wire refresh-token flow into the `work-claim` skill — every coord call checks the token's `exp`, refreshes if within ~1 min of expiry. ~30 LOC of bash, plus a refresh-token store somewhere (`.graphdb-coord-refresh` in `~/`?).
   - (b) Issue a long-lived **API key** for the coord-agent identity via `POST /api/v1/apikeys` (admin-authenticated, returns a key with configurable expiry — e.g., 90 days). Skills use `X-API-Key: <key>` header instead of `Authorization: Bearer`. Same auth surface — `pkg/api/middleware_auth.go:55` accepts both.
   
   **Strongly recommend (b)**. The API key is designed for service-to-service use, expires on a human timescale (days/weeks), and avoids the refresh-flow plumbing. The cost is one initial admin-JWT call to mint the key, then store it once. **Default if no answer**: (b).

3. **Snapshot cadence for coord data.** The data is small (KB-per-task) but losing it loses claim state. Default snapshot policy works; revisit if coord becomes load-bearing.

4. **Does the coord instance back up to anywhere?** Local snapshot is fine until the laptop dies. R2 / S3 / etc. is overkill until coord is shared across team. **Default**: local only.

5. **Does atomicity option A actually need any skill-side enforcement, or do we trust honor-system?** I.e., does `work-claim` skill perform the look-before-leap check, or does the agent simply assume the task is claimable and the user resolves collisions if they happen? Recommend: skill-side check (cheap to write, catches the obvious case).

## 7. Seeding plan

Bootstrap `:Task` nodes for the current planning-doc state. Done as a one-time script; future tasks added via `work-claim` lazy-create.

| Task ID | Status | Closing PR (`:CLOSED_BY`) |
|---|---|---|
| H1 | done | #65 / #66 |
| H3 | done | (no PR — operational; left as `closed` without `:CLOSED_BY`) |
| A4 | done | #67 |
| A4-edges | done | #70 |
| A8.2 | done | #81 |
| F1.1-spike | open | — |
| F1.1-impl | open | — (depends on F1.1-spike) |
| F3 | open | — |
| A8.1 | open | — (off critical path) |
| H2 | open | — (off critical path) |
| H4 | open → in-progress | (this spike + the rollout in §10 close it; PR 4 marks the closure) |
| S1 | open | — |

Plus `:DEPENDS_ON` edges per the planning-doc sequencing graph. The closed-task seeding is retroactive — useful because (a) `merge-coordinator` traverses `:DEPENDS_ON` even for closed tasks to construct the dependency graph, (b) the audit-history query ("what closed in May 2026?") is immediately useful from day one.

The seeding script lives at `scripts/coord-seed.sh` (proposed); reads tasks from this table and POSTs them in a single Bash loop. Idempotent via "create-if-not-exists" check (reads the existing tasks first, skips ones that match by `id` property).

## 8. Rollout sequence

When this spike is approved, implementation lands as follows:

1. **PR 1: coord setup script + corrected docs.** (~150 LOC of bash + ~100 lines of docs)
   - `scripts/coord-bootstrap.sh` — runs the server, creates admin, prints token, idempotent.
   - `scripts/coord-seed.sh` — seeds `:Task` nodes from the current planning doc.
   - Replace `docs/COORD_SETUP.md` with what actually works (cite this spike for the analysis).
   - Update `docs/COORD_GAP_2026-05-10.md` header noting it's been superseded.
2. **PR 2: skill rewrite.** (~50-100 LOC across `.claude/skills/work-claim/`, `.claude/skills/worktree-spawn/`, `.claude/skills/merge-coordinator/`)
   - Replace the `/v1/constraints/uniqueness` and `/v1/property-indexes` calls with no-ops + comments pointing at this spike.
   - Replace `/v1/nodes` POST with GraphQL `createNode` mutation.
   - Add advisory uniqueness check (option A) inline in `work-claim`.
3. **PR 3 (planning-doc-update): mark H4 done.** Update `docs/NEXT_STEPS_2026-05-10.md` §H4 with the recommendation outcome (option A) and reference this spike.
4. **(Operational, no PR): seed the coord instance.** Run the bootstrap + seed script. From this point, claims happen via the skills against a real coord instance.

PRs 1 + 2 can land in either order; PR 3 follows. PRs 1 + 2 split is the standard atomic-commit shape — the script + docs are operational, the skill rewrite is behavioral.

Subsequent (out of scope for this spike but worth flagging):

- **B-lite promotion** (if double-claims happen): single PR adding the `:Claim` uniqueness check to the GraphQL resolver.
- **B-full promotion** (if positioning timeline demands): multi-PR sub-track, scoped via its own spike.

## 9. Verification at each step

After each rollout step, the test that "this works" is concrete:

- After PR 1: `bash scripts/coord-bootstrap.sh && bash scripts/coord-seed.sh` and then `curl /graphql` returns a populated `Tasks { id }` query.
- After PR 2: invoke the `work-claim` skill on F1.1-spike from a fresh session — it creates the Agent + Claim + edges and reports success.
- After step 4 (seed): `curl /graphql` shows the planning-doc backlog as `:Task` nodes with the right edge structure.
- Ongoing: a query of the form *"who's holding stale claims older than 4 hours?"* returns useful coord visibility (run from a small monitoring script or the user's laptop on demand).

## 10. Recommendation

**GO with atomicity option B-lite** (resolver-side `:Claim` uniqueness, ~50-100 LOC in `pkg/graphql/edges_schema.go`). Ship the rollout in this order:

- **PR 1 (B-lite implementation)**: special-case `:Claim` creation in `createNodeMutationResolver`. Reject if another active Claim exists for the same Task ID. Adds a `for_task` required-property convention on Claim creation.
- **PR 2 (coord setup script + corrected docs)**: the operational layer per §8.
- **PR 3 (skill rewrite)**: per §8.
- **PR 4 (planning-doc update)**: mark H4 done, point at this spike + the implementation PRs.

PR 1 is the half-day. PRs 2-4 are docs/scripts/skill bash and ride on top of PR 1.

**Why this beats option A as the starting point**: per §5.2, the strategic framing pulls toward B-lite. The dogfood demo lands *only* if uniqueness actually holds at the storage layer. A leaves it as honor-system, which undercuts the positioning sentence "graphdb coordinates its own development." The engineering cost is small enough that it doesn't deserve to be deferred for the operational milestone alone.

**Fallback if B-lite implementation surprises us**: drop to A, document the race window, file a follow-up. The rollout is structured so PR 2 + PR 3 don't depend on B-lite specifically — they assume *some* atomicity story. Switch the dependency at PR-2 time if PR 1 stalls.

**Defer**: B-full uniqueness API. Worth doing for the customer-facing positioning story (general-purpose `/v1/constraints/uniqueness` aligns with the original COORD_SETUP.md vision and is genuinely useful for callers other than coord), but not the *blocker* for closing H4. Track as a sub-task of H4 in the next planning checkpoint with its own spike when promoted.

**Out of scope for this spike**: multi-machine deploy (single-developer is sufficient now), backup to remote storage (local snapshot is enough), refresh-token wiring (the API-key path in §6 decision 2 avoids the question entirely).

**If the user's read of §5.2 prefers A over B-lite**: re-read this section as "GO with A," skip PR 1, and rollout becomes PR 2 → PR 3 → PR 4. The change is substantively a one-line PR-1 deletion. Both paths are shippable from here.

## 11. Open questions (consolidated)

The "Open decisions for the user" in §6 plus the atomicity choice from §5.2 / §10 are reproduced here for easy reference at session boundaries:

0. **Atomicity strategy: B-lite or A?** Spike recommends **B-lite** (resolver-side `:Claim` uniqueness, ~50-100 LOC, ships PR 1 of the rollout). Strategic framing per memory `project_graphdb_dogfoods_coord.md` argues for B-lite. Engineering-pragmatic argument for A is in §5.2; A drops PR 1 entirely. **This is the only decision that changes PR scope; the others change runtime config or skill bash.**
1. Daemon runs on localhost or shared instance? **Default: localhost.**
2. JWT vs. API key for the coord-agent identity? **Strong recommend: API key** (JWT lifetime is 15 min, too short for agent sessions).
3. Snapshot cadence? **Default: out-of-the-box policy.**
4. Backup target? **Default: local-only.**
5. Skill-side advisory uniqueness check (only relevant if decision 0 = A)? **Recommend: yes, ~5 LOC of bash in `work-claim`.**

If you accept the spike's recommendation on all six, no follow-up decisions are needed before PR 1.
