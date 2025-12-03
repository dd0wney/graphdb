/**
 * GraphDB Client Tests
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { GraphDBClient } from '../src/client';
import { GraphDBError, GraphDBErrorType } from '../src/types';

// Mock fetch globally
global.fetch = vi.fn();

describe('GraphDBClient', () => {
  let client: GraphDBClient;
  const mockEndpoint = 'https://graphdb.example.com';
  const mockApiKey = 'test-api-key';

  beforeEach(() => {
    vi.clearAllMocks();
    client = new GraphDBClient({
      endpoint: mockEndpoint,
      apiKey: mockApiKey,
      timeout: 5000,
      retries: 2,
    });
  });

  describe('constructor', () => {
    it('should create client with default config', () => {
      const defaultClient = new GraphDBClient({
        endpoint: mockEndpoint,
      });
      expect(defaultClient).toBeInstanceOf(GraphDBClient);
    });

    it('should remove trailing slash from endpoint', () => {
      const clientWithSlash = new GraphDBClient({
        endpoint: 'https://graphdb.example.com/',
        apiKey: mockApiKey,
      });
      expect(clientWithSlash).toBeInstanceOf(GraphDBClient);
    });

    it('should warn when no authentication is provided', () => {
      const consoleSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      new GraphDBClient({ endpoint: mockEndpoint });
      expect(consoleSpy).toHaveBeenCalledWith(
        expect.stringContaining('No authentication provided')
      );
      consoleSpy.mockRestore();
    });
  });

  describe('GraphQL queries', () => {
    it('should execute GraphQL query successfully', async () => {
      const mockResponse = {
        data: { user: { id: '123', name: 'Test User' } },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      const result = await client.query('{ user(id: "123") { id name } }');
      expect(result).toEqual(mockResponse.data);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/graphql`,
        expect.objectContaining({
          method: 'POST',
          headers: expect.objectContaining({
            'Content-Type': 'application/json',
            'X-API-Key': mockApiKey,
          }),
        })
      );
    });

    it('should handle GraphQL errors', async () => {
      const mockResponse = {
        errors: [{ message: 'User not found' }],
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      await expect(
        client.query('{ user(id: "999") { id } }')
      ).rejects.toThrow(GraphDBError);
    });

    it('should handle variables in GraphQL query', async () => {
      const mockResponse = {
        data: { user: { id: '123', trustScore: 850 } },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      const result = await client.query(
        'query GetUser($id: ID!) { user(id: $id) { id trustScore } }',
        { id: '123' }
      );

      expect(result).toEqual(mockResponse.data);
    });
  });

  describe('REST API methods', () => {
    it('should get node by ID', async () => {
      const mockNode = {
        id: '123',
        type: 'user',
        properties: { name: 'Test User' },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockNode,
      });

      const result = await client.getNode('123');
      expect(result).toEqual(mockNode);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes/123`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('should create node', async () => {
      const input = {
        type: 'user',
        properties: { name: 'New User' },
      };

      const mockNode = {
        id: '456',
        ...input,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => mockNode,
      });

      const result = await client.createNode(input);
      expect(result).toEqual(mockNode);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify(input),
        })
      );
    });

    it('should update node', async () => {
      const update = {
        properties: { name: 'Updated User' },
      };

      const mockNode = {
        id: '123',
        type: 'user',
        properties: { name: 'Updated User' },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockNode,
      });

      const result = await client.updateNode('123', update);
      expect(result).toEqual(mockNode);
    });

    it('should delete node', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 204,
      });

      await client.deleteNode('123');
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes/123`,
        expect.objectContaining({ method: 'DELETE' })
      );
    });

    it('should query nodes with filters', async () => {
      const mockResult = {
        data: [{ id: '1', type: 'user', properties: { name: 'User 1' } }],
        total: 1,
        hasMore: false,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResult,
      });

      const result = await client.queryNodes(
        { type: 'user' },
        { limit: 10, offset: 0 }
      );

      expect(result).toEqual(mockResult);
    });

    it('should create edge', async () => {
      const input = {
        type: 'TRUSTS',
        source: '123',
        target: '456',
        properties: { weight: 0.8 },
      };

      const mockEdge = {
        id: 'edge-789',
        ...input,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => mockEdge,
      });

      const result = await client.createEdge(input);
      expect(result).toEqual(mockEdge);
    });
  });

  describe('Retry logic', () => {
    it('should retry on network error', async () => {
      (global.fetch as ReturnType<typeof vi.fn>)
        .mockRejectedValueOnce(new Error('Network error'))
        .mockRejectedValueOnce(new Error('Network error'))
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          json: async () => ({ data: { success: true } }),
        });

      const result = await client.query('{ ping }');
      expect(result).toEqual({ success: true });
      expect(global.fetch).toHaveBeenCalledTimes(3);
    });

    it('should retry on 5xx errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>)
        .mockResolvedValueOnce({
          ok: false,
          status: 503,
          json: async () => ({ error: 'Service unavailable' }),
        })
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          json: async () => ({ data: { success: true } }),
        });

      const result = await client.query('{ ping }');
      expect(result).toEqual({ success: true });
      expect(global.fetch).toHaveBeenCalledTimes(2);
    });

    it('should not retry on 4xx errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: false,
        status: 404,
        json: async () => ({ error: 'Not found' }),
      });

      await expect(client.getNode('999')).rejects.toThrow(GraphDBError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should throw after max retries', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockRejectedValue(
        new Error('Network error')
      );

      await expect(client.query('{ ping }')).rejects.toThrow(GraphDBError);
      expect(global.fetch).toHaveBeenCalledTimes(3); // Initial + 2 retries
    });
  });

  describe('Timeout handling', () => {
    it('should timeout after configured duration', async () => {
      const slowClient = new GraphDBClient({
        endpoint: mockEndpoint,
        apiKey: mockApiKey,
        timeout: 100, // 100ms timeout
        retries: 0,
      });

      (global.fetch as ReturnType<typeof vi.fn>).mockImplementation(
        () =>
          new Promise((resolve) => {
            setTimeout(() => resolve({ ok: true, status: 200 }), 200); // Takes 200ms
          })
      );

      await expect(slowClient.query('{ slow }')).rejects.toThrow();
    }, 10000);

    it('should retry on timeout errors', async () => {
      const clientWithRetries = new GraphDBClient({
        endpoint: mockEndpoint,
        apiKey: mockApiKey,
        timeout: 100,
        retries: 2,
      });

      let callCount = 0;
      (global.fetch as ReturnType<typeof vi.fn>).mockImplementation(
        () => {
          callCount++;
          // First two calls timeout, third succeeds
          if (callCount < 3) {
            return new Promise((_, reject) => {
              setTimeout(() => {
                const error = new Error('Timeout');
                error.name = 'AbortError';
                reject(error);
              }, 50);
            });
          }
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ data: { success: true } }),
          });
        }
      );

      const result = await clientWithRetries.query('{ ping }');
      expect(result).toEqual({ success: true });
      expect(callCount).toBe(3); // 2 timeouts + 1 success
    }, 10000);
  });

  describe('Error handling', () => {
    it('should handle authentication errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: async () => ({ error: 'Unauthorized' }),
      });

      await expect(client.getNode('123')).rejects.toThrow(
        expect.objectContaining({
          type: GraphDBErrorType.AuthenticationError,
          statusCode: 401,
        })
      );
    });

    it('should handle not found errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: false,
        status: 404,
        json: async () => ({ error: 'Node not found' }),
      });

      await expect(client.getNode('999')).rejects.toThrow(
        expect.objectContaining({
          type: GraphDBErrorType.NotFoundError,
          statusCode: 404,
        })
      );
    });

    it('should handle validation errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: false,
        status: 400,
        json: async () => ({ error: 'Invalid input' }),
      });

      await expect(
        client.createNode({ type: '', properties: {} })
      ).rejects.toThrow(
        expect.objectContaining({
          type: GraphDBErrorType.ValidationError,
        })
      );
    });

    it('should handle non-JSON error responses', async () => {
      vi.clearAllMocks(); // Clear any previous mocks

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValue({
        ok: false,
        status: 500,
        json: vi.fn().mockRejectedValue(new Error('Invalid JSON')),
        text: vi.fn().mockResolvedValue('Internal Server Error'),
      });

      await expect(client.getNode('123')).rejects.toThrow(
        expect.objectContaining({
          message: 'Internal Server Error',
          type: GraphDBErrorType.ServerError,
          statusCode: 500,
        })
      );
    });
  });

  describe('Authentication', () => {
    it('should use API key header', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: {} }),
      });

      await client.query('{ ping }');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            'X-API-Key': mockApiKey,
          }),
        })
      );
    });

    it('should use JWT token header when provided', async () => {
      const jwtClient = new GraphDBClient({
        endpoint: mockEndpoint,
        jwtToken: 'test-jwt-token',
      });

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: {} }),
      });

      await jwtClient.query('{ ping }');
      expect(global.fetch).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer test-jwt-token',
          }),
        })
      );
    });
  });

  describe('Syntopica/Cluso use cases', () => {
    it('should get trust score', async () => {
      const mockTrustScore = {
        userId: 'user-123',
        score: 847,
        components: {
          verification: 0.9,
          activity: 0.85,
          reputation: 0.83,
        },
        lastUpdated: '2025-11-19T10:00:00Z',
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { user: mockTrustScore } }),
      });

      const result = await client.getTrustScore('user-123');
      expect(result).toEqual(mockTrustScore);
    });

    it('should find fraud ring', async () => {
      const mockFraudRing = {
        nodes: [{ id: '1', type: 'user', properties: {} }],
        edges: [{ id: 'e1', type: 'SIMILAR_BEHAVIOR', source: '1', target: '2', properties: {} }],
        suspicionScore: 0.85,
        reasons: ['Similar IP addresses', 'Coordinated activity'],
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { user: { fraudRing: mockFraudRing } } }),
      });

      const result = await client.findFraudRing('user-suspicious');
      expect(result.suspicionScore).toBeGreaterThan(0.8);
      expect(result.reasons).toHaveLength(2);
    });

    it('should traverse graph', async () => {
      const mockTraversal = {
        nodes: [
          { id: '1', type: 'user', properties: {} },
          { id: '2', type: 'user', properties: {} },
        ],
        edges: [
          { id: 'e1', type: 'TRUSTS', source: '1', target: '2', properties: {} },
        ],
        paths: [{ nodes: ['1', '2'], edges: ['e1'] }],
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { traverse: mockTraversal } }),
      });

      const result = await client.traverse({
        startNodeId: 'user-123',
        edgeTypes: ['TRUSTS', 'VERIFIED_BY'],
        maxDepth: 2,
        direction: 'outgoing',
      });

      expect(result.nodes).toHaveLength(2);
      expect(result.edges).toHaveLength(1);
    });
  });

  describe('Batch operations', () => {
    it('should batch create nodes', async () => {
      const inputs = [
        { type: 'user', properties: { name: 'User 1' } },
        { type: 'user', properties: { name: 'User 2' } },
      ];

      const mockResult = {
        success: [
          { id: '1', ...inputs[0] },
          { id: '2', ...inputs[1] },
        ],
        failed: [],
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResult,
      });

      const result = await client.batchCreateNodes(inputs);
      expect(result.success).toHaveLength(2);
      expect(result.failed).toHaveLength(0);
    });

    it('should batch create edges', async () => {
      const inputs = [
        { type: 'TRUSTS', source: '1', target: '2' },
        { type: 'TRUSTS', source: '2', target: '3' },
      ];

      const mockResult = {
        success: [
          { id: 'e1', ...inputs[0], properties: {} },
          { id: 'e2', ...inputs[1], properties: {} },
        ],
        failed: [],
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResult,
      });

      const result = await client.batchCreateEdges(inputs);
      expect(result.success).toHaveLength(2);
    });
  });

  describe('Health and metrics', () => {
    it('should check health', async () => {
      const mockHealth = {
        status: 'ok' as const,
        version: '1.0.0',
        uptime: 3600,
        timestamp: '2025-11-19T10:00:00Z',
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockHealth,
      });

      const result = await client.healthCheck();
      expect(result.status).toBe('ok');
    });

    it('should get metrics', async () => {
      const mockMetrics = {
        nodes_total: 1000,
        edges_total: 5000,
        active_queries: 5,
        cache_hit_rate: 0.95,
        avg_query_latency_ms: 45,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockMetrics,
      });

      const result = await client.getMetrics();
      expect(result.cache_hit_rate).toBeGreaterThan(0.9);
    });
  });
});
