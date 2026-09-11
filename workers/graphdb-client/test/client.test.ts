/**
 * GraphDB Client Tests
 *
 * Mocks assert the server's REAL HTTP contract (method, path, headers,
 * body), verified against pkg/api in the root graphdb repo — see the
 * handler citation in each describe block. v1's mocks baked in a
 * fictitious contract (PATCH for updateNode, a `cursor` JSON field for
 * queryNodes, a GraphQL `traverse`/`trustScore`/`fraudRing` field that no
 * resolver defines, `type`/`source`/`target` bodies the server never
 * decoded) that happened to match v1's own (broken) client rather than
 * the server.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { GraphDBClient } from '../src/client';
import { GraphDBError, GraphDBErrorType } from '../src/types';

// Mock fetch globally
global.fetch = vi.fn();

/** A minimal Headers stand-in for mocked fetch Responses. */
function mockHeaders(values: Record<string, string> = {}): Pick<Headers, 'get'> {
  return {
    get: (name: string) => values[name] ?? null,
  };
}

describe('GraphDBClient', () => {
  let client: GraphDBClient;
  const mockEndpoint = 'https://graphdb.example.com';
  const mockApiKey = 'test-api-key';

  beforeEach(() => {
    // resetAllMocks (not clearAllMocks) so queued mock*Once values from a
    // prior test never leak into the next — retries that no longer fire on
    // non-idempotent methods (M-11) leave fewer calls, which would
    // otherwise desync the Once queue.
    vi.resetAllMocks();
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
        data: { user: { id: '123', score: 850 } },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      const result = await client.query(
        'query GetUser($id: ID!) { user(id: $id) { id score } }',
        { id: '123' }
      );

      expect(result).toEqual(mockResponse.data);
    });

    // Addition #8 (PR #585): the plural list field (e.g. `persons`) takes
    // `limit`/`after` — an ID cursor, not an offset. `after` is the ID of
    // the last item seen; a page shorter than `limit` is the last page
    // (pkg/graphql/after_cursor.go parseListPaging, pkg/graphql/limits.go).
    // No client code change was needed for this — the existing generic
    // query() method already sends whatever GraphQL text it is given, so
    // this test demonstrates the walking pattern rather than proving a fix.
    it('pages through persons(limit, after) via query() and stops at a short page', async () => {
      // `properties` is a JSON-encoded string on the wire
      // (pkg/graphql/schema.go createNodeType) — there is no per-property
      // GraphQL field.
      const page1 = {
        data: {
          persons: [
            { id: '1', properties: '{"name":"Alice"}' },
            { id: '2', properties: '{"name":"Bob"}' },
          ],
        },
      };
      // Shorter than the requested limit of 2 — the last page.
      const page2 = { data: { persons: [{ id: '3', properties: '{"name":"Charlie"}' }] } };

      (global.fetch as ReturnType<typeof vi.fn>)
        .mockResolvedValueOnce({ ok: true, status: 200, json: async () => page1 })
        .mockResolvedValueOnce({ ok: true, status: 200, json: async () => page2 });

      const query = `
        query Persons($limit: Int!, $after: ID) {
          persons(limit: $limit, after: $after) { id properties }
        }
      `;
      const limit = 2;
      const collected: Array<{ id: string; properties: string }> = [];
      let after: string | undefined;

      for (;;) {
        const result = await client.query<{
          persons: Array<{ id: string; properties: string }>;
        }>(query, { limit, after });
        collected.push(...result.persons);
        if (result.persons.length < limit) break;
        after = result.persons[result.persons.length - 1]!.id;
      }

      expect(collected).toHaveLength(3);
      expect(global.fetch).toHaveBeenCalledTimes(2);
      // The second request's cursor is the id of the last item of page 1
      // — not an offset or a page number.
      expect(global.fetch).toHaveBeenNthCalledWith(
        2,
        `${mockEndpoint}/graphql`,
        expect.objectContaining({
          body: expect.stringContaining('"after":"2"'),
        })
      );
    });
  });

  describe('Node REST API methods', () => {
    // Verified against pkg/api/handlers_nodes.go and pkg/api/types.go.
    it('should get node by ID', async () => {
      const mockNode = {
        id: 123,
        labels: ['Person'],
        properties: { name: 'Test User' },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockNode,
      });

      const result = await client.getNode(123);
      expect(result).toEqual(mockNode);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes/123`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('should create node with a labels body (NodeRequest), not `type`', async () => {
      const input = {
        labels: ['Person'],
        properties: { name: 'New User' },
      };

      const mockNode = { id: 456, ...input };

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

    // Defect #1: the route only accepts GET, PUT, DELETE
    // (pkg/api/handlers_nodes.go handleNode) — PATCH is rejected 405.
    it('should update node with PUT, not PATCH', async () => {
      const update = {
        properties: { name: 'Updated User' },
      };

      const mockNode = {
        id: 123,
        labels: ['Person'],
        properties: { name: 'Updated User' },
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockNode,
      });

      const result = await client.updateNode(123, update);
      expect(result).toEqual(mockNode);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes/123`,
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify(update),
        })
      );
    });

    // deleteNode responds 200 with a JSON body ({"deleted": id}), not 204
    // (pkg/api/handlers_nodes.go deleteNode) — only the vector-index
    // delete route uses 204.
    it('should delete node', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ deleted: 123 }),
      });

      await client.deleteNode(123);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes/123`,
        expect.objectContaining({ method: 'DELETE' })
      );
    });

    // Defect #2: GET /nodes returns a bare array; the next cursor is in
    // the X-Next-Cursor response header, absent on the last page
    // (pkg/api/pagination.go, pkg/api/handlers_nodes.go listNodes).
    describe('queryNodes', () => {
      it('should return { nodes, cursor } reading the cursor from X-Next-Cursor', async () => {
        const mockNodes = [
          { id: 1, labels: ['Person'], properties: { name: 'User 1' } },
        ];

        (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: mockHeaders({ 'X-Next-Cursor': '1' }),
          json: async () => mockNodes,
        });

        const result = await client.queryNodes(
          { label: 'Person' },
          { limit: 10 }
        );

        expect(result).toEqual({ nodes: mockNodes, cursor: '1' });
        expect(global.fetch).toHaveBeenCalledWith(
          `${mockEndpoint}/nodes?label=Person&limit=10`,
          expect.objectContaining({ method: 'GET' })
        );
      });

      it('should omit cursor when X-Next-Cursor is absent (last page)', async () => {
        const mockNodes = [
          { id: 2, labels: ['Person'], properties: { name: 'User 2' } },
        ];

        (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: mockHeaders(),
          json: async () => mockNodes,
        });

        const result = await client.queryNodes();

        expect(result).toEqual({ nodes: mockNodes });
        expect(result.cursor).toBeUndefined();
      });

      it('should send ?cursor= to fetch the next page', async () => {
        (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
          ok: true,
          status: 200,
          headers: mockHeaders(),
          json: async () => [],
        });

        await client.queryNodes(undefined, { cursor: '42', limit: 5 });

        expect(global.fetch).toHaveBeenCalledWith(
          `${mockEndpoint}/nodes?limit=5&cursor=42`,
          expect.anything()
        );
      });
    });
  });

  describe('Edge REST API methods', () => {
    // Verified against pkg/api/handlers_edges.go and pkg/api/types.go.
    it('should create edge with from_node_id/to_node_id, not source/target', async () => {
      const input = {
        type: 'TRUSTS',
        from_node_id: 123,
        to_node_id: 456,
        properties: { weight: 0.8 },
      };

      const mockEdge = { id: 789, weight: 0, ...input };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => mockEdge,
      });

      const result = await client.createEdge(input);
      expect(result).toEqual(mockEdge);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/edges`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify(input),
        })
      );
    });

    // Addition #5.
    it('should get edge by ID', async () => {
      const mockEdge = {
        id: 789,
        from_node_id: 123,
        to_node_id: 456,
        type: 'TRUSTS',
        properties: {},
        weight: 0.8,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockEdge,
      });

      const result = await client.getEdge(789);
      expect(result).toEqual(mockEdge);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/edges/789`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    // Addition #5.
    it('should update edge with PUT', async () => {
      const update = { properties: { weight: 'high' }, weight: 0.95 };
      const mockEdge = {
        id: 789,
        from_node_id: 123,
        to_node_id: 456,
        type: 'TRUSTS',
        properties: { weight: 'high' },
        weight: 0.95,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockEdge,
      });

      const result = await client.updateEdge(789, update);
      expect(result).toEqual(mockEdge);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/edges/789`,
        expect.objectContaining({
          method: 'PUT',
          body: JSON.stringify(update),
        })
      );
    });

    // Addition #5. Same 200-with-body contract as deleteNode.
    it('should delete edge', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ deleted: 789 }),
      });

      await client.deleteEdge(789);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/edges/789`,
        expect.objectContaining({ method: 'DELETE' })
      );
    });
  });

  // Defect #3: no GraphQL resolver named `traverse` exists anywhere in
  // pkg/graphql. The real route is POST /traverse
  // (pkg/api/handlers_algorithms_traversal.go handleTraversal), and the
  // response is a flat { nodes, count, time, truncated } — no `edges` or
  // `paths` field exists on TraversalResponse.
  describe('traverse', () => {
    it('should call POST /traverse with the server request shape', async () => {
      const mockTraversal = {
        nodes: [
          { id: 1, labels: ['Person'], properties: {} },
          { id: 2, labels: ['Person'], properties: {} },
        ],
        count: 2,
        time: '1.2ms',
        truncated: false,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockTraversal,
      });

      const result = await client.traverse({
        startNodeId: 123,
        edgeTypes: ['TRUSTS', 'VERIFIED_BY'],
        maxDepth: 2,
        direction: 'outgoing',
      });

      expect(result).toEqual(mockTraversal);
      expect(result.nodes).toHaveLength(2);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/traverse`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            start_node_id: 123,
            max_depth: 2,
            edge_types: ['TRUSTS', 'VERIFIED_BY'],
            direction: 'outgoing',
          }),
        })
      );
    });
  });

  // Defect #4: no GraphQL resolver named `trustScore` or `fraudRing`
  // exists on the User type (or anywhere else) in pkg/graphql. These
  // methods never worked against the real server — removed, no
  // typed replacement, since nothing could have depended on them
  // functioning.
  describe('removed v1 methods (defect #4)', () => {
    it('does not expose getTrustScore', () => {
      expect('getTrustScore' in client).toBe(false);
    });

    it('does not expose findFraudRing', () => {
      expect('findFraudRing' in client).toBe(false);
    });
  });

  describe('Batch operations', () => {
    // Bodies verified against pkg/api/types.go BatchNodeRequest/
    // BatchEdgeRequest (which embed NodeRequest/EdgeRequest — the same
    // labels/from_node_id/to_node_id shape as the single-item routes).
    // Response shapes verified against BatchNodeResponse/
    // BatchEdgeResponse — the server never returns a {success, failed}
    // envelope; `failed` is a count, and per-item failures are in
    // `errors` as {index, error}.
    it('should batch create nodes', async () => {
      const inputs = [
        { labels: ['Person'], properties: { name: 'User 1' } },
        { labels: ['Person'], properties: { name: 'User 2' } },
      ];

      const mockResult = {
        nodes: [
          { id: 1, ...inputs[0] },
          { id: 2, ...inputs[1] },
        ],
        created: 2,
        time: '0.5ms',
        failed: 0,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => mockResult,
      });

      const result = await client.batchCreateNodes(inputs);
      expect(result.nodes).toHaveLength(2);
      expect(result.failed).toBe(0);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/nodes/batch`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ nodes: inputs }),
        })
      );
    });

    it('should batch create edges', async () => {
      const inputs = [
        { type: 'TRUSTS', from_node_id: 1, to_node_id: 2 },
        { type: 'TRUSTS', from_node_id: 2, to_node_id: 3 },
      ];

      const mockResult = {
        edges: [
          { id: 1, properties: {}, weight: 0, ...inputs[0] },
          { id: 2, properties: {}, weight: 0, ...inputs[1] },
        ],
        created: 2,
        time: '0.5ms',
        failed: 0,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => mockResult,
      });

      const result = await client.batchCreateEdges(inputs);
      expect(result.edges).toHaveLength(2);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/edges/batch`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ edges: inputs }),
        })
      );
    });
  });

  // Addition #6. Verified against pkg/api/handlers_compliance.go.
  describe('Compliance API', () => {
    it('should get audit log with translated query params', async () => {
      const mockResponse = {
        events: [
          {
            id: 'evt-1',
            timestamp: '2026-09-01T00:00:00Z',
            action: 'read',
            resource_type: 'node',
            status: 'success',
          },
        ],
        count: 1,
        total: 1,
        offset: 0,
        limit: 10,
        has_more: false,
        tenant: 'default',
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockResponse,
      });

      const result = await client.getAuditLog({ userId: 'u1', limit: 10 });
      expect(result).toEqual(mockResponse);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/v1/compliance/audit-log?user_id=u1&limit=10`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('should get a tenant masking policy by path segment', async () => {
      const mockPolicy = {
        tenant_id: 'tenant-a',
        properties: { email: 'hash' },
        auto_detect: false,
        updated_at: '2026-09-01T00:00:00Z',
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockPolicy,
      });

      const result = await client.getMaskingPolicy('tenant-a');
      expect(result).toEqual(mockPolicy);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/v1/compliance/masking-policy/tenant-a`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('should set a masking policy translating autoDetect to auto_detect', async () => {
      const mockPolicy = {
        tenant_id: 'tenant-a',
        properties: { email: 'hash' },
        auto_detect: true,
        updated_at: '2026-09-01T00:00:00Z',
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockPolicy,
      });

      const result = await client.setMaskingPolicy({
        properties: { email: 'hash' },
        autoDetect: true,
      });
      expect(result).toEqual(mockPolicy);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/v1/compliance/masking-policy`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ properties: { email: 'hash' }, auto_detect: true }),
        })
      );
    });
  });

  // Addition #7. Verified against pkg/api/handlers_vectors.go.
  describe('Vector index API', () => {
    it('should list vector indexes', async () => {
      const mockList = {
        indexes: [{ property_name: 'embedding' }],
        count: 1,
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockList,
      });

      const result = await client.listVectorIndexes();
      expect(result).toEqual(mockList);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/vector-indexes`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    it('should create a vector index translating camelCase to the wire body', async () => {
      const mockIndex = {
        property_name: 'embedding',
        dimensions: 128,
        metric: 'cosine',
      };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => mockIndex,
      });

      const result = await client.createVectorIndex({
        propertyName: 'embedding',
        dimensions: 128,
        metric: 'cosine',
      });
      expect(result).toEqual(mockIndex);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/vector-indexes`,
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            property_name: 'embedding',
            dimensions: 128,
            metric: 'cosine',
          }),
        })
      );
    });

    it('should get a vector index by name', async () => {
      const mockIndex = { property_name: 'embedding' };

      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => mockIndex,
      });

      const result = await client.getVectorIndex('embedding');
      expect(result).toEqual(mockIndex);
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/vector-indexes/embedding`,
        expect.objectContaining({ method: 'GET' })
      );
    });

    // deleteVectorIndex is the one delete route that really does 204
    // (pkg/api/handlers_vectors.go deleteVectorIndex) — unlike
    // deleteNode/deleteEdge above.
    it('should delete a vector index (204 No Content)', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: true,
        status: 204,
      });

      await client.deleteVectorIndex('embedding');
      expect(global.fetch).toHaveBeenCalledWith(
        `${mockEndpoint}/vector-indexes/embedding`,
        expect.objectContaining({ method: 'DELETE' })
      );
    });
  });

  describe('Retry logic', () => {
    // Retries are exercised with GET (getNode) — an idempotent method.
    // Non-idempotent methods (query/createNode use POST) are NOT retried
    // (security audit M-11); see the dedicated pins below.
    it('should retry idempotent (GET) requests on network error', async () => {
      (global.fetch as ReturnType<typeof vi.fn>)
        .mockRejectedValueOnce(new Error('Network error'))
        .mockRejectedValueOnce(new Error('Network error'))
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          json: async () => ({ id: 123, labels: [], properties: {} }),
        });

      const result = await client.getNode(123);
      expect(result).toBeDefined();
      expect(global.fetch).toHaveBeenCalledTimes(3);
    });

    it('should retry idempotent (GET) requests on 5xx errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>)
        .mockResolvedValueOnce({
          ok: false,
          status: 503,
          json: async () => ({ error: 'Service unavailable' }),
        })
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          json: async () => ({ id: 123, labels: [], properties: {} }),
        });

      const result = await client.getNode(123);
      expect(result).toBeDefined();
      expect(global.fetch).toHaveBeenCalledTimes(2);
    });

    it('should NOT retry non-idempotent (POST) requests on 5xx (M-11)', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: false,
        status: 503,
        json: async () => ({ error: 'Service unavailable' }),
      });

      await expect(client.query('{ ping }')).rejects.toThrow(GraphDBError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should NOT retry non-idempotent (POST) requests on network error (M-11)', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
        new Error('Network error')
      );

      await expect(client.query('{ ping }')).rejects.toThrow(GraphDBError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should not retry on 4xx errors', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
        ok: false,
        status: 404,
        json: async () => ({ error: 'Not found' }),
      });

      await expect(client.getNode(999)).rejects.toThrow(GraphDBError);
      expect(global.fetch).toHaveBeenCalledTimes(1);
    });

    it('should throw after max retries (idempotent GET)', async () => {
      (global.fetch as ReturnType<typeof vi.fn>).mockRejectedValue(
        new Error('Network error')
      );

      await expect(client.getNode(123)).rejects.toThrow(GraphDBError);
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

      // Honor the abort signal so the 100ms timeout actually rejects the
      // in-flight request (the client's AbortController fires at timeout).
      (global.fetch as ReturnType<typeof vi.fn>).mockImplementation(
        (_url: string, opts: RequestInit) =>
          new Promise((_resolve, reject) => {
            opts.signal?.addEventListener('abort', () => {
              const err = new Error('aborted');
              err.name = 'AbortError';
              reject(err);
            });
          })
      );

      await expect(slowClient.getNode(123)).rejects.toThrow();
    }, 10000);

    it('should retry idempotent (GET) requests on timeout errors', async () => {
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
            json: async () => ({ id: 123, labels: [], properties: {} }),
          });
        }
      );

      const result = await clientWithRetries.getNode(123);
      expect(result).toBeDefined();
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

      await expect(client.getNode(123)).rejects.toThrow(
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

      await expect(client.getNode(999)).rejects.toThrow(
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
        client.createNode({ labels: [], properties: {} })
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

      await expect(client.getNode(123)).rejects.toThrow(
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
