/**
 * GraphDB Client for Cloudflare Workers
 * Core client implementation
 */

import {
  GraphDBClientConfig,
  GraphQLResponse,
  GraphQLVariables,
  GraphDBError,
  GraphDBErrorType,
  Node,
  Edge,
  QueryResult,
  TraversalOptions,
  TraversalResult,
  TrustScore,
  FraudRing,
  QueryOptions,
  CreateNodeInput,
  UpdateNodeInput,
  CreateEdgeInput,
  BatchResult,
  HealthCheckResponse,
  MetricsResponse,
} from './types';

/**
 * GraphDB Client for Cloudflare Workers
 *
 * Optimized for Cloudflare Workers environment with:
 * - Automatic retry with exponential backoff
 * - Request timeout enforcement (critical for Workers 50-500ms limits)
 * - GraphQL primary, REST fallback
 * - Built-in error handling
 *
 * @example
 * ```typescript
 * const graphDB = new GraphDBClient({
 *   endpoint: env.GRAPHDB_URL,
 *   apiKey: env.GRAPHDB_API_KEY,
 *   timeout: 5000,
 *   retries: 2,
 * });
 *
 * const trustScore = await graphDB.getTrustScore('user-123');
 * ```
 */
export class GraphDBClient {
  private config: Required<GraphDBClientConfig>;

  constructor(config: GraphDBClientConfig) {
    this.config = {
      endpoint: config.endpoint.replace(/\/$/, ''), // Remove trailing slash
      apiKey: config.apiKey || '',
      jwtToken: config.jwtToken || '',
      timeout: config.timeout || 5000,
      retries: config.retries || 2,
      retryDelay: config.retryDelay || 100,
      enableGraphQL: config.enableGraphQL !== false,
      enableREST: config.enableREST !== false,
      headers: config.headers || {},
    };

    if (!this.config.apiKey && !this.config.jwtToken) {
      console.warn('[GraphDB] No authentication provided. Requests may fail.');
    }
  }

  /**
   * Execute a GraphQL query
   */
  async query<T = unknown>(
    query: string,
    variables?: GraphQLVariables
  ): Promise<T> {
    if (!this.config.enableGraphQL) {
      throw new GraphDBError(
        'GraphQL is disabled',
        GraphDBErrorType.ValidationError
      );
    }

    const response = await this.executeGraphQL<T>(query, variables);

    if (response.errors && response.errors.length > 0) {
      throw new GraphDBError(
        response.errors[0].message,
        GraphDBErrorType.GraphQLError,
        undefined,
        response.errors
      );
    }

    if (!response.data) {
      throw new GraphDBError(
        'No data returned from GraphQL query',
        GraphDBErrorType.GraphQLError
      );
    }

    return response.data;
  }

  /**
   * Execute a GraphQL mutation
   */
  async mutate<T = unknown>(
    mutation: string,
    variables?: GraphQLVariables
  ): Promise<T> {
    return this.query<T>(mutation, variables);
  }

  /**
   * Get a node by ID (REST API)
   */
  async getNode(id: string): Promise<Node> {
    return this.request<Node>('GET', `/nodes/${id}`);
  }

  /**
   * Create a new node (REST API)
   */
  async createNode(input: CreateNodeInput): Promise<Node> {
    return this.request<Node>('POST', '/nodes', input);
  }

  /**
   * Update a node (REST API)
   */
  async updateNode(id: string, input: UpdateNodeInput): Promise<Node> {
    return this.request<Node>('PATCH', `/nodes/${id}`, input);
  }

  /**
   * Delete a node (REST API)
   */
  async deleteNode(id: string): Promise<void> {
    await this.request<void>('DELETE', `/nodes/${id}`);
  }

  /**
   * Query nodes with filters (REST API)
   */
  async queryNodes(
    filters?: Record<string, unknown>,
    options?: QueryOptions
  ): Promise<QueryResult<Node>> {
    const params = new URLSearchParams();

    if (options?.limit) params.set('limit', options.limit.toString());
    if (options?.offset) params.set('offset', options.offset.toString());
    if (options?.cursor) params.set('cursor', options.cursor);
    if (options?.sortBy) params.set('sortBy', options.sortBy);
    if (options?.sortOrder) params.set('sortOrder', options.sortOrder);

    if (filters) {
      params.set('filter', JSON.stringify(filters));
    }

    const query = params.toString();
    const url = query ? `/nodes?${query}` : '/nodes';

    return this.request<QueryResult<Node>>('GET', url);
  }

  /**
   * Create an edge between two nodes (REST API)
   */
  async createEdge(input: CreateEdgeInput): Promise<Edge> {
    return this.request<Edge>('POST', '/edges', input);
  }

  /**
   * Traverse the graph from a starting node
   *
   * @example
   * ```typescript
   * const trustNetwork = await graphDB.traverse({
   *   startNodeId: 'user-123',
   *   edgeTypes: ['VERIFIED_BY', 'TRUSTS'],
   *   maxDepth: 2,
   *   direction: 'outgoing',
   * });
   * ```
   */
  async traverse(options: TraversalOptions): Promise<TraversalResult> {
    const query = `
      query TraverseGraph($startNodeId: ID!, $edgeTypes: [String!], $maxDepth: Int!, $direction: String!, $limit: Int) {
        traverse(
          startNodeId: $startNodeId
          edgeTypes: $edgeTypes
          maxDepth: $maxDepth
          direction: $direction
          limit: $limit
        ) {
          nodes {
            id
            type
            properties
          }
          edges {
            id
            type
            source
            target
            properties
          }
          paths {
            nodes
            edges
          }
        }
      }
    `;

    const variables = {
      startNodeId: options.startNodeId,
      edgeTypes: options.edgeTypes || [],
      maxDepth: options.maxDepth,
      direction: options.direction,
      limit: options.limit,
    };

    const result = await this.query<{ traverse: TraversalResult }>(query, variables);
    return result.traverse;
  }

  /**
   * Get trust score for a user (Syntopica use case)
   *
   * @example
   * ```typescript
   * const trustScore = await graphDB.getTrustScore('user-123');
   * console.log(trustScore.score); // 847
   * ```
   */
  async getTrustScore(userId: string): Promise<TrustScore> {
    const query = `
      query GetTrustScore($userId: ID!) {
        user(id: $userId) {
          id
          trustScore
          trustComponents {
            verification
            activity
            reputation
          }
          lastUpdated
        }
      }
    `;

    const result = await this.query<{ user: TrustScore }>(query, { userId });
    return result.user;
  }

  /**
   * Detect fraud ring (Cluso use case)
   *
   * @example
   * ```typescript
   * const fraudRing = await graphDB.findFraudRing('user-suspicious');
   * if (fraudRing.suspicionScore > 0.8) {
   *   console.log('High fraud risk detected!');
   * }
   * ```
   */
  async findFraudRing(userId: string): Promise<FraudRing> {
    const query = `
      query FindFraudRing($userId: ID!) {
        user(id: $userId) {
          id
          fraudRing {
            nodes {
              id
              type
              properties
            }
            edges {
              id
              type
              source
              target
              properties
            }
            suspicionScore
            reasons
          }
        }
      }
    `;

    const result = await this.query<{ user: { fraudRing: FraudRing } }>(query, { userId });
    return result.user.fraudRing;
  }

  /**
   * Batch create nodes (REST API)
   */
  async batchCreateNodes(inputs: CreateNodeInput[]): Promise<BatchResult<Node>> {
    return this.request<BatchResult<Node>>('POST', '/nodes/batch', { nodes: inputs });
  }

  /**
   * Batch create edges (REST API)
   */
  async batchCreateEdges(inputs: CreateEdgeInput[]): Promise<BatchResult<Edge>> {
    return this.request<BatchResult<Edge>>('POST', '/edges/batch', { edges: inputs });
  }

  /**
   * Health check
   */
  async healthCheck(): Promise<HealthCheckResponse> {
    return this.request<HealthCheckResponse>('GET', '/health');
  }

  /**
   * Get metrics
   */
  async getMetrics(): Promise<MetricsResponse> {
    return this.request<MetricsResponse>('GET', '/metrics');
  }

  /**
   * Execute GraphQL request (internal)
   */
  private async executeGraphQL<T>(
    query: string,
    variables?: GraphQLVariables
  ): Promise<GraphQLResponse<T>> {
    const body = JSON.stringify({ query, variables });

    const response = await this.fetchWithRetry('/graphql', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...this.getAuthHeaders(),
        ...this.config.headers,
      },
      body,
    });

    return response.json();
  }

  /**
   * Execute REST API request (internal)
   */
  private async request<T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<T> {
    if (!this.config.enableREST) {
      throw new GraphDBError(
        'REST API is disabled',
        GraphDBErrorType.ValidationError
      );
    }

    const options: RequestInit = {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...this.getAuthHeaders(),
        ...this.config.headers,
      },
    };

    if (body) {
      options.body = JSON.stringify(body);
    }

    const response = await this.fetchWithRetry(path, options);

    if (response.status === 204) {
      return undefined as T;
    }

    return response.json();
  }

  /**
   * Fetch with retry logic and timeout (internal)
   */
  private async fetchWithRetry(
    path: string,
    options: RequestInit,
    attempt = 0
  ): Promise<Response> {
    const url = `${this.config.endpoint}${path}`;
    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), this.config.timeout);

    try {
      const response = await fetch(url, {
        ...options,
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      // Handle error responses
      if (!response.ok) {
        return this.handleErrorResponse(response, path, options, attempt);
      }

      return response;
    } catch (error) {
      clearTimeout(timeoutId);

      // Handle timeout
      if (error instanceof Error && error.name === 'AbortError') {
        if (attempt < this.config.retries) {
          return this.retryRequest(path, options, attempt);
        }
        throw new GraphDBError(
          `Request timeout after ${this.config.timeout}ms`,
          GraphDBErrorType.TimeoutError
        );
      }

      // Handle network errors
      if (attempt < this.config.retries) {
        return this.retryRequest(path, options, attempt);
      }

      throw new GraphDBError(
        `Network error: ${error instanceof Error ? error.message : 'Unknown error'}`,
        GraphDBErrorType.NetworkError,
        undefined,
        error
      );
    }
  }

  /**
   * Handle error response (internal)
   */
  private async handleErrorResponse(
    response: Response,
    path: string,
    options: RequestInit,
    attempt: number
  ): Promise<Response> {
    const statusCode = response.status;

    // Retry on 5xx errors
    if (statusCode >= 500 && attempt < this.config.retries) {
      return this.retryRequest(path, options, attempt);
    }

    // Parse error message
    let errorMessage = `HTTP ${statusCode}`;
    let errorDetails: unknown;

    try {
      const errorBody = await response.json() as { error?: string; message?: string };
      errorMessage = errorBody.error || errorBody.message || errorMessage;
      errorDetails = errorBody;
    } catch {
      errorMessage = await response.text() || errorMessage;
    }

    // Determine error type
    let errorType = GraphDBErrorType.ServerError;
    if (statusCode === 401 || statusCode === 403) {
      errorType = GraphDBErrorType.AuthenticationError;
    } else if (statusCode === 404) {
      errorType = GraphDBErrorType.NotFoundError;
    } else if (statusCode >= 400 && statusCode < 500) {
      errorType = GraphDBErrorType.ValidationError;
    }

    throw new GraphDBError(errorMessage, errorType, statusCode, errorDetails);
  }

  /**
   * Retry request with exponential backoff (internal)
   */
  private async retryRequest(
    path: string,
    options: RequestInit,
    attempt: number
  ): Promise<Response> {
    const delay = this.config.retryDelay * Math.pow(2, attempt);
    await new Promise((resolve) => setTimeout(resolve, delay));
    return this.fetchWithRetry(path, options, attempt + 1);
  }

  /**
   * Get authentication headers (internal)
   */
  private getAuthHeaders(): Record<string, string> {
    const headers: Record<string, string> = {};

    if (this.config.apiKey) {
      headers['X-API-Key'] = this.config.apiKey;
    } else if (this.config.jwtToken) {
      headers['Authorization'] = `Bearer ${this.config.jwtToken}`;
    }

    return headers;
  }
}
