# Auto-embed quickstart

This doc shows how to exercise the **auto-embed-on-create** path
([R2.5b](./internals/design/S11_AUTO_EMBEDDER_REDESIGN.md)) in a real
deployment. When configured, every `CreateNode*` for a matching label
triggers an asynchronous embedding write — the node's source text is
projected into the per-tenant LSA latent space and the resulting vector
lands on a target property.

This is the deployment-side counterpart to the unit + integration tests
that ship the feature. The Go tests pin correctness; this doc pins the
operator path.

## Prerequisites

1. **An LSA index built for the target tenant.** Auto-embed routes
   through `LSAEmbedder` which reads the per-tenant `LSAIndex` from
   `s.lsaIndexes`. Without an index, `LSAEmbedder` returns
   `ErrNoIndexForTenant` and the observer drops the task (silently per
   contract). Build via:
   - **Startup bootstrap** (`GRAPHDB_LSA_BOOTSTRAP_LABELS` +
     `GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES`) — see `pkg/api/server_init.go`'s
     `bootstrapIndexesFromEnv` comment header.
   - **Admin API**: `POST /hybrid-search/lsa-index` (requires admin auth).

2. **A label-source-target shape.** Pick one node label, one string
   property to embed, and one vector property to write to. Example:
   `Doc` label with `body` source → `embedding` target.

## Minimum viable config (docker-compose)

Edit the `graphdb` service in `docker-compose.yml`:

```yaml
graphdb:
  build:
    context: .
    dockerfile: Dockerfile
  container_name: cluso-graphdb
  environment:
    PORT: "8080"
    JWT_SECRET: "dev-only-secret-not-for-production"

    # Build an LSA index for the default tenant at startup over Doc.body.
    GRAPHDB_LSA_BOOTSTRAP_LABELS: "Doc"
    GRAPHDB_LSA_BOOTSTRAP_BODY_PROPERTIES: "body"
    GRAPHDB_LSA_BOOTSTRAP_DIMS: "200"

    # Enable auto-embed on Doc creation: body → embedding.
    GRAPHDB_AUTO_EMBED_ENABLED: "true"
    GRAPHDB_AUTO_EMBED_LABEL: "Doc"
    GRAPHDB_AUTO_EMBED_SOURCE_PROPERTY: "body"
    GRAPHDB_AUTO_EMBED_TARGET_PROPERTY: "embedding"

    # Optional pool tuning (defaults: 4 workers / 256 queue).
    # GRAPHDB_AUTO_EMBED_WORKERS: "4"
    # GRAPHDB_AUTO_EMBED_QUEUE_DEPTH: "256"
  ports:
    - "8080:8080"
  volumes:
    - graphdb_data:/data
```

**Note on bootstrap ordering**: the LSA bootstrap runs before the
auto-embed bootstrap (see `pkg/api/server_init.go`). If the LSA index
fails to build (corpus too small, vocabulary below `MaxVocab`), the
auto-embed observer still registers but every embed task drops with
`ErrNoIndexForTenant`. Check startup logs for both:

```
✅ Bootstrapped LSA index for default (... docs, 200 dims)
✅ Bootstrapped auto-embed observer (label="Doc", source="body", target="embedding", workers=4, queue=256)
```

If you see only the auto-embed line without the LSA line, the LSA build
failed — auto-embed will be configured but ineffective until you build
the index post-boot via the admin API.

## Bring it up + verify

```bash
docker-compose up -d graphdb
docker-compose logs graphdb | grep -E "✅|⚠️"
```

Get an auth token (default admin password is logged at first boot if
unset; see container logs):

```bash
TOKEN=$(curl -sX POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<password>"}' | jq -r '.token')
```

If the LSA bootstrap didn't fire (no corpus at startup), build it now
with a few seed documents, then build the index:

```bash
# Seed a corpus (skip if you bootstrapped via env).
for body in \
  "graph databases store nodes and edges" \
  "vector embeddings enable semantic search" \
  "hybrid retrieval combines keyword and dense methods"; do
  curl -sX POST http://localhost:8080/nodes \
    -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' \
    -d "{\"labels\":[\"Doc\"],\"properties\":{\"body\":{\"type\":\"string\",\"value\":\"$body\"}}}"
done

# Build the LSA index from the seed corpus.
curl -sX POST http://localhost:8080/hybrid-search/lsa-index \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"labels":["Doc"],"body_properties":["body"],"dims":200,"min_doc_freq":1}'
```

Now create a new Doc and confirm the embedding lands:

```bash
NODE_ID=$(curl -sX POST http://localhost:8080/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"labels":["Doc"],"properties":{"body":{"type":"string","value":"new document about graph embeddings"}}}' \
  | jq -r '.id')

# Give the async pool a moment to process.
sleep 1

# Read back the node; the `embedding` property should be a vector.
curl -s "http://localhost:8080/nodes/$NODE_ID" \
  -H "Authorization: Bearer $TOKEN" \
  | jq '.properties.embedding'
```

If the embedding property is present and its `value` is an array of
floats (length matches the configured `dims`), auto-embed is working
end-to-end.

## What this validates

Together with the closing-PRs in the verification gap closure arc, this
quickstart documents the **deployment dimension** of validating the F4
spike §5 + S11 spike §7.5 designs end-to-end:

| Dimension | Verification |
|---|---|
| Memory footprint (Decision 2's Option A) | PR #195 — bench validated 3.46 GB at 100 tenants × 10k vectors × 768 dims (vs spike's 3.2 GB estimate; +8% delta) |
| Backpressure under load (spike §7.5 drop-on-full) | PR #196 — load test validated CreateNode latency stays at 1.50ms even under 97% pool-drop saturation |
| **Deployment path** (R2.5b env-driven wiring) | **This doc** — documents the operator path; runtime exercise is the operator's |

## Known caveats

- **Update-driven embedding is deferred.** `OnNodeUpdated` is a no-op
  in R2.5a (see `pkg/intelligence/auto_embed_observer.go` for the TODO
  rationale). Mutating a Doc's `body` does not refresh its `embedding`.
  Workaround: delete + recreate, or call `POST /v1/embeddings` manually.

- **User-provided embeddings are preserved.** If `CreateNode*` is
  called with the target property already set (e.g.,
  `{"body": "...", "embedding": {"type":"vector", "value":[...]}}`),
  the observer skips the writeback — the user-supplied vector wins.

- **Single-policy via env.** The env-driven bootstrap supports exactly
  one `(label, source, target)` triple. Multi-policy requires
  programmatic wiring (call `intelligence.NewAutoEmbedObserver` with
  a multi-`EmbeddingPolicy` slice) — out of scope for the env path.

- **Auto-embed runs only on the in-memory `*GraphStorage` backend.**
  The `*BTreeGraphStorage` backend (C2 experimental) has a no-op
  `AddObserver`; auto-embed configured against it will silently never
  fire. The compile-time `var _ Storage = (*BTreeGraphStorage)(nil)`
  assertion makes the no-op explicit; runtime configuration that wires
  auto-embed against BTree storage logs the bootstrap line but produces
  no embeddings.

## References

- **Spec**: [`S11_AUTO_EMBEDDER_REDESIGN.md`](./internals/design/S11_AUTO_EMBEDDER_REDESIGN.md)
- **Vector tenant model**: [`F4_VECTOR_TENANT_REDESIGN.md`](./internals/design/F4_VECTOR_TENANT_REDESIGN.md)
- **Memory bench**: PR #195 (`pkg/storage/vector_index_memory_test.go`)
- **Load test**: PR #196 (`pkg/intelligence/auto_embed_observer_load_test.go`)
