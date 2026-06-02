#!/usr/bin/env bash
# Track R component (1c) — auto-embed -> HNSW -> vector-search end-to-end exercise.
#
# Validates the IN-SERVER LSA auto-embed path:
#   POST /nodes (label=Doc, body=text)  -> AutoEmbedObserver fires
#     -> LSAEmbedder computes vector     -> UpdateNodeForTenant writes TypeVector
#     -> UpdateNodeVectorIndexes inserts into the per-tenant HNSW
#   POST /vector-search                  -> correct nearest neighbours
#
# Scope: this exercises the TypeVector writeback path gated on #243 (HNSW
# recall). It does NOT exercise #246 (TypeFloatArray REST ingest) — the
# observer writes VectorValue directly.
#
# Assertion is RANKING, not non-empty results: two lexically-distinct
# clusters; an in-cluster query must rank its own cluster above the other.
#
# Modes:
#   scripts/verify-track-r-1c-autoembed.sh            assert against $BASE_URL
#   scripts/verify-track-r-1c-autoembed.sh --docker   build+up the compose
#                                                     deployment, assert, tear down
set -euo pipefail

COMPOSE_FILE="$(cd "$(dirname "$0")/.." && pwd)/docker-compose.track-r-1c.yml"
DOCKER_MODE=0
[ "${1:-}" = "--docker" ] && DOCKER_MODE=1

if [ "$DOCKER_MODE" = 1 ]; then
  BASE_URL="${BASE_URL:-http://localhost:8088}"
  ADMIN_PASS="${ADMIN_PASS:-track-r-1c-admin-password}"
else
  # Local-binary mode default (matches the smoke harness port).
  BASE_URL="${BASE_URL:-http://localhost:8099}"
  ADMIN_PASS="${ADMIN_PASS:-1c-verify-admin-password}"
fi
ADMIN_USER="${ADMIN_USER:-admin}"
DIMS="${DIMS:-8}"

pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; exit 1; }
info() { printf '\033[36m==>\033[0m %s\n' "$1"; }

if [ "$DOCKER_MODE" = 1 ]; then
  info "Bringing up the compose deployment ($COMPOSE_FILE) ..."
  docker compose -f "$COMPOSE_FILE" up -d --build
  # Tear the deployment down on any exit (pass or fail).
  trap 'echo; info "Tearing down compose deployment ..."; docker compose -f "$COMPOSE_FILE" down -v >/dev/null 2>&1 || true' EXIT
fi

# ---- wait for server ----
info "Waiting for server at $BASE_URL ..."
for i in $(seq 1 30); do
  if curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then break; fi
  sleep 1
  [ "$i" = 30 ] && fail "server did not become healthy"
done
pass "server healthy"

# ---- auth ----
info "Logging in as $ADMIN_USER ..."
TOKEN=$(curl -fsS -X POST "$BASE_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" | jq -r '.access_token')
[ -n "$TOKEN" ] && [ "$TOKEN" != "null" ] || fail "login returned no access_token"
pass "got access token"

auth_curl() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }

# ---- create vector index on the target property BEFORE any traffic ----
# (CreateIndexForTenant does NOT backfill; the index must exist before the
#  observer's writeback fires or the vector lands on the node but never in HNSW.)
info "Creating vector index on 'embedding' (dims=$DIMS, cosine) ..."
auth_curl -X POST "$BASE_URL/vector-indexes" \
  -d "{\"property_name\":\"embedding\",\"dimensions\":$DIMS,\"metric\":\"cosine\"}" >/dev/null
pass "vector index created"

# ---- corpus: two lexically-distinct clusters ----
# ocean cluster vocabulary vs finance cluster vocabulary. LSA is term
# co-occurrence, so lexical (not just semantic) separation is what matters.
ocean_train=(
  "the ocean tide carries warm marine currents across the open sea"
  "coral reef ecosystems shelter fish along the shallow seabed"
  "a humpback whale migrates through deep marine ocean waters"
  "plankton drift on the surface current feeding schooling fish"
  "salt spray and crashing waves shape the rocky coastal tide"
  "the reef teems with marine fish above the sandy seabed"
  "ocean currents and tides move plankton across the open sea"
  "whale and coral and fish share the warm shallow marine reef"
)
finance_train=(
  "the bank approved a mortgage loan with fixed annual interest"
  "credit and debt accumulate when loan interest compounds yearly"
  "a deposit into the savings account grows the capital balance"
  "the ledger records every invoice payment and credit entry"
  "mortgage equity rises as the borrower pays down the loan"
  "the bank charges interest on the outstanding credit debt"
  "capital deposit and loan repayment appear on the ledger"
  "invoice payment and bank interest adjust the account balance"
)
# Traffic docs (created AFTER the LSA index is built -> these get embedded).
ocean_traffic=(
  "marine fish swim near the coral reef in the warm ocean tide"
  "deep ocean currents carry plankton past the migrating whale"
  "waves and salt spray break over the shallow coastal seabed"
)
finance_traffic=(
  "the bank issued a mortgage loan against the borrower equity"
  "interest on the credit card debt grows the outstanding balance"
  "a capital deposit and invoice payment post to the account ledger"
)

create_doc() { # body cluster phase
  auth_curl -X POST "$BASE_URL/nodes" \
    -d "$(jq -nc --arg b "$1" --arg c "$2" --arg p "$3" \
      '{labels:["Doc"],properties:{body:$b,cluster:$c,phase:$p}}')" | jq -r '.id'
}

# ---- seed training corpus (no LSA index yet -> observer drops these) ----
info "Seeding LSA training corpus (${#ocean_train[@]} ocean + ${#finance_train[@]} finance) ..."
for d in "${ocean_train[@]}";   do create_doc "$d" ocean   train >/dev/null; done
for d in "${finance_train[@]}"; do create_doc "$d" finance train >/dev/null; done
pass "training corpus seeded"

# ---- build the LSA index over the seeded corpus ----
info "Building LSA index (dims=$DIMS, min_doc_freq=1) ..."
LSA=$(auth_curl -X POST "$BASE_URL/hybrid-search/lsa-index" \
  -d "{\"labels\":[\"Doc\"],\"body_properties\":[\"body\"],\"dims\":$DIMS,\"min_doc_freq\":1}")
NDOCS=$(echo "$LSA" | jq -r '.indexed_docs')
echo "    LSA indexed_docs=$NDOCS dims=$(echo "$LSA" | jq -r '.dimensions')"
[ "$NDOCS" -ge "$DIMS" ] || fail "LSA build degenerate (indexed_docs=$NDOCS)"
pass "LSA index built"

# ---- drive traffic: NEW Doc nodes embedded by the observer via LSA ----
NTRAFFIC=$(( ${#ocean_traffic[@]} + ${#finance_traffic[@]} ))
info "Driving traffic ($NTRAFFIC nodes) -> auto-embed observer fires ..."
for d in "${ocean_traffic[@]}";   do create_doc "$d" ocean   traffic >/dev/null; done
for d in "${finance_traffic[@]}"; do create_doc "$d" finance traffic >/dev/null; done
pass "traffic created (async embedding in flight)"

# ---- helper: embed a query string via LSA (/v1/embeddings) ----
embed_query() {
  auth_curl -X POST "$BASE_URL/v1/embeddings" \
    -d "$(jq -nc --arg in "$1" '{input:$in}')" | jq -c '.data[0].embedding'
}

# ---- poll until all traffic vectors are indexed (async pool drain) ----
info "Waiting for async writeback to populate HNSW ..."
QV_OCEAN=$(embed_query "warm marine ocean reef fish swimming in the tide")
for i in $(seq 1 30); do
  CNT=$(auth_curl -X POST "$BASE_URL/vector-search" \
    -d "$(jq -nc --argjson q "$QV_OCEAN" --argjson k "$NTRAFFIC" \
      '{property_name:"embedding",query_vector:$q,k:$k}')" | jq -r '.count')
  [ "$CNT" = "$NTRAFFIC" ] && break
  sleep 1
  [ "$i" = 30 ] && fail "only $CNT/$NTRAFFIC traffic vectors indexed after 30s"
done
pass "all $NTRAFFIC traffic vectors indexed in HNSW"

# ---- RANKING assertion: in-cluster query ranks its own cluster on top ----
assert_ranking() { # query_vector_json expected_cluster label
  local qv="$1" want="$2" lbl="$3"
  local res top top_cluster ocean_in_top finance_in_top half
  res=$(auth_curl -X POST "$BASE_URL/vector-search" \
    -d "$(jq -nc --argjson q "$qv" '{property_name:"embedding",query_vector:$q,k:6,include_nodes:true}')")
  echo "    [$lbl] results (node_id score cluster):"
  echo "$res" | jq -r '.results[] | "      \(.node_id) \(.score|.*1000|floor/1000) \(.node.properties.cluster)"'
  top_cluster=$(echo "$res" | jq -r '.results[0].node.properties.cluster')
  [ "$top_cluster" = "$want" ] || fail "[$lbl] top result cluster=$top_cluster, expected $want"
  # Top half (k=6 -> top 3) should be dominated by the wanted cluster.
  half=$(echo "$res" | jq -r --arg w "$want" '[.results[0:3][] | select(.node.properties.cluster==$w)] | length')
  [ "$half" -ge 2 ] || fail "[$lbl] only $half/3 of top-3 are $want"
  pass "[$lbl] $want ranked on top ($half/3 of top-3); #1 is $want"
}

info "Ranking assertion: ocean query -> ocean docs on top"
assert_ranking "$QV_OCEAN" ocean "ocean-query"

QV_FINANCE=$(embed_query "bank mortgage loan interest on outstanding credit debt")
info "Ranking assertion: finance query -> finance docs on top"
assert_ranking "$QV_FINANCE" finance "finance-query"

printf '\n\033[32mALL CHECKS PASSED\033[0m — auto-embed -> HNSW -> vector-search verified end-to-end.\n'
