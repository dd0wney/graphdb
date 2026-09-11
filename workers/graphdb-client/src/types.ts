/**
 * GraphDB Client for Cloudflare Workers
 * Type definitions
 */

/**
 * Client configuration options
 */
export interface GraphDBClientConfig {
  /** GraphDB API endpoint URL */
  endpoint: string;

  /** API key for authentication (recommended) */
  apiKey?: string;

  /** JWT token for authentication (alternative to apiKey) */
  jwtToken?: string;

  /** Request timeout in milliseconds (default: 5000ms) */
  timeout?: number;

  /** Number of retry attempts for failed requests (default: 2) */
  retries?: number;

  /** Initial retry delay in milliseconds (default: 100ms) */
  retryDelay?: number;

  /** Enable GraphQL queries (default: true) */
  enableGraphQL?: boolean;

  /** Enable REST API fallback (default: true) */
  enableREST?: boolean;

  /** Custom headers to include in all requests */
  headers?: Record<string, string>;
}

/**
 * GraphQL query variables
 */
export type GraphQLVariables = Record<string, unknown>;

/**
 * GraphQL response structure
 */
export interface GraphQLResponse<T = unknown> {
  data?: T;
  errors?: Array<{
    message: string;
    locations?: Array<{ line: number; column: number }>;
    path?: Array<string | number>;
    extensions?: Record<string, unknown>;
  }>;
}

/**
 * Node properties (generic key-value pairs)
 */
export type NodeProperties = Record<string, unknown>;

/**
 * Graph node. Matches pkg/api/types.go NodeResponse: the id is the JSON
 * number the server's uint64 id marshals to (not a string), and a node's
 * type is carried as `labels` (a node can carry more than one label).
 * v1 declared `id: string`, `type: string`, and `createdAt`/`updatedAt`
 * that the server never returns.
 */
export type Node = {
  id: number;
  labels: string[];
  properties: NodeProperties;
};

/**
 * Graph edge. Matches pkg/api/types.go EdgeResponse. v1 declared
 * `id: string` and `source`/`target` that the server never returns.
 */
export type Edge = {
  id: number;
  from_node_id: number;
  to_node_id: number;
  type: string;
  properties: NodeProperties;
  weight: number;
};

/**
 * Query result with pagination
 */
export interface QueryResult<T> {
  data: T[];
  total: number;
  hasMore: boolean;
  cursor?: string;
}

/**
 * Traversal options
 */
export interface TraversalOptions {
  /** Starting node ID */
  startNodeId: string;

  /** Edge types to traverse (empty = all types) */
  edgeTypes?: string[];

  /** Maximum traversal depth */
  maxDepth: number;

  /** Traversal direction */
  direction: 'outgoing' | 'incoming' | 'both';

  /** Limit number of nodes returned */
  limit?: number;

  /** Filter function for nodes */
  nodeFilter?: (node: Node) => boolean;
}

/**
 * Traversal result
 */
export interface TraversalResult {
  nodes: Node[];
  edges: Edge[];
  paths: Array<{
    nodes: string[];
    edges: string[];
  }>;
}

/**
 * Trust score result
 */
export interface TrustScore {
  userId: string;
  score: number;
  components: {
    verification: number;
    activity: number;
    reputation: number;
  };
  lastUpdated: string;
}

/**
 * Fraud ring detection result
 */
export interface FraudRing {
  nodes: Node[];
  edges: Edge[];
  suspicionScore: number;
  reasons: string[];
}

/**
 * Error types
 */
export enum GraphDBErrorType {
  NetworkError = 'NETWORK_ERROR',
  TimeoutError = 'TIMEOUT_ERROR',
  AuthenticationError = 'AUTHENTICATION_ERROR',
  GraphQLError = 'GRAPHQL_ERROR',
  NotFoundError = 'NOT_FOUND_ERROR',
  ValidationError = 'VALIDATION_ERROR',
  ServerError = 'SERVER_ERROR',
}

/**
 * GraphDB client error
 */
export class GraphDBError extends Error {
  constructor(
    message: string,
    public type: GraphDBErrorType,
    public statusCode?: number,
    public details?: unknown
  ) {
    super(message);
    this.name = 'GraphDBError';
  }
}

/**
 * Query options for REST API
 */
export interface QueryOptions {
  /** Result limit */
  limit?: number;

  /** Pagination offset */
  offset?: number;

  /** Cursor for cursor-based pagination */
  cursor?: string;

  /** Sort field */
  sortBy?: string;

  /** Sort order */
  sortOrder?: 'asc' | 'desc';

  /** Fields to include in response */
  fields?: string[];
}

/**
 * Create node input. Matches pkg/api/types.go NodeRequest: the server
 * decodes `labels`, never `type` — a POST /nodes body carrying `type`
 * fails validation with "at least one label is required" on every call.
 */
export type CreateNodeInput = {
  labels: string[];
  properties: NodeProperties;
};

/**
 * Update node input
 */
export interface UpdateNodeInput {
  properties: Partial<NodeProperties>;
}

/**
 * Create edge input. Matches pkg/api/types.go EdgeRequest: the server
 * decodes `from_node_id`/`to_node_id`, never `source`/`target`, and
 * accepts an optional `weight`. The v1 field names were never read by
 * the server, so every edge was created with from_node_id: 0,
 * to_node_id: 0.
 */
export type CreateEdgeInput = {
  from_node_id: number;
  to_node_id: number;
  type: string;
  properties?: NodeProperties;
  weight?: number;
};

/**
 * One failed item from a batch create request. `index` is the item's
 * position in the REQUEST array, not the response (failed items are
 * omitted from the response array entirely).
 */
export type BatchItemError = {
  index: number;
  error: string;
};

/**
 * Batch node creation response. Matches pkg/api/types.go
 * BatchNodeResponse — v1's `{success, failed}` shape (with `failed` as
 * an array of created items) never matched what the server actually
 * returns: `failed` is a count, and per-item failures are in `errors`.
 */
export type BatchNodeResult = {
  nodes: Node[];
  created: number;
  time: string;
  failed: number;
  errors?: BatchItemError[];
};

/**
 * Batch edge creation response. Matches pkg/api/types.go
 * BatchEdgeResponse.
 */
export type BatchEdgeResult = {
  edges: Edge[];
  created: number;
  time: string;
  failed: number;
  errors?: BatchItemError[];
};

/**
 * Health check response
 */
export interface HealthCheckResponse {
  status: 'ok' | 'degraded' | 'down';
  version: string;
  uptime: number;
  timestamp: string;
}

/**
 * Metrics response
 */
export interface MetricsResponse {
  nodes_total: number;
  edges_total: number;
  active_queries: number;
  cache_hit_rate: number;
  avg_query_latency_ms: number;
}
