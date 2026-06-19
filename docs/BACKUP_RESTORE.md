# Backup & Restore — Hot Backup Endpoint

GraphDB ships a built-in hot-backup endpoint (`POST /admin/backup`) that captures a consistent archive of the running store without stopping the server.

---

## Endpoint

```
POST /admin/backup
Authorization: Bearer <admin-token>
```

Returns a `.tar.gz` stream directly in the response body. The response `Content-Type` is `application/gzip` and `Content-Disposition` sets a timestamped filename.

### Example

```bash
curl -fSL \
  -X POST \
  -H "Authorization: Bearer $TOKEN" \
  https://host/admin/backup \
  -o backup.tar.gz
```

Replace `$TOKEN` with your admin API key (`ADMIN_API_KEY` / `X-Admin-Token` header value as configured). The `-fSL` flags cause `curl` to fail loudly on HTTP errors and follow redirects.

---

## Archive contents

| Path in archive | What it contains |
|---|---|
| `snapshot.json` (or `snapshot.mmap`) | Point-in-time serialised graph (nodes, edges, tenant indexes) |
| `wal/` | Write-ahead log segments current at snapshot time |
| `auth/` | Tenant credential files (hashed passwords, API keys) |
| `lsa/` | LSA vector-index persistence files |
| `manifest.json` | Archive metadata: graphdb version, creation timestamp (UTC), snapshot mode (`json`/`mmap`), list of included files |

### Consistency guarantee

The archive is **snapshot-consistent**: the snapshot is taken first (atomic point-in-time flush), and then WAL segments current at that instant are included. On restore, WAL replay brings the store from the snapshot state up to the best available recovery point. This is a best-effort-current-WAL guarantee — any writes that arrived and were acknowledged after the snapshot was captured but before the WAL copy completed may be replayed; any writes that arrived after both steps are not included.

---

## Security warning

> **The backup archive contains sensitive data.**
>
> - It includes **password hashes and API keys** for all tenants (from `auth/`).
> - It includes the **graph data of every tenant** in the store.
> - The endpoint is **admin-only** — requests without a valid admin token receive `403 Forbidden`.
> - Always transfer and store backup archives over **TLS / HTTPS**. A backup transferred over plaintext HTTP exposes every tenant's credentials and data.
> - Treat backup archives with the same access controls as production database dumps. Store them in access-controlled, encrypted-at-rest object storage.

---

## Offline restore procedure

Restore is an offline operation — the server must be stopped before replacing its data directory.

1. **Stop the server.**

   ```bash
   # Docker
   docker-compose -f docker-compose.prod.yml stop graphdb-community

   # systemd
   systemctl stop graphdb-server
   ```

2. **Back up the current data directory** (optional but recommended before overwriting).

   ```bash
   cp -r /data /data.pre-restore-$(date +%Y%m%dT%H%M%S)
   ```

3. **Clear the data directory.**

   ```bash
   rm -rf /data/*
   ```

4. **Extract the archive into the data directory.**

   ```bash
   tar xzf backup.tar.gz -C /data
   ```

5. **Start the server.**

   ```bash
   # Docker
   docker-compose -f docker-compose.prod.yml start graphdb-community

   # systemd
   systemctl start graphdb-server
   ```

On startup, the server loads the snapshot and replays the WAL segments in `wal/` to reconstruct the graph in memory. Vector indexes (LSA) are rebuilt from the node embeddings in the loaded graph; they are not persisted independently but regenerated automatically on first access.

---

## Cold backup (alternative — server stopped)

If a hot backup is not required, the traditional cold-backup approach (stop the server, archive the volume, restart) remains valid and documented in [`docs/DEPLOYMENT_GUIDE.md`](./DEPLOYMENT_GUIDE.md).
