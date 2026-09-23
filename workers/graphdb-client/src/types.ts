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
 * The not-durable report the server sends with a 202 Accepted. The write
 * applied in memory, but its WAL append failed, so the write is not yet
 * durable. Mirrors the five `notDurableFields` in pkg/api/types.go and the
 * data clients/go/errors.go carries in NotDurableError.
 *
 * `id` is the affected entity's id. It is absent when the body omits it,
 * which the bulk-delete and vector-index handlers do, because those writes
 * have no single entity id.
 *
 * A write that carries this MUST NOT be retried: the server already applied
 * it once, and POST is not idempotent, so a retry duplicates the mutation.
 */
export type NotDurable = {
  applied: boolean;
  durable: boolean;
  retry: boolean;
  error: string;
  message: string;
  id?: number;
};

/**
 * What a delete resolves with. It is an empty object on a 204 No Content,
 * the ordinary success, and carries `notDurable` on a 202 Accepted.
 *
 * These methods returned `Promise<void>` before the 202 contract existed.
 * Resolving with an object instead is not a breaking change: a caller that
 * ignores the value, which is every caller written against `void`, behaves
 * exactly as it did.
 */
export type DeleteResult = {
  notDurable?: NotDurable;
};

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
  /** Set only on a 202 Accepted; absent on every other status. */
  notDurable?: NotDurable;
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
  /** Set only on a 202 Accepted; absent on every other status. */
  notDurable?: NotDurable;
};

/**
 * Result of queryNodes(). GET /nodes returns a bare JSON array (no
 * envelope) plus the next cursor in the X-Next-Cursor response header,
 * absent on the last page (pkg/api/pagination.go, handlers_nodes.go
 * listNodes). There is no `total`/`hasMore` on the wire — v1 declared
 * both but the server never sent them.
 */
export type QueryResult<T> = {
  nodes: T[];
  cursor?: string;
};

/**
 * Server-side filter for queryNodes(). GET /nodes only honours `?label=`
 * (handlers_nodes.go listNodes) — v1's generic `filters` object was
 * serialized into a `?filter=` query param the server never read.
 */
export type QueryNodesFilter = {
  label?: string;
};

/**
 * Query options for queryNodes(). Only `limit` and `cursor` are read by
 * GET /nodes (pkg/api/pagination.go parsePageRequest) — `offset`,
 * `sortBy`, `sortOrder` and `fields` from v1's QueryOptions were never
 * read server-side and are dropped here rather than silently doing
 * nothing.
 */
export type QueryNodesOptions = {
  /** Page size. Server default is 100, capped at 1000. */
  limit?: number;

  /** Cursor from a previous QueryResult, to fetch the next page. */
  cursor?: string;
};

/**
 * Traversal options for traverse(). Sent as the JSON body of
 * POST /traverse (pkg/api/handlers_algorithms_traversal.go
 * handleTraversal) after translating to the server's snake_case
 * TraversalRequest fields. There is no `limit` or `nodeFilter` on the
 * wire — TraversalRequest has no such fields, so v1's values were
 * silently ignored.
 */
export type TraversalOptions = {
  /** Starting node ID */
  startNodeId: number;

  /** Edge types to traverse (empty/omitted = all types) */
  edgeTypes?: string[];

  /** Maximum traversal depth */
  maxDepth: number;

  /** Traversal direction */
  direction: 'outgoing' | 'incoming' | 'both';
};

/**
 * Traversal result. Matches pkg/api/types.go TraversalResponse: a flat
 * node list, not the `{nodes, edges, paths}` shape v1 expected from a
 * GraphQL `traverse` field that no resolver ever defined.
 */
export type TraversalResult = {
  nodes: Node[];
  count: number;
  time: string;
  /** True when the server capped the result at MaxTraversalNodes. */
  truncated?: boolean;
};

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
 * Update edge input for PUT /edges/{id}. Matches pkg/api/types.go
 * EdgeUpdateRequest. `weight: undefined` (an omitted field) leaves the
 * edge's stored weight unchanged; there is no way to explicitly re-zero
 * it separately from "don't touch it" on the wire.
 */
export type UpdateEdgeInput = {
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

/**
 * One audit log entry. Matches pkg/audit/audit.go Event.
 */
export type AuditLogEntry = {
  id: string;
  timestamp: string;
  tenant_id?: string;
  user_id?: string;
  username?: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  status: string;
  error_message?: string;
  ip_address?: string;
  user_agent?: string;
  metadata?: Record<string, unknown>;
};

/**
 * Options for getAuditLog(). Translated to the ?user_id=/?resource_type=/
 * etc. query params GET /v1/compliance/audit-log reads
 * (pkg/api/handlers_compliance.go handleComplianceAuditLog). `startTime`/
 * `endTime` are RFC3339 strings.
 */
export type AuditLogOptions = {
  userId?: string;
  username?: string;
  action?: string;
  resourceType?: string;
  status?: string;
  startTime?: string;
  endTime?: string;
  limit?: number;
  offset?: number;
};

/**
 * Response from getAuditLog(). Matches the map[string]any body
 * handleComplianceAuditLog returns.
 */
export type AuditLogResponse = {
  events: AuditLogEntry[];
  count: number;
  total: number;
  offset: number;
  limit: number;
  has_more: boolean;
  tenant?: string;
  cross_tenant?: boolean;
};

/**
 * Per-property masking strategy. Matches pkg/masking/masking_types.go
 * MaskingStrategy.
 */
export type MaskingStrategyName =
  | 'full'
  | 'partial'
  | 'hash'
  | 'redact'
  | 'tokenize'
  | 'none';

/**
 * A tenant's masking policy. Matches pkg/masking/policy_types.go Policy.
 */
export type MaskingPolicy = {
  tenant_id: string;
  properties?: Record<string, MaskingStrategyName>;
  auto_detect: boolean;
  updated_at: string;
};

/**
 * Body for setMaskingPolicy() — POST /v1/compliance/masking-policy.
 * Admin-only; the target tenant comes from the caller's auth context, not
 * this body (mirrors clients/python's set_masking_policy).
 */
export type SetMaskingPolicyInput = {
  properties: Record<string, MaskingStrategyName>;
  autoDetect?: boolean;
};

/**
 * Distance metric for a vector index. Matches pkg/vector's
 * DistanceMetric, as serialized by pkg/api/handlers_vectors.go
 * metricToString.
 */
export type VectorMetric = 'cosine' | 'euclidean' | 'dot_product';

/**
 * A vector index, as returned by the /vector-indexes endpoints. Matches
 * pkg/api/handlers_vectors.go VectorIndexResponse.
 */
export type VectorIndex = {
  property_name: string;
  dimensions?: number;
  metric?: string;
  /** Set only on a 202 Accepted; absent on every other status. */
  notDurable?: NotDurable;
};

/**
 * Response from listVectorIndexes(). Matches
 * pkg/api/handlers_vectors.go VectorIndexListResponse.
 */
export type VectorIndexList = {
  indexes: VectorIndex[];
  count: number;
};

/**
 * Body for createVectorIndex() — POST /vector-indexes. Translated to the
 * server's snake_case VectorIndexRequest fields.
 */
export type CreateVectorIndexInput = {
  propertyName: string;
  dimensions: number;
  m?: number;
  efConstruction?: number;
  metric?: VectorMetric;
};
