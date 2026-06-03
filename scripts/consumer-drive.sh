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
  rm -rf "$DATA-icij" && ( cd /tmp && /tmp/cd-import --nodes /tmp/cd-icij-synth/nodes.csv --edges /tmp/cd-icij-synth/edges.csv --data "$DATA-icij" ) >/dev/null 2>&1 || { log "coi-screen: synthetic corpus import FAILED"; fail=1; }
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
  wait $SPID $EPID 2>/dev/null
else
  log "SKIP understand-graphdb (not found at $UG)"
fi

[ "$ran" = 0 ] && log "WARNING: no consumers found — nothing driven"
if [ "$fail" = 0 ]; then log "consumer-drive: PASS"; else log "consumer-drive: FAIL"; fi
exit $fail
