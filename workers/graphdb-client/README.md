# @graphdb/client

GraphDB client for Cloudflare Workers, optimized for low latency and high reliability.

## Features

✅ **GraphQL & REST API Support** - Query with GraphQL, fall back to REST
✅ **KV Cache Wrapper** - Automatic caching with Cloudflare KV (cache-aside pattern)
✅ **Automatic Retries** - Exponential backoff for failed requests
✅ **Timeout Handling** - Enforced timeouts for Workers constraints (50-500ms)
✅ **Type-Safe** - Full TypeScript support with comprehensive types
✅ **Error Handling** - Structured error types for debugging
✅ **Auth Support** - API key and JWT authentication
✅ **Syntopica/Cluso Ready** - Built-in trust score and fraud detection methods

## Installation

```bash
npm install @graphdb/client
```

## Quick Start

```typescript
import { GraphDBClient } from '@graphdb/client';

// Create client
const graphDB = new GraphDBClient({
  endpoint: env.GRAPHDB_URL,
  apiKey: env.GRAPHDB_API_KEY,
  timeout: 5000,
  retries: 2,
});

// Query nodes
const users = await graphDB.queryNodes({ type: 'user' }, { limit: 10 });

// Traverse graph
const network = await graphDB.traverse({
  startNodeId: 'user-123',
  edgeTypes: ['TRUSTS', 'VERIFIED_BY'],
  maxDepth: 2,
  direction: 'outgoing',
});
```

## Configuration

### Client Options

```typescript
interface GraphDBClientConfig {
  endpoint: string;           // GraphDB API URL (required)
  apiKey?: string;            // API key authentication
  jwtToken?: string;          // JWT token authentication (alternative)
  timeout?: number;           // Request timeout in ms (default: 5000)
  retries?: number;           // Retry attempts (default: 2)
  retryDelay?: number;        // Initial retry delay in ms (default: 100)
  enableGraphQL?: boolean;    // Enable GraphQL queries (default: true)
  enableREST?: boolean;       // Enable REST fallback (default: true)
  headers?: Record<string, string>;  // Custom headers
}
```

### Example Configurations

#### Production (with API key)
```typescript
const graphDB = new GraphDBClient({
  endpoint: 'https://graphdb.cluso.app',
  apiKey: env.GRAPHDB_API_KEY,
  timeout: 5000,
  retries: 2,
});
```

#### Development (with JWT)
```typescript
const graphDB = new GraphDBClient({
  endpoint: 'http://localhost:8080',
  jwtToken: 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...',
  timeout: 10000,
});
```

#### Edge-optimized (fast failures)
```typescript
const graphDB = new GraphDBClient({
  endpoint: env.GRAPHDB_URL,
  apiKey: env.GRAPHDB_API_KEY,
  timeout: 500,   // 500ms max for edge functions
  retries: 1,     // Fail fast
});
```

## API Reference

### GraphQL Queries

#### Basic Query
```typescript
const result = await graphDB.query<{ user: User }>(
  `query GetUser($id: ID!) {
    user(id: $id) {
      id
      name
      trustScore
    }
  }`,
  { id: 'user-123' }
);
```

#### Mutation
```typescript
const result = await graphDB.mutate<{ createUser: User }>(
  `mutation CreateUser($input: UserInput!) {
    createUser(input: $input) {
      id
      name
    }
  }`,
  { input: { name: 'New User', type: 'user' } }
);
```

### REST API Methods

#### Nodes

**Get node by ID:**
```typescript
const user = await graphDB.getNode('user-123');
// { id: 'user-123', type: 'user', properties: { name: '...' } }
```

**Create node:**
```typescript
const newUser = await graphDB.createNode({
  type: 'user',
  properties: { name: 'Alice', email: 'alice@example.com' },
});
```

**Update node:**
```typescript
const updated = await graphDB.updateNode('user-123', {
  properties: { trustScore: 850 },
});
```

**Delete node:**
```typescript
await graphDB.deleteNode('user-123');
```

**Query nodes with filters:**
```typescript
const users = await graphDB.queryNodes(
  { type: 'user', 'properties.verified': true },
  { limit: 100, offset: 0, sortBy: 'createdAt', sortOrder: 'desc' }
);
```

#### Edges

**Create edge:**
```typescript
const edge = await graphDB.createEdge({
  type: 'TRUSTS',
  source: 'user-123',
  target: 'user-456',
  properties: { weight: 0.8, since: '2025-01-01' },
});
```

#### Batch Operations

**Batch create nodes:**
```typescript
const result = await graphDB.batchCreateNodes([
  { type: 'user', properties: { name: 'Alice' } },
  { type: 'user', properties: { name: 'Bob' } },
  { type: 'user', properties: { name: 'Charlie' } },
]);

console.log(result.success.length); // 3
console.log(result.failed.length);  // 0
```

**Batch create edges:**
```typescript
const result = await graphDB.batchCreateEdges([
  { type: 'TRUSTS', source: 'user-1', target: 'user-2' },
  { type: 'TRUSTS', source: 'user-2', target: 'user-3' },
]);
```

### Graph Traversal

**Traverse graph:**
```typescript
const network = await graphDB.traverse({
  startNodeId: 'user-123',
  edgeTypes: ['TRUSTS', 'VERIFIED_BY'],
  maxDepth: 2,
  direction: 'outgoing',
  limit: 100,
});

console.log(network.nodes);  // All reachable nodes
console.log(network.edges);  // All traversed edges
console.log(network.paths);  // All paths from start node
```

**Traversal options:**
- `direction`: `'outgoing'` | `'incoming'` | `'both'`
- `maxDepth`: Maximum hops from start node
- `edgeTypes`: Filter by edge types (empty = all types)
- `limit`: Max nodes to return

### Syntopica/Cluso Use Cases

#### Trust Score Lookup

```typescript
const trustScore = await graphDB.getTrustScore('user-123');

console.log(trustScore.score);  // 847
console.log(trustScore.components);
// {
//   verification: 0.9,
//   activity: 0.85,
//   reputation: 0.83
// }
```

**With KV Cache Wrapper (Recommended):**
```typescript
import { GraphDBClient, GraphDBCache } from '@graphdb/client';

// Create cache wrapper
const cache = new GraphDBCache(graphDB, env.TRUST_CACHE, {
  trustScoreTTL: 3600,  // 1 hour
  nodeTTL: 300,         // 5 minutes
});

// Automatic cache-aside pattern
const trustScore = await cache.getTrustScore('user-123');

// Check cache performance
const stats = cache.getStats();
console.log(`Cache hit rate: ${(stats.hitRate * 100).toFixed(1)}%`);
```

**Manual caching (for custom control):**
```typescript
async function getTrustScoreCached(userId: string) {
  // 1. Try cache first
  const cached = await env.TRUST_CACHE.get(`trust:${userId}`, 'json');
  if (cached) return cached;

  // 2. Cache miss - query GraphDB
  const trustScore = await graphDB.getTrustScore(userId);

  // 3. Cache for 1 hour
  await env.TRUST_CACHE.put(
    `trust:${userId}`,
    JSON.stringify(trustScore),
    { expirationTtl: 3600 }
  );

  return trustScore;
}
```

#### Fraud Ring Detection

```typescript
const fraudRing = await graphDB.findFraudRing('user-suspicious');

if (fraudRing.suspicionScore > 0.8) {
  console.log('High fraud risk!');
  console.log('Reasons:', fraudRing.reasons);
  // ['Similar IP addresses', 'Coordinated activity', ...]

  console.log('Related accounts:', fraudRing.nodes.length);
  console.log('Suspicious connections:', fraudRing.edges.length);
}
```

### Health & Metrics

**Health check:**
```typescript
const health = await graphDB.healthCheck();
// { status: 'ok', version: '1.0.0', uptime: 3600, timestamp: '...' }
```

**Get metrics:**
```typescript
const metrics = await graphDB.getMetrics();
console.log(metrics);
// {
//   nodes_total: 1000000,
//   edges_total: 5000000,
//   active_queries: 5,
//   cache_hit_rate: 0.95,
//   avg_query_latency_ms: 45
// }
```

### KV Cache Wrapper

The `GraphDBCache` class provides automatic caching with Cloudflare KV using the cache-aside pattern.

**Setup:**
```typescript
import { GraphDBClient, GraphDBCache } from '@graphdb/client';

const graphDB = new GraphDBClient({
  endpoint: env.GRAPHDB_URL,
  apiKey: env.GRAPHDB_API_KEY,
});

const cache = new GraphDBCache(graphDB, env.TRUST_CACHE, {
  trustScoreTTL: 3600,    // Trust scores: 1 hour
  nodeTTL: 300,           // Nodes: 5 minutes
  traversalTTL: 600,      // Traversals: 10 minutes
  fraudTTL: 86400,        // Fraud detection: 24 hours
});
```

**Methods:**
```typescript
// Cache-aside queries
const trustScore = await cache.getTrustScore('user-123');
const node = await cache.getNode('node-456');
const result = await cache.traverse('user-123', ['TRUSTS'], 2, 'outgoing');

// Cache invalidation
await cache.invalidateTrustScore('user-123');
await cache.invalidateNode('node-456');
await cache.invalidateMultiple(['trust:user-1', 'node:node-2']);

// Statistics
const stats = cache.getStats();
console.log(`Hit rate: ${(stats.hitRate * 100).toFixed(1)}%`);
console.log(`Hits: ${stats.hits}, Misses: ${stats.misses}`);

cache.resetStats();
```

**Cache Configuration:**
```typescript
interface CacheConfig {
  defaultTTL?: number;      // Default: 3600 seconds (1 hour)
  trustScoreTTL?: number;   // Default: 3600 seconds (1 hour)
  nodeTTL?: number;         // Default: 300 seconds (5 minutes)
  traversalTTL?: number;    // Default: 600 seconds (10 minutes)
  fraudTTL?: number;        // Default: 86400 seconds (24 hours)
}
```

**Benefits:**
- Automatic cache-aside pattern implementation
- Per-data-type TTL configuration
- Built-in cache statistics tracking
- Graceful error handling (falls back to GraphDB on KV errors)
- Cache key generation and management

## Error Handling

All errors thrown by the client are instances of `GraphDBError`:

```typescript
import { GraphDBError, GraphDBErrorType } from '@graphdb/client';

try {
  const user = await graphDB.getNode('user-999');
} catch (error) {
  if (error instanceof GraphDBError) {
    console.log(error.type);        // GraphDBErrorType.NotFoundError
    console.log(error.statusCode);  // 404
    console.log(error.message);     // 'Node not found'
    console.log(error.details);     // Additional error info
  }
}
```

### Error Types

```typescript
enum GraphDBErrorType {
  NetworkError = 'NETWORK_ERROR',           // Network failure
  TimeoutError = 'TIMEOUT_ERROR',           // Request timeout
  AuthenticationError = 'AUTHENTICATION_ERROR',  // 401/403
  GraphQLError = 'GRAPHQL_ERROR',           // GraphQL errors
  NotFoundError = 'NOT_FOUND_ERROR',        // 404
  ValidationError = 'VALIDATION_ERROR',     // 400/422
  ServerError = 'SERVER_ERROR',             // 5xx errors
}
```

### Retry Behavior

The client automatically retries on:
- **Network errors** (up to `retries` times with exponential backoff)
- **5xx server errors** (up to `retries` times)
- **Timeout errors** (up to `retries` times)

The client does NOT retry on:
- **4xx client errors** (except for potential rate limiting in the future)
- **Authentication errors** (401/403)
- **Validation errors** (400)

## Complete Examples

### Cloudflare Worker with KV Cache

```typescript
// worker.ts
import { GraphDBClient } from '@graphdb/client';

interface Env {
  GRAPHDB_URL: string;
  GRAPHDB_API_KEY: string;
  TRUST_CACHE: KVNamespace;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const graphDB = new GraphDBClient({
      endpoint: env.GRAPHDB_URL,
      apiKey: env.GRAPHDB_API_KEY,
      timeout: 5000,
      retries: 2,
    });

    const url = new URL(request.url);
    const userId = url.searchParams.get('userId');

    if (!userId) {
      return new Response('Missing userId', { status: 400 });
    }

    try {
      // Try KV cache first (95% hit rate expected)
      const cached = await env.TRUST_CACHE.get(`trust:${userId}`, 'json');
      if (cached) {
        return new Response(JSON.stringify(cached), {
          headers: { 'Content-Type': 'application/json', 'X-Cache': 'HIT' },
        });
      }

      // Cache miss - query GraphDB
      const trustScore = await graphDB.getTrustScore(userId);

      // Cache for 1 hour
      await env.TRUST_CACHE.put(
        `trust:${userId}`,
        JSON.stringify(trustScore),
        { expirationTtl: 3600 }
      );

      return new Response(JSON.stringify(trustScore), {
        headers: { 'Content-Type': 'application/json', 'X-Cache': 'MISS' },
      });
    } catch (error) {
      console.error('Error:', error);
      return new Response('Internal error', { status: 500 });
    }
  },
};
```

### Campaign Generation (Bulk Import)

```typescript
// Generate 500 nodes + 2000 edges in batches
async function generateCampaign(conceptIds: string[]) {
  // Create concept nodes in batch
  const concepts = conceptIds.map(id => ({
    type: 'concept',
    properties: { id, domain: 'physics' },
  }));

  const nodeResult = await graphDB.batchCreateNodes(concepts);
  console.log(`Created ${nodeResult.success.length} nodes`);

  // Create prerequisite edges
  const edges = [];
  for (let i = 0; i < conceptIds.length - 1; i++) {
    edges.push({
      type: 'PREREQUISITE',
      source: conceptIds[i],
      target: conceptIds[i + 1],
    });
  }

  const edgeResult = await graphDB.batchCreateEdges(edges);
  console.log(`Created ${edgeResult.success.length} edges`);
}
```

### Real-time Synopsis Approval

```typescript
// Update trust score when synopsis is approved
async function approveSynopsis(synopsisId: string, userId: string) {
  // Create edge: User -> Synopsis
  await graphDB.createEdge({
    type: 'APPROVED',
    source: userId,
    target: synopsisId,
    properties: {
      stars: 5,
      timestamp: new Date().toISOString(),
    },
  });

  // Update user trust score
  const user = await graphDB.getNode(userId);
  const newTrustScore = user.properties.trustScore + 10;

  await graphDB.updateNode(userId, {
    properties: { trustScore: newTrustScore },
  });

  // Invalidate cache
  await env.TRUST_CACHE.delete(`trust:${userId}`);
}
```

## Performance Tips

### 1. Use Cloudflare KV for Caching

Cache frequently accessed data like trust scores:

```typescript
// Cache hit rate: 95% expected
// Latency: KV = 10-50ms, GraphDB = 50-500ms
const cached = await env.TRUST_CACHE.get(`trust:${userId}`, 'json');
if (cached) return cached; // 95% of requests end here
```

### 2. Batch Operations

Use batch methods for bulk operations (5-10x faster):

```typescript
// Bad: 500 individual requests
for (const input of inputs) {
  await graphDB.createNode(input);
}

// Good: 1 batch request
await graphDB.batchCreateNodes(inputs);
```

### 3. Limit Traversal Depth

Keep graph traversals shallow (maxDepth ≤ 3):

```typescript
// Good: Fast, focused traversal
const network = await graphDB.traverse({
  startNodeId: userId,
  maxDepth: 2,
  limit: 100,
});

// Bad: Slow, unbounded traversal
const network = await graphDB.traverse({
  startNodeId: userId,
  maxDepth: 5,  // Exponential growth!
});
```

### 4. Use GraphQL for Complex Queries

GraphQL is more efficient for complex nested queries:

```typescript
// Fetch user + trust network in one request
const result = await graphDB.query(`
  query GetUserWithNetwork($id: ID!) {
    user(id: $id) {
      id
      trustScore
      trustedBy {
        id
        trustScore
      }
    }
  }
`, { id: userId });
```

## Testing

```bash
# Run tests
npm test

# Watch mode
npm run test:watch

# Coverage
npm run test:coverage
```

## TypeScript Support

This package includes full TypeScript definitions:

```typescript
import {
  GraphDBClient,
  GraphDBCache,
  Node,
  Edge,
  TrustScore,
  FraudRing,
  GraphDBError,
  CacheConfig,
  CacheStats,
} from '@graphdb/client';

// Type-safe queries
const user: Node = await graphDB.getNode('user-123');
const trustScore: TrustScore = await graphDB.getTrustScore('user-123');

// Type-safe caching
const cache = new GraphDBCache(graphDB, env.TRUST_CACHE);
const stats: CacheStats = cache.getStats();
```

## License

MIT

## Contributing

Contributions welcome! Please open an issue or PR.

## Links

- [GraphDB Documentation](https://github.com/dd0wney/graphdb)
- [Cloudflare Workers Docs](https://developers.cloudflare.com/workers/)
- [Syntopica](https://syntopica.com)
- [Cluso](https://cluso.app)
