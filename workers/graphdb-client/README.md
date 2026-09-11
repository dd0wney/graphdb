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
✅ **Compliance & Vector Search** - Audit log, masking policy, and vector index methods

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

// Query nodes (only `label` is a server-side filter — see "Query nodes" below)
const users = await graphDB.queryNodes({ label: 'Person' }, { limit: 10 });

// Traverse graph
const network = await graphDB.traverse({
  startNodeId: 123,
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

A per-label GraphQL type (`pkg/graphql/schema.go` createNodeType) exposes
`id`, `labels`, and `properties` — `properties` is a JSON-encoded string,
not individual property fields:

```typescript
const result = await graphDB.query<{ person: { id: string; properties: string } }>(
  `query GetPerson($id: ID!) {
    person(id: $id) {
      id
      properties
    }
  }`,
  { id: '123' }
);
const properties = JSON.parse(result.person.properties);
```

#### Cursor Pagination (`limit`/`after`)

The plural query for a label (e.g. `persons` for a `Person` label) takes
`limit` and an ID-cursor `after`: `after` is the ID of the last item the
caller has already seen, and a page shorter than `limit` is the last page.
`after` cannot be combined with `orderBy` or a non-zero `offset` — the
cursor already fixes the walk order and the start position.

```typescript
const limit = 50;
const people: Array<{ id: string; properties: string }> = [];
let after: string | undefined;

for (;;) {
  const result = await graphDB.query<{
    persons: Array<{ id: string; properties: string }>;
  }>(
    `query Persons($limit: Int!, $after: ID) {
      persons(limit: $limit, after: $after) { id properties }
    }`,
    { limit, after }
  );

  people.push(...result.persons);
  if (result.persons.length < limit) break; // short page = last page
  after = result.persons[result.persons.length - 1].id;
}
```

#### Mutation

The generic `createNode` mutation (pkg/graphql/mutations.go) takes
`properties` as a JSON-encoded **string**, not a nested object:

```typescript
const result = await graphDB.mutate<{ createNode: { id: string } }>(
  `mutation CreateNode($labels: [String!]!, $properties: String!) {
    createNode(labels: $labels, properties: $properties) {
      id
    }
  }`,
  { labels: ['Person'], properties: JSON.stringify({ name: 'New Person' }) }
);
```

For most create/read/update/delete work, the REST methods below are more
convenient — `createNode()` builds this same request without the manual
`JSON.stringify`.

### REST API Methods

#### Nodes

**Get node by ID:**
```typescript
const person = await graphDB.getNode(123);
// { id: 123, labels: ['Person'], properties: { name: '...' } }
```

**Create node:**
```typescript
const newPerson = await graphDB.createNode({
  labels: ['Person'],
  properties: { name: 'Alice', email: 'alice@example.com' },
});
```

**Update node:**
```typescript
const updated = await graphDB.updateNode(123, {
  properties: { verified: true },
});
```

**Delete node:**
```typescript
await graphDB.deleteNode(123);
```

**Query nodes (cursor pagination):**

`GET /nodes` only honours a `label` filter and `limit`/`cursor` for
paging server-side — `offset`, `sortBy`, `sortOrder` are not read by the
server. The response is a bare array; the next page's cursor comes back
in the `X-Next-Cursor` response header and is absent on the last page.

```typescript
let cursor: string | undefined;
const people = [];

do {
  const page = await graphDB.queryNodes({ label: 'Person' }, { limit: 100, cursor });
  people.push(...page.nodes);
  cursor = page.cursor;
} while (cursor);
```

#### Edges

**Create edge:**
```typescript
const edge = await graphDB.createEdge({
  type: 'TRUSTS',
  from_node_id: 123,
  to_node_id: 456,
  properties: { since: '2025-01-01' },
  weight: 0.8,
});
```

**Get, update, delete edge:**
```typescript
const edge = await graphDB.getEdge(789);

const updated = await graphDB.updateEdge(789, {
  properties: { since: '2026-01-01' },
  weight: 0.9,
});

await graphDB.deleteEdge(789);
```

#### Batch Operations

**Batch create nodes:**
```typescript
const result = await graphDB.batchCreateNodes([
  { labels: ['Person'], properties: { name: 'Alice' } },
  { labels: ['Person'], properties: { name: 'Bob' } },
  { labels: ['Person'], properties: { name: 'Charlie' } },
]);

console.log(result.nodes.length); // 3 created
console.log(result.failed);       // 0 — a count, not an array
console.log(result.errors);       // [{ index, error }] for any failures
```

**Batch create edges:**
```typescript
const result = await graphDB.batchCreateEdges([
  { type: 'TRUSTS', from_node_id: 1, to_node_id: 2 },
  { type: 'TRUSTS', from_node_id: 2, to_node_id: 3 },
]);
```

### Graph Traversal

**Traverse graph:**
```typescript
const network = await graphDB.traverse({
  startNodeId: 123,
  edgeTypes: ['TRUSTS', 'VERIFIED_BY'],
  maxDepth: 2,
  direction: 'outgoing',
});

console.log(network.nodes);      // All reachable nodes
console.log(network.count);      // network.nodes.length
console.log(network.truncated);  // true if the server capped the result
```

`POST /traverse` (`pkg/api/handlers_algorithms_traversal.go`) returns
only a flat node list — there is no `edges` or `paths` field on the
response, and no `limit` argument on the request.

**Traversal options:**
- `direction`: `'outgoing'` | `'incoming'` | `'both'`
- `maxDepth`: Maximum hops from start node
- `edgeTypes`: Filter by edge types (empty/omitted = all types)

### Compliance API

**Audit log:**
```typescript
const log = await graphDB.getAuditLog({
  resourceType: 'node',
  status: 'success',
  limit: 50,
});
console.log(log.events, log.total, log.has_more);
```

**Masking policy (admin-only for writes):**
```typescript
const policy = await graphDB.getMaskingPolicy('tenant-a');

await graphDB.setMaskingPolicy({
  properties: { email: 'hash', ssn: 'redact' },
  autoDetect: true,
});
```

### Vector Index API

```typescript
await graphDB.createVectorIndex({
  propertyName: 'embedding',
  dimensions: 128,
  metric: 'cosine',
});

const indexes = await graphDB.listVectorIndexes();
const index = await graphDB.getVectorIndex('embedding');
await graphDB.deleteVectorIndex('embedding');
```

### Health & Metrics

> These two methods were not re-verified against `pkg/api` for this
> release (see `CHANGELOG.md`). `getMetrics()` in particular may be
> pointed at the wrong route or response shape — treat the example
> output below as unverified.

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

const cache = new GraphDBCache(graphDB, env.GRAPHDB_CACHE, {
  nodeTTL: 300,           // Nodes: 5 minutes
  traversalTTL: 600,      // Traversals: 10 minutes
});
```

**Methods:**
```typescript
// Cache-aside queries
const node = await cache.getNode(456);
const result = await cache.traverse(123, ['TRUSTS'], 2, 'outgoing');

// Cache invalidation
await cache.invalidateNode(456);
await cache.invalidateMultiple(['node:1', 'node:2']);

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
  nodeTTL?: number;         // Default: 300 seconds (5 minutes)
  traversalTTL?: number;    // Default: 600 seconds (10 minutes)
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
  const person = await graphDB.getNode(999);
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
  GRAPHDB_CACHE: KVNamespace;
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
    const nodeIdParam = url.searchParams.get('nodeId');
    const nodeId = nodeIdParam ? Number(nodeIdParam) : NaN;

    if (!Number.isFinite(nodeId)) {
      return new Response('Missing or invalid nodeId', { status: 400 });
    }

    try {
      // Try KV cache first
      const cached = await env.GRAPHDB_CACHE.get(`node:${nodeId}`, 'json');
      if (cached) {
        return new Response(JSON.stringify(cached), {
          headers: { 'Content-Type': 'application/json', 'X-Cache': 'HIT' },
        });
      }

      // Cache miss - query GraphDB
      const node = await graphDB.getNode(nodeId);

      // Cache for 5 minutes
      await env.GRAPHDB_CACHE.put(
        `node:${nodeId}`,
        JSON.stringify(node),
        { expirationTtl: 300 }
      );

      return new Response(JSON.stringify(node), {
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
async function generateCampaign(conceptCount: number) {
  // Create concept nodes in batch
  const concepts = Array.from({ length: conceptCount }, (_, i) => ({
    labels: ['Concept'],
    properties: { domain: 'physics', order: i },
  }));

  const nodeResult = await graphDB.batchCreateNodes(concepts);
  console.log(`Created ${nodeResult.nodes.length} nodes`);
  const conceptIds = nodeResult.nodes.map((n) => n.id);

  // Create prerequisite edges
  const edges = [];
  for (let i = 0; i < conceptIds.length - 1; i++) {
    edges.push({
      type: 'PREREQUISITE',
      from_node_id: conceptIds[i],
      to_node_id: conceptIds[i + 1],
    });
  }

  const edgeResult = await graphDB.batchCreateEdges(edges);
  console.log(`Created ${edgeResult.edges.length} edges`);
}
```

### Recording Approval (Node + Edge Update)

```typescript
// Record that a reviewer approved a synopsis, and bump its review count.
async function approveSynopsis(synopsisId: number, reviewerId: number) {
  // Create edge: Reviewer -> Synopsis
  await graphDB.createEdge({
    type: 'APPROVED',
    from_node_id: reviewerId,
    to_node_id: synopsisId,
    properties: { timestamp: new Date().toISOString() },
    weight: 1,
  });

  // Update the synopsis's review count
  const synopsis = await graphDB.getNode(synopsisId);
  const reviewCount = Number(synopsis.properties.reviewCount ?? 0) + 1;

  await graphDB.updateNode(synopsisId, {
    properties: { reviewCount },
  });

  // Invalidate cache
  await env.GRAPHDB_CACHE.delete(`node:${synopsisId}`);
}
```

## Performance Tips

### 1. Use Cloudflare KV for Caching

Cache frequently accessed nodes:

```typescript
// Latency: KV = 10-50ms, GraphDB = 50-500ms
const cached = await env.GRAPHDB_CACHE.get(`node:${nodeId}`, 'json');
if (cached) return cached;
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
// Good: fast, focused traversal
const network = await graphDB.traverse({
  startNodeId: personId,
  maxDepth: 2,
});

// Bad: slow, unbounded traversal
const network = await graphDB.traverse({
  startNodeId: personId,
  maxDepth: 5,  // Exponential growth! There is no server-side `limit` —
                // MaxTraversalNodes caps the result and sets `truncated`.
});
```

### 4. Use GraphQL to Combine Several Reads

GraphQL lets several top-level fields share one round trip — for
example a node plus a filtered list from another label:

```typescript
const result = await graphDB.query<{
  person: { id: string; properties: string };
  concepts: Array<{ id: string; properties: string }>;
}>(`
  query PersonAndConcepts($id: ID!, $limit: Int!) {
    person(id: $id) { id properties }
    concepts(limit: $limit) { id properties }
  }
`, { id: String(personId), limit: 10 });
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
  GraphDBError,
  CacheConfig,
  CacheStats,
} from '@graphdb/client';

// Type-safe queries
const person: Node = await graphDB.getNode(123);
const edge: Edge = await graphDB.getEdge(789);

// Type-safe caching
const cache = new GraphDBCache(graphDB, env.GRAPHDB_CACHE);
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
