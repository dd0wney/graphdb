# Cluso GraphDB Integration Guide

How to use your custom Go graph database as a backend for Cluso trust scoring.

---

## Architecture Overview

```
Cloudflare Edge (300+ locations)
    ├─> KV Cache (10-50ms) ──────────┐
    ├─> Durable Objects (50-100ms) ──┤ 95% of queries
    └─> D1 Cache (100-150ms) ────────┘ served from cache
                ↓
        [5% cache misses]
                ↓
    Digital Ocean Droplet (NYC)
    ┌────────────────────────────┐
    │  Cluso GraphDB (Go)        │
    │  - Primary (read/write)    │ ← 50-500ms
    │  - Replica (SF) - optional │
    └────────────────────────────┘
```

---

## Step 1: Deploy GraphDB to Digital Ocean

### Build for Linux

```bash
cd /Users/darraghdowney/Workspace/github.com/cluso-graphdb

# Build for Linux AMD64
GOOS=linux GOARCH=amd64 go build -o graphdb-linux ./cmd/graphdb

# Or use a Makefile
cat > Makefile << 'EOF'
.PHONY: build-linux
build-linux:
	GOOS=linux GOARCH=amd64 go build -o bin/graphdb-linux ./cmd/graphdb

.PHONY: deploy
deploy: build-linux
	scp bin/graphdb-linux root@$(DROPLET_IP):/usr/local/bin/graphdb
	ssh root@$(DROPLET_IP) systemctl restart graphdb
EOF

make build-linux
```

### Create Droplet

```bash
# Create 8GB RAM droplet
doctl compute droplet create cluso-graphdb-primary \
  --size s-4vcpu-8gb \
  --image ubuntu-22-04-x64 \
  --region nyc3 \
  --ssh-keys $(doctl compute ssh-key list --format ID --no-header) \
  --tag-names production,graphdb,primary \
  --enable-monitoring \
  --enable-backups

# Get IP
DROPLET_IP=$(doctl compute droplet get cluso-graphdb-primary --format PublicIPv4 --no-header)
echo "Droplet IP: $DROPLET_IP"
```

### Deploy Binary

```bash
# Copy binary
scp graphdb-linux root@$DROPLET_IP:/usr/local/bin/graphdb
ssh root@$DROPLET_IP chmod +x /usr/local/bin/graphdb

# Create data directory
ssh root@$DROPLET_IP mkdir -p /var/lib/graphdb

# Create systemd service
ssh root@$DROPLET_IP << 'EOF'
cat > /etc/systemd/system/graphdb.service << 'SERVICE'
[Unit]
Description=Cluso GraphDB
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/var/lib/graphdb
ExecStart=/usr/local/bin/graphdb
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
SERVICE

systemctl daemon-reload
systemctl enable graphdb
systemctl start graphdb
systemctl status graphdb
EOF
```

---

## Step 2: Add HTTP/REST API

Currently, the GraphDB only has a Go API. Let's add a REST API for Cloudflare Workers to call.

### Create REST Server

```go
// cmd/graphdb-server/main.go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/darraghdowney/cluso-graphdb/pkg/query"
	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
	"github.com/gorilla/mux"
)

type Server struct {
	graph     *storage.GraphStorage
	traverser *query.Traverser
}

func main() {
	// Initialize graph
	graph, err := storage.NewGraphStorage("/var/lib/graphdb")
	if err != nil {
		log.Fatalf("Failed to create storage: %v", err)
	}
	defer graph.Close()

	server := &Server{
		graph:     graph,
		traverser: query.NewTraverser(graph),
	}

	router := mux.NewRouter()

	// Node endpoints
	router.HandleFunc("/nodes", server.createNode).Methods("POST")
	router.HandleFunc("/nodes/{id}", server.getNode).Methods("GET")
	router.HandleFunc("/nodes", server.findNodes).Methods("GET")

	// Edge endpoints
	router.HandleFunc("/edges", server.createEdge).Methods("POST")
	router.HandleFunc("/nodes/{id}/edges/outgoing", server.getOutgoingEdges).Methods("GET")

	// Query endpoints
	router.HandleFunc("/traverse", server.traverse).Methods("POST")
	router.HandleFunc("/path", server.findPath).Methods("POST")

	// Health check
	router.HandleFunc("/health", server.health).Methods("GET")

	log.Println("🚀 Cluso GraphDB Server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

// createNode creates a new node
func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Labels     []string               `json:"labels"`
		Properties map[string]interface{} `json:"properties"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Convert properties
	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
		props[k] = convertToValue(v)
	}

	node, err := s.graph.CreateNode(req.Labels, props)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     node.ID,
		"labels": node.Labels,
	})
}

// getNode retrieves a node by ID
func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	node, err := s.graph.GetNode(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(nodeToJSON(node))
}

// findNodes finds nodes by label or property
func (s *Server) findNodes(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")

	if label != "" {
		nodes, err := s.graph.FindNodesByLabel(label)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		result := make([]interface{}, len(nodes))
		for i, node := range nodes {
			result[i] = nodeToJSON(node)
		}

		json.NewEncoder(w).Encode(result)
	} else {
		http.Error(w, "label parameter required", http.StatusBadRequest)
	}
}

// createEdge creates a new edge
func (s *Server) createEdge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromNodeID uint64                 `json:"from_node_id"`
		ToNodeID   uint64                 `json:"to_node_id"`
		Type       string                 `json:"type"`
		Properties map[string]interface{} `json:"properties"`
		Weight     float64                `json:"weight"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	props := make(map[string]storage.Value)
	for k, v := range req.Properties {
		props[k] = convertToValue(v)
	}

	edge, err := s.graph.CreateEdge(req.FromNodeID, req.ToNodeID, req.Type, props, req.Weight)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           edge.ID,
		"from_node_id": edge.FromNodeID,
		"to_node_id":   edge.ToNodeID,
		"type":         edge.Type,
	})
}

// getOutgoingEdges gets outgoing edges from a node
func (s *Server) getOutgoingEdges(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id, err := strconv.ParseUint(vars["id"], 10, 64)
	if err != nil {
		http.Error(w, "Invalid node ID", http.StatusBadRequest)
		return
	}

	edges, err := s.graph.GetOutgoingEdges(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := make([]interface{}, len(edges))
	for i, edge := range edges {
		result[i] = edgeToJSON(edge)
	}

	json.NewEncoder(w).Encode(result)
}

// traverse performs graph traversal
func (s *Server) traverse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartNodeID uint64   `json:"start_node_id"`
		Direction   string   `json:"direction"` // "outgoing", "incoming", "both"
		EdgeTypes   []string `json:"edge_types"`
		MaxDepth    int      `json:"max_depth"`
		MaxResults  int      `json:"max_results"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	direction := query.DirectionOutgoing
	switch req.Direction {
	case "incoming":
		direction = query.DirectionIncoming
	case "both":
		direction = query.DirectionBoth
	}

	result, err := s.traverser.BFS(query.TraversalOptions{
		StartNodeID: req.StartNodeID,
		Direction:   direction,
		EdgeTypes:   req.EdgeTypes,
		MaxDepth:    req.MaxDepth,
		MaxResults:  req.MaxResults,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	nodes := make([]interface{}, len(result.Nodes))
	for i, node := range result.Nodes {
		nodes[i] = nodeToJSON(node)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

// findPath finds shortest path between two nodes
func (s *Server) findPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FromNodeID uint64   `json:"from_node_id"`
		ToNodeID   uint64   `json:"to_node_id"`
		EdgeTypes  []string `json:"edge_types"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	path, err := s.traverser.FindShortestPath(req.FromNodeID, req.ToNodeID, req.EdgeTypes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	nodes := make([]interface{}, len(path.Nodes))
	for i, node := range path.Nodes {
		nodes[i] = nodeToJSON(node)
	}

	edges := make([]interface{}, len(path.Edges))
	for i, edge := range path.Edges {
		edges[i] = edgeToJSON(edge)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"edges": edges,
	})
}

// health check
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	stats := s.graph.GetStatistics()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "healthy",
		"node_count":  stats.NodeCount,
		"edge_count":  stats.EdgeCount,
	})
}

// Helper functions
func convertToValue(v interface{}) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case float64:
		return storage.IntValue(int64(val))
	case bool:
		return storage.BoolValue(val)
	default:
		return storage.StringValue(fmt.Sprint(v))
	}
}

func nodeToJSON(node *storage.Node) map[string]interface{} {
	props := make(map[string]interface{})
	for k, v := range node.Properties {
		props[k] = valueToInterface(v)
	}

	return map[string]interface{}{
		"id":         node.ID,
		"labels":     node.Labels,
		"properties": props,
		"created_at": node.CreatedAt,
		"updated_at": node.UpdatedAt,
	}
}

func edgeToJSON(edge *storage.Edge) map[string]interface{} {
	props := make(map[string]interface{})
	for k, v := range edge.Properties {
		props[k] = valueToInterface(v)
	}

	return map[string]interface{}{
		"id":           edge.ID,
		"from_node_id": edge.FromNodeID,
		"to_node_id":   edge.ToNodeID,
		"type":         edge.Type,
		"properties":   props,
		"weight":       edge.Weight,
		"created_at":   edge.CreatedAt,
	}
}

func valueToInterface(v storage.Value) interface{} {
	switch v.Type {
	case storage.TypeString:
		s, _ := v.AsString()
		return s
	case storage.TypeInt:
		i, _ := v.AsInt()
		return i
	case storage.TypeFloat:
		f, _ := v.AsFloat()
		return f
	case storage.TypeBool:
		b, _ := v.AsBool()
		return b
	case storage.TypeTimestamp:
		t, _ := v.AsTimestamp()
		return t.Unix()
	default:
		return nil
	}
}
```

### Install Dependencies

```bash
go get github.com/gorilla/mux
```

---

## Step 3: Update Cloudflare Worker Client

```typescript
// cluso/src/clients/graphdb.ts
export class GraphDBClient {
  private baseURL: string;

  constructor(baseURL: string) {
    this.baseURL = baseURL; // e.g., "http://your-droplet-ip:8080"
  }

  /**
   * Query with cache layer
   */
  async query(cacheKey: string, queryFn: () => Promise<any>, cacheTTL: number = 3600) {
    // Try KV cache first
    const cached = await env.TRUST_CACHE.get(cacheKey, 'json');
    if (cached) {
      console.log(`✅ Cache HIT: ${cacheKey}`);
      return cached;
    }

    // Cache miss - query GraphDB
    const result = await queryFn();

    // Cache result
    await env.TRUST_CACHE.put(cacheKey, JSON.stringify(result), {
      expirationTtl: cacheTTL,
    });

    return result;
  }

  /**
   * Find fraud ring (example query)
   */
  async findFraudRing(userId: string): Promise<string[]> {
    const cacheKey = `fraud-ring:${userId}`;

    return this.query(cacheKey, async () => {
      // Traverse 2 hops to find similar users
      const response = await fetch(`${this.baseURL}/traverse`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          start_node_id: parseInt(userId),
          direction: 'both',
          edge_types: ['SIMILAR_BEHAVIOR'],
          max_depth: 2,
          max_results: 100,
        }),
      });

      const data = await response.json();
      return data.nodes
        .filter((n: any) => n.properties.fraudScore > 80)
        .map((n: any) => n.properties.id);
    }, 86400); // Cache for 24 hours
  }

  /**
   * Get trust network
   */
  async getTrustNetwork(userId: string): Promise<any> {
    const cacheKey = `trust-network:${userId}`;

    return this.query(cacheKey, async () => {
      const response = await fetch(`${this.baseURL}/traverse`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          start_node_id: parseInt(userId),
          direction: 'outgoing',
          edge_types: ['VERIFIED_BY'],
          max_depth: 2,
          max_results: 50,
        }),
      });

      return await response.json();
    }, 3600); // Cache for 1 hour
  }
}
```

---

## Step 4: Use in Cluso API

```typescript
// cluso/src/index.ts
import { GraphDBClient } from './clients/graphdb';

// Initialize client
const graphDB = new GraphDBClient(env.GRAPHDB_URL);

// Fraud detection endpoint
app.post('/api/v1/fraud/analyze', async (c) => {
  const { userId } = await c.req.json();

  // Check if user is in fraud ring (cached!)
  const fraudRing = await graphDB.findFraudRing(userId);

  return c.json({
    userId,
    inFraudRing: fraudRing.length > 3,
    suspiciousConnections: fraudRing.length,
    fraudRingMembers: fraudRing,
  });
});
```

---

## Performance Expectations

### Without Cache (Direct GraphDB query)
```
Simple traversal (1-hop):  50-100ms
Medium traversal (2-hop):  150-300ms
Complex traversal (3-hop): 400-800ms
```

### With Cloudflare Cache (KV hit)
```
All queries: 10-50ms ⚡
```

**Result: 5-20x speedup!**

---

## Monitoring

### Check GraphDB Health

```bash
curl http://your-droplet-ip:8080/health
```

### Monitor Logs

```bash
ssh root@droplet-ip journalctl -u graphdb -f
```

### Performance Metrics

Add to `storage.go`:
```go
func (gs *GraphStorage) RecordQueryTime(duration time.Duration) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	// Update running average
	gs.stats.TotalQueries++
	gs.stats.AvgQueryTime = (gs.stats.AvgQueryTime*float64(gs.stats.TotalQueries-1) + duration.Seconds()) / float64(gs.stats.TotalQueries)
}
```

---

## Next Steps

1. ✅ Deploy GraphDB to Digital Ocean
2. ✅ Add REST API server
3. ✅ Update Cloudflare Worker client
4. ✅ Integrate with Cluso fraud detection
5. ⏳ Add Write-Ahead Log (durability)
6. ⏳ Implement replication (high availability)
7. ⏳ Add authentication (API keys)

---

**You now have a complete custom graph database backend for Cluso!** 🎉
