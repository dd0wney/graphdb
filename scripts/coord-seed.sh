#!/usr/bin/env bash
# coord-seed.sh — seed a coord instance with the current project's
# :Project node + initial :Task set + :IN_PROJECT edges from the
# planning doc. Idempotent: skips :Tasks whose `<project>:<id>` already
# exists.
#
# Pre-requisite: coord-bootstrap.sh has run; ~/.graphdb-coord-key exists.
# Reads GRAPHDB_COORD_URL + GRAPHDB_COORD_TOKEN from env, falling back to
# defaults that match coord-bootstrap.sh's outputs.
#
# COORD_PROJECT is required. Auto-detected from `git remote get-url
# origin` (basename, `.git` stripped). Override via the env var.
#
# For migrating an existing single-project coord (PR #86) to the
# multi-project schema, run scripts/coord-migrate-add-projects.sh first;
# that script renames un-prefixed Task ids and creates :IN_PROJECT edges
# for the existing nodes. This script handles fresh seeds going forward.
#
# Atomicity note (option A — advisory): single-writer at bootstrap time.
# Two operators racing this script could create duplicate :Tasks. Once
# B-lite (`docs/COORD_DEPLOY_SPIKE_2026-05-10.md` PR 1) lands, resolver-
# side uniqueness ends the race; until then, don't run from two machines
# at once.

set -euo pipefail

COORD_URL="${GRAPHDB_COORD_URL:-http://localhost:8090}"
COORD_KEY_FILE="${COORD_KEY_FILE:-$HOME/.graphdb-coord-key}"
COORD_TOKEN="${GRAPHDB_COORD_TOKEN:-$(cat "$COORD_KEY_FILE" 2>/dev/null || true)}"
COORD_DATA_DIR="${COORD_DATA_DIR:-$HOME/.graphdb-coord-data}"

if [[ -z "$COORD_TOKEN" ]]; then
  echo "[coord-seed] ERROR: no API key. Run coord-bootstrap.sh first." >&2
  exit 1
fi

log() { echo "[coord-seed] $*" >&2; }

# Auto-detect COORD_PROJECT from `git remote get-url origin`. Strips the
# trailing `.git` and takes the basename. Handles both
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
[[ -z "$COORD_PROJECT" ]] && { log "ERROR: COORD_PROJECT empty after auto-detect." ; exit 1; }
log "COORD_PROJECT=$COORD_PROJECT"

# Tasks to seed — derived from docs/NEXT_STEPS_2026-05-10.md.
# Format: "id:track:status:closing_pr_or_empty"
# `id` is the project-relative id; the script prefixes it with
# "<COORD_PROJECT>:" before sending. Closed Tasks are seeded so audit-
# history queries work from day one (e.g., "did A4-edges close before
# A8.2?"). Closing PR refs become :CLOSED_BY edges in a future iteration.
#
# Status enum (extended from binary open/done as of 2026-05-10, mirrors
# Taskmaster's taxonomy — see docs/COMPARE_TASKMASTER_2026-05-10.md §7):
#   pending      — not yet started; the default for new Tasks
#   in-progress  — actively being worked on; set by work-claim on Claim creation
#   blocked      — paused waiting on something external (not a DEPENDS_ON Task,
#                  which is implicit; this is for human-resolvable blockers)
#   done         — completed; CLOSED_BY edge typically points at the closing PR
#   deferred     — explicitly punted to a later planning round
#   cancelled    — abandoned without completion (distinct from deferred)
#
# coord-next/coord-clusters filter on `pending` (and respect DEPENDS_ON) when
# recommending the next task. work-claim flips `pending` → `in-progress` on a
# successful Claim and `in-progress` → `done` on release.
TASKS=(
  "H1:H:done:65,66"
  "H3:H:done:"
  "A4:A:done:67"
  "A4-edges:A:done:70"
  "A8.2:A:done:81"
  "F1.1-spike:F:pending:"
  "F1.1-impl:F:pending:"
  "F3:F:pending:"
  "A8.1:A:pending:"
  "H2:H:pending:"
  "H4-PR1-blite:H:done:91"
  "H4-PR2-bootstrap:H:done:86,87"
  "H4-PR3-skill-rewrite:H:done:93"
  "H4-PR4-planning-update:H:done:94"
  "S1:S:pending:"
)

# Pre-fetch nodes once for both the :Project lookup and the existing-Task
# scan. Avoids a second round-trip after the :Project create.
NODES_JSON=$(curl -sSf -H "X-API-Key: $COORD_TOKEN" "$COORD_URL/nodes")

# Step 1: ensure the :Project node exists. Capture its node id for the
# :IN_PROJECT edge writes below.
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

if [[ -z "$PROJECT_NODE_ID" ]]; then
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
  log "created :Project node $PROJECT_NODE_ID with id=$COORD_PROJECT"
else
  log "found existing :Project node $PROJECT_NODE_ID with id=$COORD_PROJECT"
fi

# Step 2: read existing Task ids once. Filter to *this project's* tasks
# (others might exist if multiple projects share this coord) so the
# duplicate check is project-scoped.
log "fetching existing :Task ids for $COORD_PROJECT (idempotency check)"
EXISTING_IDS=$(echo "$NODES_JSON" | python3 -c "
import json, sys, base64
prefix = '$COORD_PROJECT:'
for n in json.load(sys.stdin):
    if 'Task' in n.get('labels', []):
        raw = n['properties'].get('id', '')
        try:
            decoded = base64.b64decode(raw).decode('utf-8')
        except Exception:
            decoded = raw
        if decoded.startswith(prefix):
            print(decoded)
")

# Pre-fetch :IN_PROJECT edges from this project's tasks so we don't
# duplicate the edge on a partial-rerun. REST GET /edges is 405; use
# GraphQL.
EDGES_JSON=$(curl -sSf -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
  "$COORD_URL/graphql" \
  -d '{"query":"{ edges { id type fromNodeId toNodeId } }"}')
EXISTING_IN_PROJECT_FROMS=$(echo "$EDGES_JSON" | python3 -c "
import json, sys
data = json.load(sys.stdin).get('data', {})
target = '$PROJECT_NODE_ID'
for e in data.get('edges', []):
    if e.get('type') == 'IN_PROJECT' and str(e.get('toNodeId')) == target:
        print(e.get('fromNodeId'))
")

CREATED=0
SKIPPED=0
EDGES_CREATED=0
for entry in "${TASKS[@]}"; do
  task_id_raw=$(echo "$entry" | cut -d: -f1)
  track=$(echo "$entry" | cut -d: -f2)
  task_status=$(echo "$entry" | cut -d: -f3)
  closing_pr=$(echo "$entry" | cut -d: -f4)
  task_id="${COORD_PROJECT}:${task_id_raw}"

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

  # Link new Task → Project. Skip if (somehow) already linked — preserves
  # the script's "safe to re-run" guarantee even if a previous partial
  # run created the Task but failed before the edge.
  if ! echo "$EXISTING_IN_PROJECT_FROMS" | grep -qx "$NEW_ID"; then
    curl -sSf -X POST -H "X-API-Key: $COORD_TOKEN" -H 'Content-Type: application/json' \
      "$COORD_URL/edges" \
      -d "$(printf '{"type":"IN_PROJECT","from_node_id":%s,"to_node_id":%s}' "$NEW_ID" "$PROJECT_NODE_ID")" >/dev/null
    EDGES_CREATED=$((EDGES_CREATED + 1))
  fi
done

# Regenerate the GraphQL schema cache so subsequent { tasks { ... } }
# and { projects { ... } } queries see the new labels. Required only on
# the *first* seed run when no Task nodes existed yet; subsequent runs
# are no-ops but the regenerate call is harmless and idempotent.
if [[ $CREATED -gt 0 ]]; then
  log "regenerating GraphQL schema cache (admin call required)"
  ADMIN_PW_FILE="${COORD_DATA_DIR}/.graphdb_admin_password"
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

log "done — created=$CREATED skipped=$SKIPPED edges=$EDGES_CREATED"
log "verify with:"
log "  curl -sS -X POST -H 'X-API-Key: \$(cat ~/.graphdb-coord-key)' -H 'Content-Type: application/json' \\"
log "    http://localhost:8090/graphql -d '{\"query\":\"{ projects { id properties } tasks { id properties } }\"}'"
