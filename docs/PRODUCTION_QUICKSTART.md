# Cluso GraphDB - Production Quickstart

## Single-Node Deployment

> **graphdb is single-node by design.** Per the A8.1 architectural decision (2026-05-12, see `docs/A8_1_SPIKE_2026-05-12.md`), the standalone replication binaries (`graphdb-primary`, `graphdb-replica`, plus their `nng` variants) and the `pkg/replication/` library were retired. The deployment surface is now `cmd/server` only.
>
> Horizontal scale (sharded write path, distributed query) is a multi-quarter scope explicitly out of the current roadmap. If you need multi-node HA today, the deployment patterns are: per-tenant deployment behind a router, or wait for a future `cmd/server`-native replication rebuild.
>
> A prior version of this guide documented multi-node deployment using the legacy `graphdb-primary` / `graphdb-replica` binaries. That guidance was unsafe — the binaries pre-dated the multi-tenant work and routed writes to the default tenant. They've been deleted from the codebase; the old guide is in git history.

---

## Prerequisites

- Linux server (Ubuntu 22.04+ or equivalent) with systemd
- 4 CPU cores, 8GB RAM, 100GB SSD (sized for your corpus — see Scale Considerations below)
- Go 1.23+ if building from source
- Network access on port 8080 (HTTP API) and whatever other ports you expose

## Step 1: Build the binary

```bash
# On your build machine
cd cluso-graphdb
go build -o graphdb ./cmd/server
./graphdb --version
```

For deploying a pre-built artifact, copy the binary to the target machine — no static dependencies beyond glibc.

## Step 2: Deploy the server

```bash
# Create data directory
sudo mkdir -p /var/lib/graphdb
sudo chown graphdb:graphdb /var/lib/graphdb

# Copy binary
sudo cp graphdb /usr/local/bin/
sudo chmod +x /usr/local/bin/graphdb

# Create systemd service
sudo tee /etc/systemd/system/graphdb.service > /dev/null <<EOF
[Unit]
Description=Cluso GraphDB
After=network.target

[Service]
Type=simple
User=graphdb
Group=graphdb
WorkingDirectory=/var/lib/graphdb
Environment="DATA_DIR=/var/lib/graphdb"
Environment="JWT_SECRET=<set-a-strong-secret>"
ExecStart=/usr/local/bin/graphdb --port 8080
Restart=on-failure
RestartSec=5s

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
EOF

# Create user
sudo useradd --system --home /var/lib/graphdb --shell /usr/sbin/nologin graphdb

# Start
sudo systemctl daemon-reload
sudo systemctl enable graphdb
sudo systemctl start graphdb

# Verify
sudo systemctl status graphdb
curl http://localhost:8080/health
```

**Required environment variables:**
- `JWT_SECRET` — any non-empty value. Required in every environment; the previous "auto-generate in non-prod" behavior was removed as a security finding. See `pkg/api/server_init.go:74-77`.
- `DATA_DIR` — where the server persists graph state, snapshots, auth state, and LSA indexes. Defaults to `./data/server` if unset.

**Optional environment variables** (see `pkg/api/server_init.go` for the full list):
- `ADMIN_PASSWORD` — sets initial admin password on first boot. If unset and `GRAPHDB_ENV=production`, no admin user is created (you must seed manually).
- `GRAPHDB_ENV` — `production` triggers stricter defaults (no auto-generated admin password).
- `AUDIT_PERSISTENT=true` + `AUDIT_DIR=/var/log/graphdb/audit` — enable on-disk audit logging.
- `GRAPHDB_LSA_BOOTSTRAP_LABELS` / `GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES` / `GRAPHDB_LSA_BOOTSTRAP_TENANTS` — build LSA indexes at boot. See `pkg/api/server_init.go`'s `bootstrapIndexesFromEnv` for the full env-var surface.

## Step 3: Verify the deployment

```bash
# Liveness
curl http://localhost:8080/health

# Readiness (deeper check — storage accessible, etc.)
curl http://localhost:8080/health/ready

# Prometheus metrics
curl http://localhost:8080/metrics

# Authenticate (replace ADMIN_PASSWORD with your value)
TOKEN=$(curl -s -X POST http://localhost:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"ADMIN_PASSWORD"}' | jq -r '.token')

# Smoke-test a write
curl -X POST http://localhost:8080/v1/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"labels":["Doc"],"properties":{"title":"first node"}}'
```

## Step 4: Monitoring

The server exposes Prometheus metrics at `/metrics`. Key gauges and counters:

- `graphdb_http_requests_total{method,path,status}` — request volume
- `graphdb_http_request_duration_seconds{path}` — latency histogram
- `graphdb_storage_node_count` / `graphdb_storage_edge_count` — corpus size
- `graphdb_wal_*` — WAL throughput, fsync latency, batched-write stats
- `graphdb_lsm_*` — LSM cache hit rate, level sizes

See `docs/MONITORING_SETUP.md` for a Prometheus + Grafana setup that scrapes these.

Health-check script suitable for `cron` or a remote watchdog:

```bash
#!/bin/bash
set -eu
HOST=${1:-http://localhost:8080}
if ! curl -sf "$HOST/health/ready" > /dev/null; then
  echo "graphdb readiness check failed on $HOST"
  exit 1
fi
```

## Step 5: Backups

The graph storage layer persists via WAL + periodic snapshots (`snapshot.json`). LSA indexes persist alongside (`lsa/<tenantID>.lsa`). Both live under `DATA_DIR`.

Daily backup script:

```bash
#!/bin/bash
set -eu
BACKUP_DIR=/var/backups/graphdb
DATA_DIR=${DATA_DIR:-/var/lib/graphdb}
DATE=$(date +%Y%m%d-%H%M%S)

# Trigger snapshot (admin-only endpoint; flushes in-memory state to disk)
curl -sf -X POST http://localhost:8080/v1/admin/snapshot \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# Brief wait — snapshot is atomic-rename, completes in seconds for typical corpora
sleep 5

# Archive the data dir
mkdir -p "$BACKUP_DIR"
tar -czf "$BACKUP_DIR/graphdb-$DATE.tar.gz" -C "$(dirname "$DATA_DIR")" "$(basename "$DATA_DIR")"

# Retain 7 days
find "$BACKUP_DIR" -name 'graphdb-*.tar.gz' -mtime +7 -delete
```

Restore: stop the service, replace `DATA_DIR` contents with the backup, restart.

## Step 6: Update Procedure

Single-node deployment means updates require a brief restart. The procedure:

```bash
# 1. Pre-update: take a fresh backup (see Step 5).
bash /usr/local/bin/graphdb-backup.sh

# 2. Stop the service.
sudo systemctl stop graphdb

# 3. Replace the binary.
sudo cp graphdb-v1.1 /usr/local/bin/graphdb
sudo chmod +x /usr/local/bin/graphdb

# 4. Start.
sudo systemctl start graphdb

# 5. Verify.
curl -sf http://localhost:8080/health/ready
curl -sf http://localhost:8080/v1/version  # confirm new version
```

**Typical downtime:** 5-30 seconds depending on snapshot size (the server replays WAL from the most recent snapshot on boot). For corpora under 1M nodes, expect under 15 seconds.

**If the new binary fails to start** (corrupted state, schema-breaking change, etc.): restore from backup, swap the binary back, restart. Keep the previous version's binary on disk during the update window so rollback is one `cp` away.

## Scale Considerations

Single-node graphdb scales to what one machine can hold — write throughput is bounded by a single Go process, read throughput by CPU and the LSM cache. Documented working ranges:

- **Nodes/edges:** comfortable to ~10M nodes + 50M edges on 32GB RAM. Beyond that, expect cache pressure and snapshot times to grow.
- **LSA scale ceiling:** ~100K-500K documents per tenant at 200 dims. See `docs/F1_1_PER_TENANT_LSA_DESIGN.md` §4 for the memory model. For larger corpora, the OpenAI-compatible `/v1/embeddings` endpoint supports BYO embeddings (point at `text-embedding-3-large` etc.) instead of using the built-in LSA.
- **Concurrent connections:** bounded by `LimitNOFILE` (set above to 65536); each HTTP request is goroutine-cheap.

**If you need to scale beyond one node:**
- **Per-tenant deployment** — run one graphdb per tenant behind a router. Simple, no replication. Works today.
- **Read-only replication** — not currently supported. A future rebuild on top of `cmd/server` is roadmap-tracked (see A8.1 spike doc).

## Troubleshooting

### Service fails to start

```bash
# Check logs
sudo journalctl -u graphdb -n 100 --no-pager

# Common causes:
# - JWT_SECRET unset (refuses to start)
# - DATA_DIR not writable by the graphdb user
# - Port 8080 in use by another process
# - Corrupted snapshot.json (try moving it aside and replaying from WAL)
```

### LSA queries return 503

The tenant's LSA index hasn't been built yet, or persistence restore failed. Check logs for `LSA snapshot restore:` lines. To rebuild:

```bash
# Via admin endpoint
curl -X POST http://localhost:8080/v1/admin/lsa/rebuild \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","labels":["Doc"],"body_properties":["body"]}'
```

### Slow restart after crash

WAL replay against a large corpus can be slow if there's no recent snapshot. Either:
- Schedule periodic snapshots more aggressively (admin endpoint `/v1/admin/snapshot`)
- Increase `BatchSize` / `FlushInterval` on the WAL config to reduce WAL entry count

### High memory usage

The graph storage is in-memory with periodic snapshots; memory scales with corpus size. If the process is OOM-ing:
- Reduce `MaxVocab` / `Dims` on LSA config to shrink LSA footprint
- Enable edge compression (default on; see `pkg/storage`)
- For corpora that don't fit, see Scale Considerations above

## Next Steps

- `docs/INTEGRATION_GUIDE.md` — connecting external systems
- `docs/MONITORING_SETUP.md` — Prometheus + Grafana scrape config
- `docs/API.md` — full REST + GraphQL surface
- `docs/AUTOMATED_UPGRADES.md` — admin-orchestrated update paths (single-node restart is the supported path; multi-node coordination from this doc is historical and references retired binaries)

## Questions?

File an issue on GitHub: https://github.com/dd0wney/graphdb/issues
