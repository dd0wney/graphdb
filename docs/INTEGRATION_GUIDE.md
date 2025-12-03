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

## Step 2: Use the Built-in REST API

GraphDB includes a production-ready HTTP API server in `pkg/api/server.go` using Go's standard library.

### Available Endpoints

The production API server provides:

**Node Operations:**

- `POST /nodes` - Create a new node
- `GET /nodes/{id}` - Get a node by ID
- `PUT /nodes/{id}` - Update a node
- `DELETE /nodes/{id}` - Delete a node
- `GET /nodes?label={label}` - Find nodes by label

**Edge Operations:**

- `POST /edges` - Create a new edge
- `GET /edges/{id}` - Get an edge by ID
- `DELETE /edges/{id}` - Delete an edge

**Query Operations:**

- `POST /query` - Execute a graph query
- `POST /traverse` - Perform graph traversal

**Health & Monitoring:**

- `GET /health` - Health check endpoint
- `GET /metrics` - Prometheus metrics

### Running the Server

```bash
# Build and run
go build -o graphdb ./cmd/graphdb
./graphdb --port 8080 --data-dir /var/lib/graphdb
```

### Example API Calls

```bash
# Create a node
curl -X POST http://localhost:8080/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["User"], "properties": {"name": "Alice", "fraudScore": 15}}'

# Get a node
curl http://localhost:8080/nodes/1

# Create an edge
curl -X POST http://localhost:8080/edges \
  -H "Content-Type: application/json" \
  -d '{"from_node_id": 1, "to_node_id": 2, "type": "VERIFIED_BY", "weight": 1.0}'

# Traverse the graph
curl -X POST http://localhost:8080/traverse \
  -H "Content-Type: application/json" \
  -d '{"start_node_id": 1, "direction": "outgoing", "max_depth": 2}'
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

**You now have a complete custom graph database backend for Cluso!**

---

## Syntopica Learning Platform Schema

This section describes how to implement a graph schema for a learning platform with fraud detection capabilities. The design follows graph database best practices:

1. **Edges are expensive, properties are cheap** - Store aggregates on nodes
2. **No supernodes** - Cap relationships, use intermediate cluster nodes
3. **Precompute everything queryable** - Graph is for traversal, not aggregation
4. **Time-bound fraud data** - Keep hot data in graph, archive cold to Postgres
5. **Unidirectional edges** - Pick a direction, stick to it

### Node Types

#### User Node

```go
package syntopica

import (
    "time"
    "github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// CreateUser creates a User node with all required properties
func CreateUser(gs *storage.GraphStorage, externalID string) (*storage.Node, error) {
    properties := map[string]storage.Value{
        // Identity - external ID for application-level lookups
        "id": storage.StringValue(externalID),

        // Precomputed metrics (updated by cron jobs)
        "trustScore":       storage.FloatValue(50.0),  // 0-100, default to neutral
        "teachingPageRank": storage.FloatValue(0.0),   // 0-1, influence in teaching network
        "conceptCount":     storage.IntValue(0),       // Total concepts with MASTERY edge
        "verifiedCount":    storage.IntValue(0),       // Concepts at verified/mastered status

        // Fraud signals
        "flagCount":    storage.IntValue(0),  // Active fraud flags
        "clusterCount": storage.IntValue(0),  // Answer clusters user belongs to

        // Temporal
        "createdAt":    storage.TimestampValue(time.Now()),
        "lastActiveAt": storage.TimestampValue(time.Now()),
    }

    return gs.CreateNode([]string{"User"}, properties)
}
```

#### Concept Node

```go
// CreateConcept creates a Concept node for knowledge graph
func CreateConcept(gs *storage.GraphStorage, id, name, domain string, bloomLevel int, estimatedHours float64) (*storage.Node, error) {
    properties := map[string]storage.Value{
        "id":             storage.StringValue(id),
        "name":           storage.StringValue(name),
        "domain":         storage.StringValue(domain),
        "bloomLevel":     storage.IntValue(int64(bloomLevel)), // 1-6
        "estimatedHours": storage.FloatValue(estimatedHours),

        // Precomputed (updated by cron)
        "pageRank":     storage.FloatValue(0.0),
        "learnerCount": storage.IntValue(0),
    }

    return gs.CreateNode([]string{"Concept"}, properties)
}
```

#### AnswerCluster Node (Fraud Detection)

```go
// CreateAnswerCluster creates a cluster for grouping suspicious similar answers
func CreateAnswerCluster(gs *storage.GraphStorage, id, conceptID string, similarity float64) (*storage.Node, error) {
    properties := map[string]storage.Value{
        "id":         storage.StringValue(id),
        "conceptId":  storage.StringValue(conceptID),

        "memberCount": storage.IntValue(0),
        "similarity":  storage.FloatValue(similarity),

        // Fraud signals
        "flagged":    storage.BoolValue(false),
        "flagReason": storage.StringValue(""),  // 'rapid_growth' | 'high_similarity' | 'temporal_pattern'

        "createdAt":    storage.TimestampValue(time.Now()),
        "lastJoinedAt": storage.TimestampValue(time.Now()),
    }

    return gs.CreateNode([]string{"AnswerCluster"}, properties)
}
```

### Edge Types

#### MASTERY Edge (User → Concept)

```go
// MasteryStatus represents learning progress
type MasteryStatus string

const (
    MasteryEncountered MasteryStatus = "encountered"
    MasteryStudying    MasteryStatus = "studying"
    MasteryVerified    MasteryStatus = "verified"
    MasteryMastered    MasteryStatus = "mastered"
)

// CreateOrUpdateMastery creates or updates a MASTERY edge
// KEY DESIGN: ONE edge per user-concept pair, updated in place
func CreateOrUpdateMastery(gs *storage.GraphStorage, userNodeID, conceptNodeID uint64, status MasteryStatus, score int) (*storage.Edge, error) {
    properties := map[string]storage.Value{
        "status":      storage.StringValue(string(status)),
        "score":       storage.IntValue(int64(score)), // 0-100
        "verifyCount": storage.IntValue(1),
        "lastAt":      storage.TimestampValue(time.Now()),
    }

    return gs.CreateEdge(userNodeID, conceptNodeID, "MASTERY", properties, float64(score)/100.0)
}
```

#### TAUGHT Edge (User → User)

```go
// CreateOrUpdateTaught tracks teaching relationships
// NOTE: concepts field uses BytesValue since GraphDB doesn't have native arrays
func CreateOrUpdateTaught(gs *storage.GraphStorage, teacherNodeID, studentNodeID uint64, conceptIDs []string, rating float64) (*storage.Edge, error) {
    // Encode concept IDs as JSON or msgpack (cap at 20)
    if len(conceptIDs) > 20 {
        conceptIDs = conceptIDs[:20]
    }
    conceptsJSON, _ := json.Marshal(conceptIDs)

    properties := map[string]storage.Value{
        "sessionCount": storage.IntValue(1),
        "avgRating":    storage.FloatValue(rating),
        "concepts":     storage.BytesValue(conceptsJSON), // Encoded array
        "lastAt":       storage.TimestampValue(time.Now()),
    }

    return gs.CreateEdge(teacherNodeID, studentNodeID, "TAUGHT", properties, rating)
}
```

### Index Setup

Create indexes **before** inserting data for optimal performance:

```go
// SetupSyntopicaIndexes creates all required indexes
func SetupSyntopicaIndexes(gs *storage.GraphStorage) error {
    // Primary key lookups (external IDs)
    if err := gs.CreatePropertyIndex("id", storage.TypeString); err != nil {
        return fmt.Errorf("creating id index: %w", err)
    }

    // Query patterns
    if err := gs.CreatePropertyIndex("domain", storage.TypeString); err != nil {
        return fmt.Errorf("creating domain index: %w", err)
    }
    if err := gs.CreatePropertyIndex("conceptId", storage.TypeString); err != nil {
        return fmt.Errorf("creating conceptId index: %w", err)
    }
    if err := gs.CreatePropertyIndex("flagged", storage.TypeBool); err != nil {
        return fmt.Errorf("creating flagged index: %w", err)
    }

    // Sorting/filtering
    if err := gs.CreatePropertyIndex("trustScore", storage.TypeFloat); err != nil {
        return fmt.Errorf("creating trustScore index: %w", err)
    }
    if err := gs.CreatePropertyIndex("pageRank", storage.TypeFloat); err != nil {
        return fmt.Errorf("creating pageRank index: %w", err)
    }

    return nil
}
```

### Query Patterns

#### 1. Learning Path

Find prerequisites for a concept with user's current mastery status:

```go
// Using the query engine
query := `
MATCH (target:Concept {id: $conceptId})
MATCH (target)<-[:PREREQUISITE*1..5]-(prereq:Concept)
OPTIONAL MATCH (u:User {id: $userId})-[m:MASTERY]->(prereq)
RETURN prereq, m.status
`

result, err := queryEngine.Execute(ctx, query, map[string]interface{}{
    "conceptId": "calc-derivatives",
    "userId":    "user-123",
})
```

**Performance:** O(prerequisite depth × branching factor) - bounded by curriculum design

#### 2. Ready Concepts

Find concepts the user is ready to learn:

```go
query := `
MATCH (u:User {id: $userId})-[m:MASTERY]->(known:Concept)
WHERE m.status IN ['verified', 'mastered']
MATCH (known)-[:PREREQUISITE]->(next:Concept)
WHERE NOT EXISTS((u)-[:MASTERY]->(next))
RETURN DISTINCT next
`
```

#### 3. Collusion Detection

Check if user is in any flagged fraud clusters:

```go
query := `
MATCH (u:User {id: $userId})-[:MEMBER]->(c:AnswerCluster)
WHERE c.flagged = true
RETURN c, c.memberCount, c.flagReason
`
```

**Performance:** O(1-3) - most users belong to 0-3 clusters

#### 4. Programmatic Traversal (Alternative)

For simpler queries, use the traversal API directly:

```go
// Find user's fraud clusters using traversal
clusters, err := gs.Traverse(ctx, &storage.TraversalRequest{
    StartNodeID: userNodeID,
    Direction:   "outgoing",
    EdgeTypes:   []string{"MEMBER"},
    MaxDepth:    1,
    Filter: func(n *storage.Node) bool {
        flagged, _ := n.Properties["flagged"].AsBool()
        return flagged
    },
})
```

### Cardinality Constraints

Enforce the "no supernodes" principle:

```go
import "github.com/dd0wney/cluso-graphdb/pkg/constraints"

// User can teach at most 100 students
teachingLimit := &constraints.CardinalityConstraint{
    NodeLabel: "User",
    EdgeType:  "TAUGHT",
    Direction: constraints.Outgoing,
    Min:       0,
    Max:       100,
}

// Concepts have 1-10 prerequisites (curriculum design)
prereqLimit := &constraints.CardinalityConstraint{
    NodeLabel: "Concept",
    EdgeType:  "PREREQUISITE",
    Direction: constraints.Incoming,
    Min:       0,
    Max:       10,
}

// Answer clusters capped at 50 members
clusterLimit := &constraints.CardinalityConstraint{
    NodeLabel: "AnswerCluster",
    EdgeType:  "MEMBER",
    Direction: constraints.Incoming,
    Min:       0,
    Max:       50,
}
```

### Cron Jobs for Precomputation

These jobs update denormalized metrics:

```go
// UpdateUserMetrics - Run hourly
func UpdateUserMetrics(gs *storage.GraphStorage) error {
    users := gs.FindNodesByLabel("User")

    for _, user := range users {
        // Count MASTERY edges
        masteryEdges := gs.GetOutgoingEdges(user.ID, "MASTERY")
        conceptCount := len(masteryEdges)

        // Count verified/mastered
        verifiedCount := 0
        for _, edge := range masteryEdges {
            status, _ := edge.Properties["status"].AsString()
            if status == "verified" || status == "mastered" {
                verifiedCount++
            }
        }

        // Update node
        gs.UpdateNode(user.ID, map[string]storage.Value{
            "conceptCount":  storage.IntValue(int64(conceptCount)),
            "verifiedCount": storage.IntValue(int64(verifiedCount)),
        })
    }
    return nil
}

// ComputeConceptPageRank - Run daily
// Uses the built-in PageRank algorithm
func ComputeConceptPageRank(gs *storage.GraphStorage) error {
    concepts := gs.FindNodesByLabel("Concept")
    conceptIDs := make([]uint64, len(concepts))
    for i, c := range concepts {
        conceptIDs[i] = c.ID
    }

    // Run PageRank on PREREQUISITE subgraph
    ranks, err := algorithms.PageRank(gs, conceptIDs, 0.85, 20)
    if err != nil {
        return err
    }

    // Update nodes
    for nodeID, rank := range ranks {
        gs.UpdateNode(nodeID, map[string]storage.Value{
            "pageRank": storage.FloatValue(rank),
        })
    }
    return nil
}
```

### What Stays in PostgreSQL

| Data | Why Not Graph |
|------|---------------|
| Submission text/audio | Too large, not traversed |
| Embeddings | Vector ops belong in pgvector |
| Point transactions | Append-only log, not relationships |
| Verification attempts | Historical audit trail |
| Session history | Time-series data |

**Rule:** If you're not traversing relationships, it doesn't belong in the graph.

### Capacity Planning

| Entity | Expected Count | Growth Rate |
|--------|---------------|-------------|
| Users | 10k → 100k | 10x/year |
| Concepts | 5k → 20k | 4x/year |
| MASTERY edges | 500k → 5M | 10x/year |
| AnswerClusters | 1k → 10k | 10x/year |

At 100k users with 200 concepts each = 20M MASTERY edges maximum. GraphDB handles this well with disk-backed edges enabled.

### Known Limitations

1. **No native array types** - Use `BytesValue` with JSON/msgpack encoding
2. **No unique constraints** - Enforce at application level or use property index with dedup
3. **No composite indexes** - Use query engine with multiple WHERE conditions
4. **Variable-length paths can be slow** - Set query timeouts for user-facing paths
