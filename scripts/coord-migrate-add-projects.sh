#!/usr/bin/env bash
# coord-migrate-add-projects.sh — one-shot migration from single-project
# coord (PR #86) to the multi-project schema (Option C, MULTI_PROJECT_SPIKE_2026-05-10).
#
# What changes:
#   - Adds a :Project node for the current repo (auto-detected from
#     `git remote get-url origin`, override via COORD_PROJECT env var).
#   - Prefixes every existing un-prefixed :Task id with `<project>:`.
#   - Creates one :IN_PROJECT edge per :Task → :Project.
#   - Renames the existing :Claim's `for_task` to match the prefixed id.
#   - Regenerates the GraphQL schema cache.
#
# Idempotent: each step independently checks whether it's already done.
# Safe to re-run; matches the seed script's pattern.
#
# Conflict guard: if a :Project node already exists with an id different
# from the auto-detected COORD_PROJECT, the script refuses to proceed.
# This catches "wrong directory" mistakes before they corrupt the graph.
# Recovery: explicitly set COORD_PROJECT=<existing-id> or wipe the data
# dir.

set -euo pipefail

COORD_URL="${GRAPHDB_COORD_URL:-http://localhost:8090}"
COORD_KEY_FILE="${COORD_KEY_FILE:-$HOME/.graphdb-coord-key}"
COORD_TOKEN="${GRAPHDB_COORD_TOKEN:-$(cat "$COORD_KEY_FILE" 2>/dev/null || true)}"
COORD_DATA_DIR="${COORD_DATA_DIR:-$HOME/.graphdb-coord-data}"

if [[ -z "$COORD_TOKEN" ]]; then
  echo "[coord-migrate] ERROR: no API key. Run coord-bootstrap.sh first." >&2
  exit 1
fi

log() { echo "[coord-migrate] $*" >&2; }

# Auto-detect COORD_PROJECT from `git remote get-url origin`, basename
# style: strips trailing `.git` and the host/path. Handles both
# https://github.com/dd0wney/graphdb and git@github.com:dd0wney/graphdb.git.
detect_project() {
  local url
  url=$(git remote get-url origin 2>/dev/null) || return 1
  echo "${url##*/}" | sed 's/\.git$//'
}

if [[ -z "${COORD_PROJECT:-}" ]]; then
  COORD_PROJECT=$(detect_project) || {
    log "ERROR: COORD_PROJECT not set and 'git remote get-url origin' failed."
    log "  Run from inside a git repo, or set COORD_PROJECT=<slug> explicitly."
    exit 1
  }
fi

if [[ -z "$COORD_PROJECT" ]]; then
  log "ERROR: COORD_PROJECT auto-detect produced an empty string."
  exit 1
fi

log "COORD_PROJECT=$COORD_PROJECT"

# decode_b64 — strip the REST /nodes GET base64-encoding (H4.1 bug).
# Used everywhere we read string properties via REST.
decode_b64() {
  python3 -c '
import sys, base64
for line in sys.stdin:
    s = line.rstrip()
    try:
        sys.stdout.write(base64.b64decode(s).decode("utf-8") + "\n")
    except Exception:
        sys.stdout.write(s + "\n")
'
}

# Fetch the full node list once. Used for label/property scans below;
# avoids hammering the API. NODES_JSON is reused across helpers.
NODES_JSON=$(curl -sSf -H "X-API-Key: $COORD_TOKEN" "$COORD_URL/nodes")

# existing_project_ids — emit decoded `id` properties of every :Project
# node currently in the graph, one per line.
existing_project_ids() {
  echo "$NODES_JSON" | python3 -c '
import json, sys, base64
for n in json.load(sys.stdin):
    if "Project" in n.get("labels", []):
        raw = n["properties"].get("id", "")
        try:
            print(base64.b64decode(raw).decode("utf-8"))
        except Exception:
            print(raw)
'
}

# Conflict guard: if a :Project node already exists with an id different
# from COORD_PROJECT, refuse. The operator either ran from the wrong
# directory or the repo was renamed; either way, silent multi-project
# creation is worse than failing loudly.
EXISTING_PROJECTS=$(existing_project_ids)
if [[ -n "$EXISTING_PROJECTS" ]]; then
  if ! echo "$EXISTING_PROJECTS" | grep -qx "$COORD_PROJECT"; then
    log "ERROR: detected COORD_PROJECT=$COORD_PROJECT"
    log "  but coord already has :Project node(s) with id(s):"
    while IFS= read -r p; do log "    - $p"; done <<< "$EXISTING_PROJECTS"
    log "  Set COORD_PROJECT=<existing-id> explicitly to acknowledge,"
    log "  or see § Daemon lifecycle in docs/COORD_SETUP.md for the full reset sequence."
    exit 1
  fi
  log "found existing :Project { id=$COORD_PROJECT } — will reuse"
fi

# Step 1: ensure the :Project node exists. If not, create it with the
# repo URL and a minimal description (operator can edit later via PUT).
PROJECT_NODE_ID=""
if echo "$EXISTING_PROJECTS" | grep -qx "$COORD_PROJECT"; then
  PROJECT_NODE_ID=$(echo "$NODES_JSON" | python3 -c "
import json, sys, base64
target = '$COORD_PROJECT'
for n in json.load(sys.stdin):
    if 'Project' in n.get('labels', []):
        raw = n['properties'].get('id', '')
        try:
            decoded = base64.b64decode(raw).decode('utf-8')
        except Exception:
            decoded = raw
        if decoded == target:
            print(n['id'])
            break
")
  log "step 1/4: :Project { id=$COORD_PROJECT } already exists as node $PROJECT_NODE_ID"
else
  REPO_URL=$(git remote get-url origin 2>/dev/null | sed 's/\.git$//' || echo "")
  PROPS=$(printf '{"id":"%s","name":"%s"' "$COORD_PROJECT" "$COORD_PROJECT")
  if [[ -n "$REPO_URL" ]]; then
    PROPS+=$(printf ',"repo_url":"%s"' "$REPO_URL")
  fi
  PROPS+="}"
  RESP=$(curl -sSf -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
    "$COORD_URL/nodes" \
    -d "$(printf '{"labels":["Project"],"properties":%s}' "$PROPS")")
  PROJECT_NODE_ID=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")
  log "step 1/4: created :Project node $PROJECT_NODE_ID with id=$COORD_PROJECT"
fi

# Step 2: prefix un-prefixed :Task ids in-place. PUT /nodes/{id} merges
# properties (verified pkg/storage/node_operations.go:236-238) so we
# only update `id` and other props are preserved.
TASKS_RENAMED=0
TASKS_SKIPPED=0
TASK_ROWS=$(echo "$NODES_JSON" | python3 -c '
import json, sys, base64
for n in json.load(sys.stdin):
    if "Task" in n.get("labels", []):
        raw = n["properties"].get("id", "")
        try:
            decoded = base64.b64decode(raw).decode("utf-8")
        except Exception:
            decoded = raw
        print("%s\t%s" % (n["id"], decoded))
')

while IFS=$'\t' read -r node_id current_id; do
  [[ -z "$node_id" ]] && continue
  # Skip any Task already prefixed (by *any* project). Migration only
  # touches the legacy un-prefixed Tasks left by single-project coord
  # (PR #86); other projects' Tasks must not be re-namespaced.
  if [[ "$current_id" == *":"* ]]; then
    TASKS_SKIPPED=$((TASKS_SKIPPED + 1))
    continue
  fi
  new_id="${COORD_PROJECT}:${current_id}"
  curl -sSf -X PUT -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
    "$COORD_URL/nodes/$node_id" \
    -d "$(printf '{"properties":{"id":"%s"}}' "$new_id")" >/dev/null
  TASKS_RENAMED=$((TASKS_RENAMED + 1))
done <<< "$TASK_ROWS"

log "step 2/4: prefixed $TASKS_RENAMED Task id(s); $TASKS_SKIPPED already prefixed"

# Step 3: create :IN_PROJECT edges Task → Project for any Task that
# doesn't already have one. Re-query edges via GraphQL (REST GET /edges
# is 405) to know which links exist.
EDGES_JSON=$(curl -sSf -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
  "$COORD_URL/graphql" \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}')
EXISTING_IN_PROJECT=$(echo "$EDGES_JSON" | python3 -c "
import json, sys
data = json.load(sys.stdin).get('data', {})
target_project = '$PROJECT_NODE_ID'
for e in data.get('edges', []):
    if e.get('type') == 'IN_PROJECT' and str(e.get('toNodeId')) == target_project:
        print(e.get('fromNodeId'))
")

EDGES_CREATED=0
EDGES_SKIPPED=0
while IFS=$'\t' read -r node_id current_id; do
  [[ -z "$node_id" ]] && continue
  # Only link Tasks that belong to COORD_PROJECT. After step 2:
  #   - rows with un-prefixed `current_id` were just prefixed → ours.
  #   - rows with `current_id` already starting with "<other>:" → not ours.
  #   - rows with `current_id` already starting with "$COORD_PROJECT:" → ours.
  if [[ "$current_id" == *":"* && "$current_id" != "$COORD_PROJECT:"* ]]; then
    continue
  fi
  if echo "$EXISTING_IN_PROJECT" | grep -qx "$node_id"; then
    EDGES_SKIPPED=$((EDGES_SKIPPED + 1))
    continue
  fi
  curl -sSf -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
    "$COORD_URL/edges" \
    -d "$(printf '{"type":"IN_PROJECT","from_node_id":%s,"to_node_id":%s}' "$node_id" "$PROJECT_NODE_ID")" >/dev/null
  EDGES_CREATED=$((EDGES_CREATED + 1))
done <<< "$TASK_ROWS"

log "step 3/4: created $EDGES_CREATED :IN_PROJECT edge(s); $EDGES_SKIPPED already linked"

# Step 4: rename existing :Claim nodes' for_task property to match the
# prefixed Task ids. Same merge-PUT trick.
CLAIMS_RENAMED=0
CLAIMS_SKIPPED=0
CLAIM_ROWS=$(echo "$NODES_JSON" | python3 -c '
import json, sys, base64
for n in json.load(sys.stdin):
    if "Claim" in n.get("labels", []):
        raw = n["properties"].get("for_task", "")
        try:
            decoded = base64.b64decode(raw).decode("utf-8")
        except Exception:
            decoded = raw
        print("%s\t%s" % (n["id"], decoded))
')

while IFS=$'\t' read -r node_id current_for_task; do
  [[ -z "$node_id" ]] && continue
  if [[ "$current_for_task" == *":"* ]]; then
    CLAIMS_SKIPPED=$((CLAIMS_SKIPPED + 1))
    continue
  fi
  new_for_task="${COORD_PROJECT}:${current_for_task}"
  curl -sSf -X PUT -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
    "$COORD_URL/nodes/$node_id" \
    -d "$(printf '{"properties":{"for_task":"%s"}}' "$new_for_task")" >/dev/null
  CLAIMS_RENAMED=$((CLAIMS_RENAMED + 1))
done <<< "$CLAIM_ROWS"

log "step 4/4: renamed $CLAIMS_RENAMED Claim.for_task value(s); $CLAIMS_SKIPPED already prefixed"

# Schema regenerate so { projects { ... } } and {project} field on Task
# (when GraphQL gets mutations) become reachable. Same admin-JWT pattern
# as coord-seed.sh.
ADMIN_PW_FILE="$COORD_DATA_DIR/.graphdb_admin_password"
if [[ -r "$ADMIN_PW_FILE" ]]; then
  ADMIN_PW=$(cat "$ADMIN_PW_FILE")
  ADMIN_JWT=$(curl -sSf -X POST "$COORD_URL/auth/login" \
    -H 'Content-Type: application/json' \
    -d "$(printf '{"username":"admin","password":"%s"}' "$ADMIN_PW")" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
  curl -sSf -X POST -H "Authorization: Bearer $ADMIN_JWT" \
    "$COORD_URL/api/v1/schema/regenerate" >/dev/null
  log "schema regenerated"
else
  log "WARN: admin password file not found at $ADMIN_PW_FILE"
  log "  GraphQL { projects { ... } } may fail until next regenerate."
fi

log "done — Tasks renamed=$TASKS_RENAMED skipped=$TASKS_SKIPPED, "\
"Edges created=$EDGES_CREATED skipped=$EDGES_SKIPPED, "\
"Claims renamed=$CLAIMS_RENAMED skipped=$CLAIMS_SKIPPED"
log "verify with:"
log "  curl -sS -X POST -H 'X-API-Key: \$(cat ~/.graphdb-coord-key)' -H 'Content-Type: application/json' \\"
log "    http://localhost:8090/graphql -d '{\"query\":\"{ projects { id properties } tasks { id properties } edges { id type fromNodeId toNodeId } }\"}'"
