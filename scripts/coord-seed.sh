#!/usr/bin/env bash
# coord-seed.sh — seed initial :Task nodes from the planning doc into a
# running coord instance. Idempotent: skips Tasks whose `id` property
# already exists.
#
# Pre-requisite: coord-bootstrap.sh has run; ~/.graphdb-coord-key exists.
# Reads GRAPHDB_COORD_URL + GRAPHDB_COORD_TOKEN from env, falling back to
# defaults that match coord-bootstrap.sh's outputs.
#
# Atomicity note (option A — advisory): this script is single-writer at
# bootstrap time. If two operators run it simultaneously, duplicate :Task
# nodes for the same `id` could be created. Once PR 1 of the
# coord-deploy-spike rollout (resolver-side uniqueness for :Claim) lands,
# extending uniqueness to :Task is a one-line addition there. Until then,
# don't run this from two machines at once.

set -euo pipefail

COORD_URL="${GRAPHDB_COORD_URL:-http://localhost:8090}"
COORD_KEY_FILE="${COORD_KEY_FILE:-$HOME/.graphdb-coord-key}"
COORD_TOKEN="${GRAPHDB_COORD_TOKEN:-$(cat "$COORD_KEY_FILE" 2>/dev/null || true)}"

if [[ -z "$COORD_TOKEN" ]]; then
  echo "[coord-seed] ERROR: no API key. Run coord-bootstrap.sh first." >&2
  exit 1
fi

log() { echo "[coord-seed] $*" >&2; }

# Tasks to seed — derived from docs/NEXT_STEPS_2026-05-10.md.
# Format: "id:track:status:closing_pr_or_empty"
# Closed Tasks are seeded so dependency / audit-history queries work from
# day one (e.g., "what closed in May 2026?", "did A4-edges close before
# A8.2?"). Closing PR refs become :CLOSED_BY edges in a future iteration.
TASKS=(
  "H1:H:done:65,66"
  "H3:H:done:"
  "A4:A:done:67"
  "A4-edges:A:done:70"
  "A8.2:A:done:81"
  "F1.1-spike:F:open:"
  "F1.1-impl:F:open:"
  "F3:F:open:"
  "A8.1:A:open:"
  "H2:H:open:"
  "H4-PR1-blite:H:open:"
  "H4-PR2-bootstrap:H:open:"
  "H4-PR3-skill-rewrite:H:open:"
  "H4-PR4-planning-update:H:open:"
  "S1:S:open:"
)

# Read existing Task IDs once so we can skip duplicates.
log "fetching existing :Task ids for idempotency check"
EXISTING_IDS=$(curl -sSf -H "X-API-Key: $COORD_TOKEN" "$COORD_URL/nodes" \
  | python3 -c "
import json, sys, base64
nodes = json.load(sys.stdin)
ids = set()
for n in nodes:
    if 'Task' in n.get('labels', []):
        # Work around the REST /nodes base64-encoded properties bug:
        # n['properties']['id'] is base64-encoded if it's a string.
        raw_id = n['properties'].get('id', '')
        try:
            decoded = base64.b64decode(raw_id).decode('utf-8')
            ids.add(decoded)
        except Exception:
            ids.add(raw_id)
print('\n'.join(sorted(ids)))")

CREATED=0
SKIPPED=0
for entry in "${TASKS[@]}"; do
  task_id=$(echo "$entry" | cut -d: -f1)
  track=$(echo "$entry" | cut -d: -f2)
  task_status=$(echo "$entry" | cut -d: -f3)
  closing_pr=$(echo "$entry" | cut -d: -f4)

  if echo "$EXISTING_IDS" | grep -qx "$task_id"; then
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  PROPS=$(printf '{"id":"%s","track":"%s","status":"%s","created_at":"%s"' \
    "$task_id" "$track" "$task_status" "$(date -u +%Y-%m-%dT%H:%M:%SZ)")
  if [[ -n "$closing_pr" ]]; then
    PROPS+=$(printf ',"closing_prs":"%s"' "$closing_pr")
  fi
  PROPS+="}"

  RESP=$(curl -sSf -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
    "$COORD_URL/nodes" \
    -d "$(printf '{"labels":["Task"],"properties":%s}' "$PROPS")")
  NEW_ID=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
  log "  created Task#${NEW_ID}: ${task_id} [${track}] status=${task_status}"
  CREATED=$((CREATED + 1))
done

# Regenerate the GraphQL schema cache so subsequent { tasks { ... } }
# queries see the new Task label. Required only on the *first* seed run
# when no Task nodes existed yet; subsequent runs are no-ops but the
# regenerate call is harmless and idempotent.
if [[ $CREATED -gt 0 ]]; then
  log "regenerating GraphQL schema cache (admin call required)"
  ADMIN_PW_FILE="${COORD_DATA_DIR:-$HOME/.graphdb-coord-data}/.graphdb_admin_password"
  if [[ -r "$ADMIN_PW_FILE" ]]; then
    ADMIN_PW=$(cat "$ADMIN_PW_FILE")
    ADMIN_JWT=$(curl -sSf -X POST "$COORD_URL/auth/login" \
      -H 'Content-Type: application/json' \
      -d "$(printf '{"username":"admin","password":"%s"}' "$ADMIN_PW")" \
      | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
    curl -sSf -X POST -H "Authorization: Bearer $ADMIN_JWT" \
      "$COORD_URL/api/v1/schema/regenerate" >/dev/null
    log "  schema regenerated"
  else
    log "  WARN: admin password file not found; GraphQL { tasks { ... } } may fail until next regenerate"
  fi
fi

log "done — created=$CREATED, skipped=$SKIPPED"
log "verify with:"
log "  curl -sS -X POST -H 'X-API-Key: \$(cat ~/.graphdb-coord-key)' -H 'Content-Type: application/json' \\"
log "    http://localhost:8090/graphql -d '{\"query\":\"{ tasks { id properties } }\"}'"
