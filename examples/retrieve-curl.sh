#!/usr/bin/env bash
# F2 GraphRAG retrieval — end-to-end example via curl.
#
# Demonstrates seeding a small corpus, indexing it for FTS, and
# calling POST /v1/retrieve to get ranked context chunks with the
# source.path graph signal.
#
# Prerequisites:
#   - graphdb server running on $GRAPHDB_URL (default http://localhost:8080)
#   - $TOKEN exported (e.g. `export TOKEN=$(./scripts/get-token.sh)`)
#   - `jq` for response prettification
#
# Run:
#   chmod +x examples/retrieve-curl.sh
#   ./examples/retrieve-curl.sh

set -euo pipefail

URL="${GRAPHDB_URL:-http://localhost:8080}"
: "${TOKEN:?TOKEN environment variable required (Bearer token from /auth/login)}"

auth=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

echo "==> Seeding corpus on $URL"

# Three Doc nodes: one matches the query, two reachable via traversal.
alice_id=$(curl -sS -X POST "$URL/nodes" "${auth[@]}" -d '{
  "labels": ["Person"],
  "properties": {"name": "Alice", "bio": "graph database researcher"}
}' | jq -r '.id')
echo "  alice  = $alice_id"

paper_id=$(curl -sS -X POST "$URL/nodes" "${auth[@]}" -d '{
  "labels": ["Doc"],
  "properties": {"title": "On Graph Database Internals", "body": "comprehensive review of graph database storage layouts and query execution"}
}' | jq -r '.id')
echo "  paper  = $paper_id"

note_id=$(curl -sS -X POST "$URL/nodes" "${auth[@]}" -d '{
  "labels": ["Doc"],
  "properties": {"title": "Implementation Notes", "body": "design decisions and trade-offs from prototyping the storage engine"}
}' | jq -r '.id')
echo "  note   = $note_id"

# Edges: alice authored the paper; the paper cites the note.
curl -sS -X POST "$URL/edges" "${auth[@]}" -d "{
  \"from_node_id\": $alice_id,
  \"to_node_id\": $paper_id,
  \"type\": \"AUTHORED\",
  \"weight\": 1.0
}" > /dev/null

curl -sS -X POST "$URL/edges" "${auth[@]}" -d "{
  \"from_node_id\": $paper_id,
  \"to_node_id\": $note_id,
  \"type\": \"CITES\",
  \"weight\": 1.0
}" > /dev/null

echo "==> Building FTS index over Doc/Person bodies"

# Index bodies for full-text search. Without this, /v1/retrieve has
# no way to find seed nodes for a free-text query.
curl -sS -X POST "$URL/search/index" "${auth[@]}" -d '{
  "labels": ["Doc", "Person"],
  "properties": ["body", "bio"]
}' > /dev/null

echo "==> Querying /v1/retrieve"
echo

# Query: "graph database research" — should match Alice (bio) and
# the paper (body). The note is reachable via 2-hop traversal:
# alice → paper → note. With max_hops=2 we see all three.
#
# The metadata.source.path on each chunk is the BFS path from
# the contributing seed — graph signal you can't get from pure
# vector RAG.
curl -sS -X POST "$URL/v1/retrieve" "${auth[@]}" -d '{
  "query": "graph database research",
  "k": 10,
  "max_hops": 2,
  "max_tokens": 4096,
  "include_node": false
}' | jq '{
  documents: [.documents[] | {
    score: .metadata.score,
    node_id: .metadata.node_id,
    label: .metadata.source.label,
    path: .metadata.source.path,
    snippet: (.page_content | .[0:120])
  }],
  took_ms: .took_ms,
  degraded: .degraded
}'

echo
echo "==> Done. Path arrays show the BFS chain from a seed to each chunk."
