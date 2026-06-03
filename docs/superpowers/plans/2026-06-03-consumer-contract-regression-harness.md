# Consumer-Contract Regression Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Generalize the four Track-Q pins (#283/#286/#287/#288) into a standing, graphdb-owned, growing consumer-contract mechanism — greppable CI-enforced tests, a catalogue with a growth rule, and a reproducible on-demand consumer-drive drill.

**Architecture:** Three components. (1) Tag each consumer-relied invariant test with `// CONSUMER CONTRACT: <id>` so the set is greppable; existing pins are tagged in place (no churn), one new test fills the `filter_labels` gap. (2) `docs/CONSUMER_CONTRACTS.md` catalogues invariant→consumer→test→bug and states the growth rule. (3) `scripts/consumer-drive.sh` + committed deterministic embedder + synthetic-corpus generator run the real consumers on demand (no external deps), with a documented CI-promotion path. No live-consumer CI job (blocked: `understand-graphdb` remoteless, `coi-screen` private).

**Tech Stack:** Go (`testing`, `net/http/httptest`), Bash, Python 3 stdlib (`http.server`, `hashlib`, `csv`). Spec: `docs/superpowers/specs/2026-06-03-consumer-contract-regression-harness-design.md`.

---

## File structure

- **Modify (one tag-comment line each):**
  - `pkg/api/handlers_vectors_rest_ingest_test.go` — CC1
  - `pkg/api/handlers_vectors_nn_correctness_test.go` — CC2
  - `pkg/storage/vector_nn_ordering_test.go` — CC2
  - `pkg/storage/edge_adjacency_reopen_test.go` — CC3
  - `pkg/storage/batch_tenant_index_test.go` — CC4 (two funcs)
- **Create:**
  - `pkg/api/consumer_contract_label_filter_test.go` — CC5 (new test)
  - `docs/CONSUMER_CONTRACTS.md` — catalogue + growth rule
  - `scripts/embed-server.py` — deterministic OpenAI-compatible embedder
  - `scripts/gen-icij-synth.py` — synthetic ICIJ-shaped corpus generator
  - `scripts/consumer-drive.sh` — on-demand consumer-drive drill
- **Modify:** `CLAUDE.md` — one pointer line to `CONSUMER_CONTRACTS.md`

The tag convention is the unifying layer (contracts span two packages, so a single
consolidated test func is impossible — see spec). Each `CC*` id is stable and greppable.

---

### Task 1: Tag CC5's missing test — label-filtered vector search over the REST float-array path

**Files:**
- Create: `pkg/api/consumer_contract_label_filter_test.go`

This is the one genuinely-unpinned consumer contract: `understand-graphdb`/`coi-screen` pass
`filter_labels` to `/vector-search`, but no test exercises the label post-filter *on
float-array-ingested vectors* (the consumer's real path) — existing label-filter coverage
uses in-process `storage.VectorValue` (`TypeVector`), and CC1 covers float-array ingest
*without* a label filter. CC5 composes the two.

- [ ] **Step 1: Write the test**

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// CONSUMER CONTRACT: CC5-label-filtered-vector-search — understand-graphdb, coi-screen (Track Q/Q4)
//
// TestVectorSearch_RESTFloatArrayLabelFilter pins the label-filtered vector path
// on the consumer's real ingestion path: nodes created via POST /nodes with a
// JSON number array (decoded to TypeFloatArray, indexed via #246) and queried
// with filter_labels. Existing label-filter coverage uses in-process
// storage.VectorValue (TypeVector); CC1 covers float-array ingest without a
// label filter. This composes both, exactly as understand-graphdb's neural
// search (filter_labels) and coi-screen exercise it.
//
// The raw-nearest node is deliberately an Image (wrong label); the filter must
// exclude it and return only the two Document nodes — proving the post-filter
// runs on float-array-ingested vectors, not just TypeVector ones.
func TestVectorSearch_RESTFloatArrayLabelFilter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	if err := server.graph.CreateVectorIndexForTenant(tenantID, "embedding", 3, 16, 200, "cosine"); err != nil {
		t.Fatalf("CreateVectorIndexForTenant: %v", err)
	}

	mk := func(labels []string, vec []float64) uint64 {
		t.Helper()
		rr := httptest.NewRecorder()
		server.createNode(rr, reqWithTenant(t, http.MethodPost, "/nodes", NodeRequest{
			Labels:     labels,
			Properties: map[string]any{"embedding": vec},
		}, tenantID))
		if rr.Code != http.StatusCreated {
			t.Fatalf("createNode: want 201, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal create: %v", err)
		}
		return resp.ID
	}

	docHit := mk([]string{"Document"}, []float64{1, 0, 0})
	imgNearest := mk([]string{"Image"}, []float64{0.97, 0.03, 0}) // raw-nearest, wrong label
	docNear := mk([]string{"Document"}, []float64{0.9, 0.1, 0})

	rr := httptest.NewRecorder()
	server.handleVectorSearch(rr, reqWithTenant(t, http.MethodPost, "/vector-search", VectorSearchRequest{
		PropertyName: "embedding",
		QueryVector:  []float32{1, 0, 0},
		K:            5,
		FilterLabels: []string{"Document"},
	}, tenantID))
	if rr.Code != http.StatusOK {
		t.Fatalf("vector-search status %d: %s", rr.Code, rr.Body.String())
	}
	var resp VectorSearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode search: %v", err)
	}

	got := map[uint64]bool{}
	for _, r := range resp.Results {
		got[r.NodeID] = true
	}
	if len(resp.Results) != 2 {
		t.Fatalf("got %d results, want 2 Document nodes — label filter on float-array-ingested vectors", len(resp.Results))
	}
	if !got[docHit] || !got[docNear] {
		t.Errorf("top-2 = %v, want Document cluster {%d,%d}", got, docHit, docNear)
	}
	if got[imgNearest] {
		t.Errorf("Image node %d (raw-nearest) leaked past filter_labels=[Document]", imgNearest)
	}
}
```

- [ ] **Step 2: Run the test — verify PASS on current `main`**

Run: `go test ./pkg/api/ -run 'TestVectorSearch_RESTFloatArrayLabelFilter' -count=1 -timeout 90s -v`
Expected: `PASS` (CC5 pins existing-correct behaviour; #246 + the label post-filter both already work).

- [ ] **Step 3: Neuter-verify it's a real pin**

Temporarily change the test's `FilterLabels: []string{"Document"}` to `FilterLabels: nil`, re-run.
Expected: `FAIL` with "got 3 results, want 2" (the Image leaks in) — confirming the assertion is load-bearing on the filter. Then **revert** the line back to `[]string{"Document"}` and re-run → `PASS`.

- [ ] **Step 4: Commit**

```bash
git add pkg/api/consumer_contract_label_filter_test.go
git commit -m "test(api): pin label-filtered vector search on REST float-array path (CC5)"
```

---

### Task 2: Tag the four existing pins as consumer contracts

**Files:**
- Modify: `pkg/api/handlers_vectors_rest_ingest_test.go`
- Modify: `pkg/api/handlers_vectors_nn_correctness_test.go`
- Modify: `pkg/storage/vector_nn_ordering_test.go`
- Modify: `pkg/storage/edge_adjacency_reopen_test.go`
- Modify: `pkg/storage/batch_tenant_index_test.go`

Each edit inserts the tag as the final line of the test's doc comment (contiguous with the
existing comment, immediately above `func`, so it stays part of the doc comment and is
greppable).

- [ ] **Step 1: Tag CC1**

In `pkg/api/handlers_vectors_rest_ingest_test.go`, replace:
```go
func TestVectorSearch_RESTFloatArrayIngestionRoundTrip(t *testing.T) {
```
with:
```go
// CONSUMER CONTRACT: CC1-rest-vector-ingest — understand-graphdb neural (#286)
func TestVectorSearch_RESTFloatArrayIngestionRoundTrip(t *testing.T) {
```

- [ ] **Step 2: Tag CC2 (REST half)**

In `pkg/api/handlers_vectors_nn_correctness_test.go`, replace:
```go
func TestVectorSearch_NearestNeighbourCorrectness(t *testing.T) {
```
with:
```go
// CONSUMER CONTRACT: CC2-vector-nn-identity — understand-graphdb (#283)
func TestVectorSearch_NearestNeighbourCorrectness(t *testing.T) {
```

- [ ] **Step 3: Tag CC2 (storage half)**

In `pkg/storage/vector_nn_ordering_test.go`, replace:
```go
func TestVectorSearchForTenant_KnownAnswerOrdering(t *testing.T) {
```
with:
```go
// CONSUMER CONTRACT: CC2-vector-nn-identity — understand-graphdb (#283)
func TestVectorSearchForTenant_KnownAnswerOrdering(t *testing.T) {
```

- [ ] **Step 4: Tag CC3**

In `pkg/storage/edge_adjacency_reopen_test.go`, replace:
```go
func TestEdgeAdjacencySurvivesReopen(t *testing.T) {
```
with:
```go
// CONSUMER CONTRACT: CC3-adjacency-reopen — coi-screen, Stór (#287)
func TestEdgeAdjacencySurvivesReopen(t *testing.T) {
```

- [ ] **Step 5: Tag CC4 (both funcs)**

In `pkg/storage/batch_tenant_index_test.go`, replace:
```go
func TestBatchCommit_VisibleToForTenantReaders(t *testing.T) {
```
with:
```go
// CONSUMER CONTRACT: CC4-bulkimport-tenant-visible — coi-screen / import-icij (#288)
func TestBatchCommit_VisibleToForTenantReaders(t *testing.T) {
```
and replace:
```go
func TestBatchCommit_VisibleAfterReopen(t *testing.T) {
```
with:
```go
// CONSUMER CONTRACT: CC4-bulkimport-tenant-visible — coi-screen / import-icij (#288)
func TestBatchCommit_VisibleAfterReopen(t *testing.T) {
```

- [ ] **Step 6: Verify the tagged set is complete and greppable**

Run: `grep -rn "CONSUMER CONTRACT:" pkg/ | sort`
Expected: 7 matches — CC1 (×1), CC2 (×2), CC3 (×1), CC4 (×2), CC5 (×1).

- [ ] **Step 7: Verify nothing broke (tags are comments only)**

Run: `go build ./... && go vet ./pkg/api/ ./pkg/storage/`
Expected: clean. Then `go test ./pkg/api/ ./pkg/storage/ -run 'TestVectorSearch_RESTFloatArrayIngestionRoundTrip|TestVectorSearch_NearestNeighbourCorrectness|TestVectorSearchForTenant_KnownAnswerOrdering|TestEdgeAdjacencySurvivesReopen|TestBatchCommit_' -count=1 -timeout 120s`
Expected: `ok` for both packages.

- [ ] **Step 8: Commit**

```bash
git add pkg/api/handlers_vectors_rest_ingest_test.go pkg/api/handlers_vectors_nn_correctness_test.go pkg/storage/vector_nn_ordering_test.go pkg/storage/edge_adjacency_reopen_test.go pkg/storage/batch_tenant_index_test.go
git commit -m "test: tag consumer-contract pins CC1-CC4 (greppable contract set)"
```

---

### Task 3: Write the contract catalogue

**Files:**
- Create: `docs/CONSUMER_CONTRACTS.md`

- [ ] **Step 1: Write the catalogue**

```markdown
# Consumer contracts

A **consumer contract** is a graphdb behaviour a real downstream consumer depends on, pinned
by a graphdb-owned test that fails against the pre-fix code. They exist because Track Q showed
that the dangerous bugs live at consumer integration seams (REST decode, cross-process
snapshot reopen, batch-write → tenant-read) that white-box unit tests structurally miss — and
were only found by driving the real consumers. This file is the registry; the tests are the
enforcement.

Find the tests: `grep -rn "CONSUMER CONTRACT:" pkg/`

| id | Invariant | Consumer(s) | Guarding test(s) | Origin |
|----|-----------|-------------|------------------|--------|
| CC1-rest-vector-ingest | A JSON number-array property on a vector-indexed name is indexed + searchable over REST | understand-graphdb (neural) | `pkg/api` `TestVectorSearch_RESTFloatArrayIngestionRoundTrip` | #286 |
| CC2-vector-nn-identity | Vector search returns the actually-nearest nodes by identity + order, not just count | understand-graphdb | `pkg/api` `TestVectorSearch_NearestNeighbourCorrectness`; `pkg/storage` `TestVectorSearchForTenant_KnownAnswerOrdering` | #283 |
| CC3-adjacency-reopen | Edge adjacency survives a snapshot `Close()`→reopen under the default compression config | coi-screen, Stór | `pkg/storage` `TestEdgeAdjacencySurvivesReopen` | #287 |
| CC4-bulkimport-tenant-visible | Data written via the batch/bulk-import path is visible to every `*ForTenant` reader, in-memory and after reopen | coi-screen / import-icij | `pkg/storage` `TestBatchCommit_VisibleToForTenantReaders`, `TestBatchCommit_VisibleAfterReopen` | #288 |
| CC5-label-filtered-vector-search | `filter_labels` post-filters correctly on float-array-ingested vectors over REST | understand-graphdb, coi-screen | `pkg/api` `TestVectorSearch_RESTFloatArrayLabelFilter` | Q4 |

## Growth rule

When driving a consumer surfaces a divergence, the fix lands with **(a)** a tagged contract
test (`// CONSUMER CONTRACT: <id> — <consumer> (<PR>)`) that fails against the pre-fix code,
and **(b)** a new row here. A contract is retired only when its consumer is. New contract
tests live in the package that owns the behaviour (storage invariant → `pkg/storage`; REST
invariant → `pkg/api`); there is no single consolidated suite because contracts span packages.

## High-fidelity drive

The tests above are in-process. To drive the *real* consumers end-to-end on demand (the check
that originally found these bugs), run `scripts/consumer-drive.sh` — it builds graphdb, runs
`coi-screen` (embedded) against a synthetic ICIJ corpus, and runs `understand-graphdb`'s
integration suite against a local server with a deterministic embedder. No external keys or
corpus needed. Promoting it to CI is blocked today (understand-graphdb has no remote;
coi-screen is private) — see the script header for prerequisites.
```

- [ ] **Step 2: Commit**

```bash
git add docs/CONSUMER_CONTRACTS.md
git commit -m "docs: consumer-contract catalogue + growth rule (Track Q/Q4)"
```

---

### Task 4: Commit the deterministic embedder + synthetic-corpus generator as fixtures

**Files:**
- Create: `scripts/embed-server.py`
- Create: `scripts/gen-icij-synth.py`

- [ ] **Step 1: Write `scripts/embed-server.py`**

```python
#!/usr/bin/env python3
"""Deterministic OpenAI-compatible embeddings server for consumer-drive.sh.

A hashing vectorizer: tokenize, hash each token to a dimension + sign, accumulate,
L2-normalize. Deterministic (same text -> same vector) and crudely lexical, enough to drive +
assert understand-graphdb's neural search without any model or API key.

POST <any path>  body {"model":..., "input": str | [str,...]}
                 -> {"data":[{"embedding":[...]}], "model":..., "object":"list"}
Listens on 127.0.0.1:8090.
"""
import hashlib
import json
import math
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

DIMS = 64
_token = re.compile(r"[A-Za-z0-9_]+")


def embed(text: str) -> list[float]:
    vec = [0.0] * DIMS
    toks = _token.findall(text.lower()) or ["__empty__"]
    for tok in toks:
        h = hashlib.md5(tok.encode()).digest()
        vec[h[0] % DIMS] += 1.0 if (h[1] & 1) else -1.0
    norm = math.sqrt(sum(x * x for x in vec)) or 1.0
    return [x / norm for x in vec]


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", 0))) or b"{}")
        inp = body.get("input", [])
        texts = [inp] if isinstance(inp, str) else list(inp)
        payload = json.dumps({
            "object": "list",
            "data": [{"object": "embedding", "index": i, "embedding": embed(t)} for i, t in enumerate(texts)],
            "model": body.get("model", "deterministic-hash-64"),
            "usage": {"prompt_tokens": 0, "total_tokens": 0},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 8090), Handler).serve_forever()
```

- [ ] **Step 2: Write `scripts/gen-icij-synth.py`**

```python
#!/usr/bin/env python3
"""Synthetic ICIJ-shaped corpus generator for consumer-drive.sh.

Emits nodes.csv + edges.csv in the schema cmd/import-icij expects. Plants a clean 2-hop
conflict (two named Officers sharing one Entity) and >maxDegree hubs. Deterministic (seeded).
Usage: gen-icij-synth.py <outdir>
"""
import csv
import os
import random
import sys

random.seed(1729)
N_ENTITY, N_OFFICER, N_INTERMEDIARY, N_ADDRESS = 20000, 18000, 2000, 10000
JURIS = ["BVI", "PAN", "CYM", "JEY", "BMU", "SAM", "SYC", "MLT"]
outdir = sys.argv[1] if len(sys.argv) > 1 else "/tmp/icij-synth"

nodes, nid = [], 0
def add(name, ntype, juris=""):
    global nid
    nid += 1
    nodes.append((str(nid), name, juris, ntype))
    return str(nid)

acme = add("Acme Holdings Ltd", "Entity", "BVI")
smith = add("Robert Smith", "Officer")
doe = add("Jane Doe", "Officer")
entities = [acme] + [add(f"Entity {i} Ltd", "Entity", random.choice(JURIS)) for i in range(N_ENTITY - 1)]
officers = [smith, doe] + [add(f"Officer Person {i}", "Officer") for i in range(N_OFFICER - 2)]
intermediaries = [add(f"Law Firm {i}", "Intermediary", random.choice(JURIS)) for i in range(N_INTERMEDIARY)]
addresses = [add(f"{i} Offshore Plaza", "Address") for i in range(N_ADDRESS)]

edges = []
def edge(rt, a, b):
    edges.append((rt, a, b))

edge("officer_of", smith, acme)
edge("officer_of", doe, acme)
for off in officers[2:]:
    for _ in range(random.randint(1, 2)):
        edge("officer_of", off, random.choice(entities))
for inter in intermediaries[2:]:
    for _ in range(random.randint(1, 4)):
        edge("intermediary_of", inter, random.choice(entities))
for hub in intermediaries[:2]:
    for ent in random.sample(entities, 3000):
        edge("intermediary_of", hub, ent)
for ent in entities:
    edge("registered_address", ent, random.choice(addresses))
for ent in random.sample(entities, 3000):
    edge("registered_address", ent, addresses[0])

os.makedirs(outdir, exist_ok=True)
with open(f"{outdir}/nodes.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["node_id", "name", "jurisdiction", "country_codes", "countries", "node_type", "sourceID", "address", "valid_until", "note"])
    for (i, name, juris, ntype) in nodes:
        w.writerow([i, name, juris, "", "", ntype, "synthetic", "", "", ""])
with open(f"{outdir}/edges.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["rel_type", "node_id_start", "node_id_end", "link", "status", "start_date", "end_date"])
    for (rt, a, b) in edges:
        w.writerow([rt, a, b, rt, "", "", ""])

print(f"nodes={len(nodes)} edges={len(edges)} -> {outdir}")
print(f"planted: Acme={acme} Smith={smith} Doe={doe} (both officer_of Acme)")
```

- [ ] **Step 3: Smoke-test both helpers**

```bash
python3 scripts/gen-icij-synth.py /tmp/icij-synth | grep "planted:"
python3 scripts/embed-server.py >/tmp/embed.log 2>&1 &
EPID=$!; sleep 1
curl -s -X POST http://localhost:8090/v1/embeddings -d '{"input":"resolveCalls"}' | python3 -c 'import sys,json; v=json.load(sys.stdin)["data"][0]["embedding"]; print("dims=%d norm=%.3f" % (len(v), sum(x*x for x in v)**0.5))'
kill $EPID
```
Expected: `planted: Acme=1 Smith=2 Doe=3 ...` and `dims=64 norm=1.000`.

- [ ] **Step 4: Commit**

```bash
git add scripts/embed-server.py scripts/gen-icij-synth.py
git commit -m "test(scripts): deterministic embedder + synthetic ICIJ corpus generator (Q4 fixtures)"
```

---

### Task 5: Write the consumer-drive drill

**Files:**
- Create: `scripts/consumer-drive.sh`

- [ ] **Step 1: Write `scripts/consumer-drive.sh`**

```bash
#!/usr/bin/env bash
# consumer-drive.sh — drive graphdb's real consumers against the current checkout.
#
# The high-fidelity consumer-contract check (Track Q/Q4). Builds graphdb, then drives
# coi-screen (embedded) + understand-graphdb (REST) with deterministic, key-free fixtures.
# No external corpus, no API keys.
#
# Consumers are expected as sibling checkouts:  ../coi-screen  ../understand-graphdb
# Absent consumers are SKIPPED loudly (not a graphdb failure). Exit codes:
#   0 = all present consumers passed (skips allowed)
#   1 = a present consumer FAILED
#
# PROMOTE TO CI when both prerequisites are met (currently blocked):
#   - understand-graphdb pushed to a git remote (it is local-only today)
#   - a coi-screen deploy key available as a CI secret (it is private today)
# Then a workflow can: checkout all three repos, then run this script.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COI="${COI_SCREEN_REPO:-$REPO/../coi-screen}"
UG="${UNDERSTAND_GRAPHDB_REPO:-$REPO/../understand-graphdb}"
PORT="${GRAPHDB_PORT:-8080}"
DATA="/tmp/consumer-drive-data"
fail=0; ran=0

log() { echo "[consumer-drive] $*" >&2; }

log "building graphdb server + importer from $REPO"
( cd "$REPO" && go build -o /tmp/cd-server ./cmd/server && go build -o /tmp/cd-import ./cmd/import-icij ) || { log "graphdb build FAILED"; exit 1; }

# --- coi-screen: embedded library + synthetic-corpus screen ---
if [ -d "$COI/cmd/coi" ]; then
  ran=1
  log "coi-screen: go test ./... (embedded contract)"
  ( cd "$COI" && go test ./... -count=1 -timeout 180s ) || { log "coi-screen suite FAILED"; fail=1; }
  log "coi-screen: synthetic-corpus screen"
  python3 "$REPO/scripts/gen-icij-synth.py" /tmp/cd-icij-synth >/dev/null
  rm -rf "$DATA-icij" && ( cd /tmp && /tmp/cd-import --nodes /tmp/cd-icij-synth/nodes.csv --edges /tmp/cd-icij-synth/edges.csv --data "$DATA-icij" ) >/dev/null 2>&1
  out=$( cd "$COI" && go run ./cmd/coi --data "$DATA-icij" --party "Robert Smith" --party "Jane Doe" --max-hops 2 2>/dev/null )
  if echo "$out" | python3 -c 'import sys,json; d=json.load(sys.stdin); sys.exit(0 if (d and d[0]["flagged"]) else 1)'; then
    log "coi-screen: planted conflict flagged OK"
  else
    log "coi-screen: planted conflict NOT flagged — FAIL"; fail=1
  fi
else
  log "SKIP coi-screen (not found at $COI)"
fi

# --- understand-graphdb: REST integration suite against a live server ---
if [ -f "$UG/package.json" ]; then
  ran=1
  log "understand-graphdb: starting graphdb server + deterministic embedder"
  rm -rf "$DATA-ug" && mkdir -p "$DATA-ug"
  JWT_SECRET="dev-secret-consumer-drive-0123456789abcdef" ADMIN_PASSWORD="admin123" GRAPHDB_ENV="test" \
    /tmp/cd-server --port "$PORT" --data "$DATA-ug" >/tmp/cd-graphdb.log 2>&1 &
  SPID=$!
  python3 "$REPO/scripts/embed-server.py" >/tmp/cd-embed.log 2>&1 &
  EPID=$!
  for _ in $(seq 1 20); do curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1 && break; sleep 1; done
  if curl -sf "http://localhost:$PORT/health" >/dev/null 2>&1; then
    TOKEN=$(curl -s -X POST "http://localhost:$PORT/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["access_token"])')
    KEY=$(curl -s -X POST "http://localhost:$PORT/api/v1/apikeys" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"consumer-drive","environment":"test"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["key"])')
    log "understand-graphdb: GRAPHDB_INTEGRATION=1 npm test (FTS/LSA native config)"
    ( cd "$UG" && GRAPHDB_INTEGRATION=1 GRAPHDB_URL="http://localhost:$PORT" GRAPHDB_API_KEY="$KEY" GRAPHDB_TENANT="default" npm test ) || { log "understand-graphdb suite FAILED"; fail=1; }
  else
    log "understand-graphdb: graphdb server did not become healthy — FAIL"; fail=1
  fi
  kill $SPID $EPID 2>/dev/null
else
  log "SKIP understand-graphdb (not found at $UG)"
fi

[ "$ran" = 0 ] && log "WARNING: no consumers found — nothing driven"
if [ "$fail" = 0 ]; then log "consumer-drive: PASS"; else log "consumer-drive: FAIL"; fi
exit $fail
```

- [ ] **Step 2: Make executable + run end-to-end**

```bash
chmod +x scripts/consumer-drive.sh
scripts/consumer-drive.sh; echo "exit=$?"
```
Expected: log lines showing coi-screen suite + planted-conflict-flagged-OK and understand-graphdb integration PASS; final `consumer-drive: PASS` and `exit=0`. (If a sibling consumer is absent in this environment, it logs `SKIP` and still exits 0 — that is correct behaviour.)

- [ ] **Step 3: Commit**

```bash
git add scripts/consumer-drive.sh
git commit -m "test(scripts): consumer-drive drill — drive coi-screen + understand-graphdb against main (Q4)"
```

---

### Task 6: Point CLAUDE.md at the catalogue

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add the pointer**

In `CLAUDE.md`, under the "## Common workflows" section, after the "### Pre-PR" subsection, add:

```markdown
### Consumer contracts

graphdb owns regression tests for behaviours its downstream consumers depend on. See
`docs/CONSUMER_CONTRACTS.md` (catalogue + growth rule) and `grep -rn "CONSUMER CONTRACT:" pkg/`.
When driving a consumer surfaces a divergence, fix it in graphdb with a tagged contract test +
a catalogue row. `scripts/consumer-drive.sh` runs the real consumers on demand.
```

- [ ] **Step 2: Verify CLAUDE.md still parses (it's markdown — just confirm the section landed)**

Run: `grep -n "Consumer contracts" CLAUDE.md`
Expected: one match under Common workflows.

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: point CLAUDE.md at the consumer-contract catalogue (Q4)"
```

---

### Task 7: Pre-PR preflight

- [ ] **Step 1: Build, vet, lint at CI surface**

Run:
```bash
go build ./... && go vet ./pkg/api/ ./pkg/storage/ && golangci-lint run ./...
```
Expected: no errors; `golangci-lint` → `0 issues`.

- [ ] **Step 2: Run the affected Go test packages**

Run: `go test ./pkg/api/ ./pkg/storage/ -count=1 -timeout 300s`
Expected: `ok` for both. (CC5 + all tagged pins green; tags are comments, so no behaviour change.)

- [ ] **Step 3: Confirm the contract set is coherent**

Run: `grep -rn "CONSUMER CONTRACT:" pkg/ | wc -l` → expect `7`.
Run: `grep -c "^| CC" docs/CONSUMER_CONTRACTS.md` → expect `5` (one row per CC id).

- [ ] **Step 4: Open the PR**

```bash
git push -u origin <branch>
gh pr create --base main --title "test+docs: consumer-contract regression harness (Track Q/Q4)" --body "Implements docs/superpowers/specs/2026-06-03-consumer-contract-regression-harness-design.md: CC5 label-filter pin + CC1-CC4 tags + CONSUMER_CONTRACTS.md catalogue + scripts/consumer-drive.sh (deterministic, key-free). No live-consumer CI (blocked: remoteless/private consumers) — documented promotion path. Closes Track Q."
```
Expected: PR URL. CI `UNSTABLE` only from the benchmark comment-step is tolerated (see CLAUDE.md "Known infra patterns"); all functional checks must pass.

---

## Notes for the executor

- **Branch**: create one feature branch for all tasks (e.g. `test/q4-consumer-contract-harness`); the per-task commits collapse on squash-merge.
- **CC5 passes immediately** — it pins existing-correct behaviour; the neuter step (Task 1 Step 3) is what proves it's load-bearing. Do not skip the revert.
- **Tag comments must stay contiguous** with each test's existing doc comment (no blank line between the tag and `func`), or Go treats them as detached comments — still greppable, but the doc-comment association is cleaner without the gap.
- **`consumer-drive.sh` SKIP vs FAIL**: absent sibling consumers are skips (exit 0); only a present consumer's failure is exit 1. This keeps the drill runnable in environments without the siblings without producing false reds.
- The spec's "future work" (live-consumer CI job, batch delete/update gap, real ICIJ corpus run) is **out of scope** for this plan — do not implement.
