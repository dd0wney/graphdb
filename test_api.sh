#!/bin/bash
# API Integration Test Script

BASE_URL="http://localhost:8080"

echo "🧪 Cluso GraphDB API Test Suite"
echo "================================"

# Test 1: Create nodes
echo ""
echo "📝 Test 1: Create User Nodes"
NODE1=$(curl -s -X POST $BASE_URL/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["User"], "properties": {"id": "alice", "trustScore": 850}}')
echo "Created node: $NODE1"

NODE2=$(curl -s -X POST $BASE_URL/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["User"], "properties": {"id": "bob", "trustScore": 750}}')
echo "Created node: $NODE2"

NODE3=$(curl -s -X POST $BASE_URL/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["User"], "properties": {"id": "charlie", "trustScore": 920}}')
echo "Created node: $NODE3"

# Test 2: Create edges
echo ""
echo "🔗 Test 2: Create Edges"
EDGE1=$(curl -s -X POST $BASE_URL/edges \
  -H "Content-Type: application/json" \
  -d '{"from_node_id": 1, "to_node_id": 2, "type": "VERIFIED_BY", "properties": {}, "weight": 1.0}')
echo "Created edge: $EDGE1"

EDGE2=$(curl -s -X POST $BASE_URL/edges \
  -H "Content-Type: application/json" \
  -d '{"from_node_id": 2, "to_node_id": 3, "type": "VERIFIED_BY", "properties": {}, "weight": 1.0}')
echo "Created edge: $EDGE2"

# Test 3: Get node
echo ""
echo "📖 Test 3: Get Node by ID"
curl -s $BASE_URL/nodes/1 | jq .

# Test 4: Find nodes by label
echo ""
echo "🔍 Test 4: Find Nodes by Label"
curl -s "$BASE_URL/nodes?label=User" | jq 'length'
echo " User nodes found"

# Test 5: Traverse graph
echo ""
echo "🌐 Test 5: Graph Traversal (BFS)"
curl -s -X POST $BASE_URL/traverse \
  -H "Content-Type: application/json" \
  -d '{"start_node_id": 1, "direction": "outgoing", "edge_types": ["VERIFIED_BY"], "max_depth": 2, "max_results": 100}' \
  | jq '{count: .count, node_ids: [.nodes[].id]}'

# Test 6: Find shortest path
echo ""
echo "🛤️  Test 6: Shortest Path (Alice → Charlie)"
curl -s -X POST $BASE_URL/path/shortest \
  -H "Content-Type: application/json" \
  -d '{"from_node_id": 1, "to_node_id": 3, "edge_types": []}' \
  | jq '{length: .length, path: [.nodes[].properties.id]}'

# Test 7: Get statistics
echo ""
echo "📊 Test 7: Database Statistics"
curl -s $BASE_URL/stats | jq .

# Test 8: Snapshot
echo ""
echo "💾 Test 8: Create Snapshot"
curl -s -X POST $BASE_URL/snapshot | jq .

echo ""
echo "✅ All tests completed!"
