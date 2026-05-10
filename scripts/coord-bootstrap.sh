#!/usr/bin/env bash
# coord-bootstrap.sh — start a graphdb coord instance and mint a long-lived API key.
#
# Idempotent: detects an already-running daemon on :8090, reuses an existing
# API key file at ~/.graphdb-coord-key, and only does work that hasn't been
# done. Safe to re-run after a daemon restart.
#
# Output: writes ~/.graphdb-coord-key (mode 0600) and exports
# GRAPHDB_COORD_URL + GRAPHDB_COORD_TOKEN for downstream use.
#
# See docs/COORD_DEPLOY_SPIKE_2026-05-10.md for the design that motivated
# this script. Once that spike's PR 1 (B-lite resolver) lands, the option-A
# advisory uniqueness in coord-seed.sh becomes server-enforced.

set -euo pipefail

COORD_PORT="${COORD_PORT:-8090}"
COORD_DATA_DIR="${COORD_DATA_DIR:-$HOME/.graphdb-coord-data}"
COORD_KEY_FILE="${COORD_KEY_FILE:-$HOME/.graphdb-coord-key}"
COORD_BIN="${COORD_BIN:-/tmp/graphdb-coord}"
COORD_URL="http://localhost:${COORD_PORT}"

# JWT secret only matters server-side. Stable across runs so the daemon
# accepts existing tokens after restart. Not for production.
export JWT_SECRET="${JWT_SECRET:-dev-only-graphdb-coord-not-for-production}"

log() { echo "[coord-bootstrap] $*" >&2; }

# Step 1: build the binary if it's missing or older than the source tree.
if [[ ! -x "$COORD_BIN" ]] || [[ "$(find cmd/server -name '*.go' -newer "$COORD_BIN" 2>/dev/null | head -1)" ]]; then
  log "building cmd/server → $COORD_BIN"
  go build -o "$COORD_BIN" ./cmd/server/
else
  log "binary at $COORD_BIN is up to date"
fi

# Step 2: ensure data dir exists.
mkdir -p "$COORD_DATA_DIR"

# Step 3: start the daemon if nothing's listening on COORD_PORT.
if lsof -iTCP:"$COORD_PORT" -sTCP:LISTEN -P >/dev/null 2>&1; then
  log "daemon already listening on :$COORD_PORT — reusing"
else
  log "starting daemon on :$COORD_PORT (data: $COORD_DATA_DIR)"
  # cd into the data dir so .graphdb_admin_password lands there.
  (cd "$COORD_DATA_DIR" && nohup "$COORD_BIN" --port "$COORD_PORT" --data "$COORD_DATA_DIR" \
    > "$COORD_DATA_DIR/server.log" 2>&1 &)

  # Wait for the daemon to be reachable. The license-validation + index-load
  # path can take a few seconds on first start.
  for i in $(seq 1 20); do
    if curl -sSf "$COORD_URL/health" >/dev/null 2>&1 \
       || curl -sS "$COORD_URL/health" >/dev/null 2>&1; then
      log "daemon reachable after ${i}s"
      break
    fi
    if [[ $i -eq 20 ]]; then
      log "ERROR: daemon didn't become reachable in 20s. Check $COORD_DATA_DIR/server.log"
      exit 1
    fi
    sleep 1
  done
fi

# Step 4: reuse existing API key if we have one. Verify it still works.
if [[ -r "$COORD_KEY_FILE" ]]; then
  EXISTING_KEY=$(cat "$COORD_KEY_FILE")
  # Verify by hitting an authenticated endpoint. /nodes works with API keys.
  if curl -sSf -H "X-API-Key: $EXISTING_KEY" "$COORD_URL/nodes" >/dev/null 2>&1; then
    log "existing API key at $COORD_KEY_FILE still valid — reusing"
    export GRAPHDB_COORD_URL="$COORD_URL"
    export GRAPHDB_COORD_TOKEN="$EXISTING_KEY"
    echo "GRAPHDB_COORD_URL=$COORD_URL"
    echo "GRAPHDB_COORD_TOKEN=<read from $COORD_KEY_FILE>"
    exit 0
  fi
  log "existing API key didn't authenticate — will mint a new one"
fi

# Step 5: login as admin to get a JWT, then mint an API key.
ADMIN_PW_FILE="$COORD_DATA_DIR/.graphdb_admin_password"
if [[ ! -r "$ADMIN_PW_FILE" ]]; then
  log "ERROR: admin password file missing at $ADMIN_PW_FILE."
  log "  This usually means the daemon was started from a different directory."
  log "  Stop the daemon (kill \$(lsof -t -i:$COORD_PORT)) and re-run this script."
  exit 1
fi
ADMIN_PW=$(cat "$ADMIN_PW_FILE")

log "logging in as admin to mint coord-agent API key"
ADMIN_JWT=$(curl -sSf -X POST "$COORD_URL/auth/login" \
  -H 'Content-Type: application/json' \
  -d "$(printf '{"username":"admin","password":"%s"}' "$ADMIN_PW")" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")

# expires_in: 0 means never expires. The key is for service-to-service use,
# rotation happens via DELETE /api/v1/apikeys/{id} when needed.
APIKEY_RESPONSE=$(curl -sSf -X POST -H "Authorization: Bearer $ADMIN_JWT" \
  -H 'Content-Type: application/json' \
  "$COORD_URL/api/v1/apikeys" \
  -d '{"name":"coord-agent","permissions":["read","write"],"expires_in":0}')
COORD_KEY=$(echo "$APIKEY_RESPONSE" | python3 -c "import json,sys; print(json.load(sys.stdin)['key'])")

# Persist with restrictive perms.
echo "$COORD_KEY" > "$COORD_KEY_FILE"
chmod 600 "$COORD_KEY_FILE"
log "minted API key, saved to $COORD_KEY_FILE (mode 0600)"

# Print exports for the caller to source.
echo "GRAPHDB_COORD_URL=$COORD_URL"
echo "GRAPHDB_COORD_TOKEN=<read from $COORD_KEY_FILE>"
log "done. Add these to your shell rc to persist:"
log "  export GRAPHDB_COORD_URL=$COORD_URL"
log "  export GRAPHDB_COORD_TOKEN=\$(cat $COORD_KEY_FILE)"
