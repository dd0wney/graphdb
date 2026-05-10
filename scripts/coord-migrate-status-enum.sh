#!/usr/bin/env bash
# coord-migrate-status-enum.sh — one-time migration to move existing
# :Task.status values from the binary {open, done} taxonomy to the
# Taskmaster-aligned {pending, in-progress, blocked, done, deferred,
# cancelled} taxonomy.
#
# Idempotent: safe to re-run. Only touches Tasks whose status is
# literally "open"; everything else is left as-is.
#
# Pre-requisite: scripts/coord-bootstrap.sh has run; ~/.graphdb-coord-key
# exists.
#
# Designed for the H4 status-enum-richening PR (chore/coord-skill-
# enhancements-2026-05-10). After this runs, coord-next/coord-clusters
# can rely on `pending` filtering working end-to-end on existing Tasks.
#
# Per-task explicit overrides:
#   - Tasks whose `id` matches an entry in PROMOTE_TO_DONE are flipped
#     to `done` regardless of current status. Use sparingly — this is
#     for backfilling work that closed before the migration ran. Each
#     entry must include the closing PR list.
#
# Anything else with status=open becomes status=pending.

set -euo pipefail

COORD_URL="${GRAPHDB_COORD_URL:-http://localhost:8090}"
COORD_KEY_FILE="${COORD_KEY_FILE:-$HOME/.graphdb-coord-key}"
COORD_TOKEN="${GRAPHDB_COORD_TOKEN:-$(cat "$COORD_KEY_FILE" 2>/dev/null || true)}"

if [[ -z "$COORD_TOKEN" ]]; then
  echo "[migrate-status] ERROR: no API key. Run coord-bootstrap.sh first." >&2
  exit 1
fi

log() { echo "[migrate-status] $*" >&2; }

# Backfill: tasks that closed before this migration ran. Format:
# "<project-prefixed-id>:<comma-separated-PR-numbers>". Update this list
# whenever new work closes that wasn't recorded as done at seed time.
PROMOTE_TO_DONE=(
  "graphdb:H4-PR1-blite:91"
  "graphdb:H4-PR2-bootstrap:86,87"
  "graphdb:H4-PR3-skill-rewrite:93"
  "graphdb:H4-PR4-planning-update:94"
)

# Pull all nodes once.
NODES_JSON=$(curl -fsS -H "X-API-Key: $COORD_TOKEN" "$COORD_URL/nodes")

# Build the work plan in Python (the base64 decoding makes pure-bash
# painful). Output one line per Task that needs an update:
#   "<node-id> <new-status> [<closing-prs>]"
WORK_PLAN=$(echo "$NODES_JSON" | PROMOTE="${PROMOTE_TO_DONE[*]}" python3 -c "
import json, sys, os, base64

def decode(v):
    try: return base64.b64decode(v).decode('utf-8')
    except Exception: return v

# Parse the promote list: 'id:prs id2:prs2 ...' (space-separated entries)
promote = {}
for entry in os.environ.get('PROMOTE', '').split():
    # An entry is 'graphdb:H4-PR1-blite:91' — split on the LAST colon.
    parts = entry.rsplit(':', 1)
    if len(parts) == 2:
        promote[parts[0]] = parts[1]

for n in json.load(sys.stdin):
    if 'Task' not in n.get('labels', []):
        continue
    tid = decode(n['properties'].get('id', ''))
    status = decode(n['properties'].get('status', ''))

    if tid in promote:
        if status != 'done':
            print(f\"{n['id']} done {promote[tid]}\")
    elif status == 'open':
        print(f\"{n['id']} pending\")
")

if [[ -z "$WORK_PLAN" ]]; then
  log "no migrations needed — all Tasks already on the new enum"
  exit 0
fi

UPDATES=0
while IFS= read -r line; do
  NODE_ID=$(echo "$line" | awk '{print $1}')
  NEW_STATUS=$(echo "$line" | awk '{print $2}')
  CLOSING_PRS=$(echo "$line" | awk '{print $3}')

  PAYLOAD=$(NODE_ID="$NODE_ID" NEW_STATUS="$NEW_STATUS" CLOSING_PRS="$CLOSING_PRS" python3 -c "
import json, os
props = {'status': os.environ['NEW_STATUS']}
if os.environ.get('CLOSING_PRS'):
    props['closing_prs'] = os.environ['CLOSING_PRS']
query = 'mutation { updateNode(id: \"' + os.environ['NODE_ID'] + '\", properties: ' + json.dumps(json.dumps(props)) + ') { id } }'
print(json.dumps({'query': query}))
")
  RESP=$(curl -sS -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
    "$COORD_URL/graphql" -d "$PAYLOAD")

  if echo "$RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get('data',{}).get('updateNode') else 1)" 2>/dev/null; then
    log "  Task#${NODE_ID} → status=${NEW_STATUS}${CLOSING_PRS:+ closing_prs=${CLOSING_PRS}}"
    UPDATES=$((UPDATES + 1))
  else
    log "  Task#${NODE_ID} update FAILED: $RESP"
    exit 1
  fi
done <<< "$WORK_PLAN"

log "applied ${UPDATES} status updates"
log "verify with:"
log "  curl -sS -H 'X-API-Key: \$GRAPHDB_COORD_TOKEN' \$GRAPHDB_COORD_URL/nodes | jq '.[] | select(.labels[]==\"Task\") | .properties'"
