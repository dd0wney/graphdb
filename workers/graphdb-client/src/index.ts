/**
 * @graphdb/client
 *
 * GraphDB client for Cloudflare Workers with:
 * - GraphQL and REST API support
 * - Automatic retry with exponential backoff
 * - Request timeout enforcement
 * - Built-in error handling
 * - TypeScript types
 *
 * @example
 * ```typescript
 * import { GraphDBClient } from '@graphdb/client';
 *
 * const graphDB = new GraphDBClient({
 *   endpoint: env.GRAPHDB_URL,
 *   apiKey: env.GRAPHDB_API_KEY,
 *   timeout: 5000,
 *   retries: 2,
 * });
 *
 * // Get trust score
 * const trustScore = await graphDB.getTrustScore('user-123');
 *
 * // Detect fraud
 * const fraudRing = await graphDB.findFraudRing('user-suspicious');
 *
 * // Traverse graph
 * const network = await graphDB.traverse({
 *   startNodeId: 'user-123',
 *   edgeTypes: ['TRUSTS', 'VERIFIED_BY'],
 *   maxDepth: 2,
 *   direction: 'outgoing',
 * });
 * ```
 */

export { GraphDBClient } from './client';
export { GraphDBCache } from './cache';
export {
  // Configuration
  GraphDBClientConfig,

  // Core types
  Node,
  Edge,
  NodeProperties,
  QueryResult,

  // Traversal
  TraversalOptions,
  TraversalResult,

  // Syntopica/Cluso use cases
  TrustScore,
  FraudRing,

  // Query options
  QueryOptions,
  GraphQLVariables,
  GraphQLResponse,

  // Mutations
  CreateNodeInput,
  UpdateNodeInput,
  CreateEdgeInput,
  BatchResult,

  // Health & Metrics
  HealthCheckResponse,
  MetricsResponse,

  // Errors
  GraphDBError,
  GraphDBErrorType,
} from './types';

export {
  // Cache configuration and statistics
  CacheConfig,
  CacheStats,
} from './cache';
