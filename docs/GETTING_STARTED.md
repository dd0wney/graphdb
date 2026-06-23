# Getting Started with GraphDB

This guide takes you from nothing to a running single-node GraphDB server with
your first nodes, edges, and query in about five minutes. For production
deployment (TLS, encryption, cloud, monitoring), see
[`DEPLOYMENT_GUIDE.md`](./DEPLOYMENT_GUIDE.md).

> **The production server is `graphdb-server`** (`cmd/server`). `graphdb-cli` is
> an interactive REPL and `cmd/graphdb` is a demo — neither is the server.

---

## 1. Run the server

`JWT_SECRET` is **required** — the server fail-closes without it (so auth tokens
can never be signed with a guessable key). `ADMIN_PASSWORD` bootstraps the
initial `admin` user.

### Option A — Docker (recommended)

```bash
docker run -p 8080:8080 \
  -e JWT_SECRET="dev-only-secret-change-me" \
  -e ADMIN_PASSWORD="choose-a-strong-password" \
  -v graphdb_data:/data \
  dd0wney/graphdb:latest
```

### Option B — Pre-built binary

Download for Linux or macOS (amd64/arm64) from the
[releases page](https://github.com/dd0wney/graphdb/releases/latest), optionally
[verify the signature](./RELEASE_SIGNING.md), then:

```bash
export JWT_SECRET="dev-only-secret-change-me"
export ADMIN_PASSWORD="choose-a-strong-password"
./graphdb-server --port 8080 --data ./data/server
```

Defaults: port **8080** (`--port` / `PORT`), data dir **`./data/server`**
(`--data` / `DATA_DIR`). If you omit `ADMIN_PASSWORD` in a dev run, a random one
is generated and written to `<data-dir>/.graphdb_admin_password` (mode 0600).

### Confirm it's up

```bash
curl -s http://localhost:8080/health        # -> {"status":"ok", ...}
curl -s http://localhost:8080/health/ready   # readiness probe (K8s-friendly)
```

---

## 2. Get an auth token

All data endpoints require a bearer token. Log in as the bootstrap admin:

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"choose-a-strong-password"}' \
  | jq -r .access_token)
```

The response is `{ "access_token", "refresh_token", "expires_in" }` (access
tokens are short-lived; refresh with `POST /auth/refresh`).

**Scripting/CI alternative** — mint a token offline (no running server needed,
but `JWT_SECRET` must match the server's):

```bash
JWT_SECRET="dev-only-secret-change-me" \
  graphdb-admin mint-token --username admin --role admin --ttl 24h
```

---

## 3. Create nodes and an edge

```bash
# Create two Person nodes
curl -s -X POST http://localhost:8080/nodes \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"labels":["Person"],"properties":{"name":"Alice","age":30}}'
# -> {"id":1,"labels":["Person"],"properties":{"name":"Alice","age":30}}

curl -s -X POST http://localhost:8080/nodes \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"labels":["Person"],"properties":{"name":"Bob","age":28}}'
# -> {"id":2, ...}

# Connect them with a KNOWS edge (weight is required; use 1.0 if unweighted)
curl -s -X POST http://localhost:8080/edges \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"from_node_id":1,"to_node_id":2,"type":"KNOWS","weight":1.0,
       "properties":{"since":"2025-01-01"}}'
# -> {"id":1,"from_node_id":1,"to_node_id":2,"type":"KNOWS","weight":1, ...}
```

---

## 4. Query the graph

### Cypher-style query (`POST /query`)

```bash
curl -s -X POST http://localhost:8080/query \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"query":"MATCH (p:Person) RETURN p"}'
# -> {"columns":["p"],"rows":[{"p":{"id":1,"labels":["Person"],...}}],"count":2,"time":"1.2ms"}
```

### Traversal (`POST /traverse`)

```bash
curl -s -X POST http://localhost:8080/traverse \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"start_node_id":1,"max_depth":2,"direction":"outgoing"}'
```

GraphDB also speaks **GraphQL** at `POST /graphql`, and has endpoints for
shortest path (`/shortest-path`), algorithms like PageRank (`/algorithms`),
vector search (`/vector-search`), and full-text/hybrid search (`/search`,
`/hybrid-search`). The full API spec is served at
`GET /api/docs/openapi.yaml`.

---

## 5. Back up your data

```bash
curl -fSL -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/admin/backup -o backup.tar.gz
graphdb-admin backup verify backup.tar.gz
```

See [`BACKUP_RESTORE.md`](./BACKUP_RESTORE.md) for the full backup/restore runbook.

---

## Where to next

| Topic | Doc |
|---|---|
| Production deployment (TLS, encryption, cloud, monitoring) | [`DEPLOYMENT_GUIDE.md`](./DEPLOYMENT_GUIDE.md) |
| Build indexes before serving traffic | [`PRODUCTION_QUICKSTART.md`](./PRODUCTION_QUICKSTART.md) |
| Security setup (encryption at rest, audit logging) | [`SECURITY-QUICKSTART.md`](./SECURITY-QUICKSTART.md) |
| Admin CLI (`graphdb-admin`) | [`CLI-ADMIN.md`](./CLI-ADMIN.md) |
| Backup & restore | [`BACKUP_RESTORE.md`](./BACKUP_RESTORE.md) |
| Verifying signed releases | [`RELEASE_SIGNING.md`](./RELEASE_SIGNING.md) |
| API & on-disk format stability guarantees | [`STABILITY_POLICY.md`](./STABILITY_POLICY.md) |

## Common gotchas

- **Server won't start** → `JWT_SECRET` is unset. It's required, even in dev.
- **`401 Unauthorized`** → missing/expired token; re-run step 2 (access tokens are short-lived).
- **`403 Forbidden`** → the endpoint is admin-only and your token isn't an admin role.
- GraphDB is **single-node** by design; clustering code in `pkg/cluster` is experimental and not wired in.
