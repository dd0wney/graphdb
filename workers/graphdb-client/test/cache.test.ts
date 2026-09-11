/**
 * KV Cache Wrapper Tests (TDD)
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { GraphDBClient } from '../src/client';
import { GraphDBCache } from '../src/cache';
import type { Node } from '../src/types';

// Mock KVNamespace
interface MockKVNamespace {
  get: ReturnType<typeof vi.fn>;
  put: ReturnType<typeof vi.fn>;
  delete: ReturnType<typeof vi.fn>;
}

// Mock GraphDB client
vi.mock('../src/client');

describe('GraphDBCache', () => {
  let cache: GraphDBCache;
  let mockKV: MockKVNamespace;
  let mockClient: GraphDBClient;

  beforeEach(() => {
    // Create mock KV
    mockKV = {
      get: vi.fn(),
      put: vi.fn(),
      delete: vi.fn(),
    };

    // Create mock client
    mockClient = {
      getNode: vi.fn(),
      traverse: vi.fn(),
    } as unknown as GraphDBClient;

    cache = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace);
  });

  describe('constructor', () => {
    it('should create cache instance', () => {
      expect(cache).toBeInstanceOf(GraphDBCache);
    });

    it('should accept custom TTL configuration', () => {
      const customCache = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace, {
        defaultTTL: 7200,
        nodeTTL: 150,
      });
      expect(customCache).toBeInstanceOf(GraphDBCache);
    });

    it('namespaces cache keys by identity (H-11)', () => {
      // A KV namespace is Worker-global; without an identity prefix two
      // tenants sharing a Worker would read each other's cached graph data.
      const a = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace, {
        namespace: 'tenant-a',
      });
      const b = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace, {
        namespace: 'tenant-b',
      });
      const plain = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace);

      expect(a.generateKey('node', 1)).toBe('tenant-a:node:1');
      expect(a.generateKey('node', 1)).not.toBe(b.generateKey('node', 1));
      // Default (no namespace) preserves the legacy un-prefixed key.
      expect(plain.generateKey('node', 1)).toBe('node:1');
    });
  });

  describe('getNode with cache', () => {
    const nodeId = 123;
    const mockNode: Node = {
      id: nodeId,
      labels: ['Person'],
      properties: { name: 'Test User' },
    };

    it('should return cached node on cache hit', async () => {
      mockKV.get.mockResolvedValue(mockNode);

      const result = await cache.getNode(nodeId);

      expect(result).toEqual(mockNode);
      expect(mockKV.get).toHaveBeenCalledWith(`node:${nodeId}`, 'json');
      expect(mockClient.getNode).not.toHaveBeenCalled();
    });

    it('should fetch from GraphDB on cache miss', async () => {
      mockKV.get.mockResolvedValue(null);
      (mockClient.getNode as ReturnType<typeof vi.fn>).mockResolvedValue(mockNode);

      const result = await cache.getNode(nodeId);

      expect(result).toEqual(mockNode);
      expect(mockClient.getNode).toHaveBeenCalledWith(nodeId);
      expect(mockKV.put).toHaveBeenCalled();
    });

    it('should track cache hits and misses', async () => {
      mockKV.get.mockResolvedValueOnce(null);
      (mockClient.getNode as ReturnType<typeof vi.fn>).mockResolvedValue(mockNode);
      await cache.getNode(nodeId);

      mockKV.get.mockResolvedValueOnce(mockNode);
      await cache.getNode(nodeId);

      const stats = cache.getStats();
      expect(stats.hits).toBe(1);
      expect(stats.misses).toBe(1);
      expect(stats.hitRate).toBeCloseTo(0.5);
    });
  });

  describe('cache invalidation', () => {
    it('should invalidate node cache', async () => {
      const nodeId = 123;

      await cache.invalidateNode(nodeId);

      expect(mockKV.delete).toHaveBeenCalledWith(`node:${nodeId}`);
    });

    it('should invalidate multiple keys', async () => {
      await cache.invalidateMultiple(['node:1', 'node:2', 'node:3']);

      expect(mockKV.delete).toHaveBeenCalledTimes(3);
      expect(mockKV.delete).toHaveBeenCalledWith('node:1');
      expect(mockKV.delete).toHaveBeenCalledWith('node:2');
      expect(mockKV.delete).toHaveBeenCalledWith('node:3');
    });
  });

  describe('cache statistics', () => {
    it('should reset statistics', () => {
      cache.resetStats();

      const stats = cache.getStats();
      expect(stats.hits).toBe(0);
      expect(stats.misses).toBe(0);
      expect(stats.hitRate).toBe(0);
    });
  });

  describe('error handling', () => {
    it('should handle KV get errors gracefully', async () => {
      const nodeId = 123;
      const mockNode: Node = { id: nodeId, labels: ['Person'], properties: {} };
      mockKV.get.mockRejectedValue(new Error('KV error'));
      (mockClient.getNode as ReturnType<typeof vi.fn>).mockResolvedValue(mockNode);

      // Should fall back to fetching from GraphDB
      const result = await cache.getNode(nodeId);

      expect(result).toBeDefined();
      expect(mockClient.getNode).toHaveBeenCalled();
    });

    it('should handle KV put errors gracefully', async () => {
      const nodeId = 123;
      const mockNode: Node = { id: nodeId, labels: ['Person'], properties: {} };
      mockKV.get.mockResolvedValue(null);
      mockKV.put.mockRejectedValue(new Error('KV put error'));
      (mockClient.getNode as ReturnType<typeof vi.fn>).mockResolvedValue(mockNode);

      // Should still return result even if cache write fails
      const result = await cache.getNode(nodeId);

      expect(result).toBeDefined();
    });
  });

  describe('cache key generation', () => {
    it('should generate consistent cache keys', () => {
      const key1 = cache.generateKey('node', 123);
      const key2 = cache.generateKey('node', 123);

      expect(key1).toBe(key2);
      expect(key1).toBe('node:123');
    });

    it('should generate different keys for different types', () => {
      const nodeKey = cache.generateKey('node', 123);
      const traversalKey = cache.generateKey('traversal', 123);

      expect(nodeKey).not.toBe(traversalKey);
      expect(nodeKey).toBe('node:123');
      expect(traversalKey).toBe('traversal:123');
    });
  });

  describe('TTL configuration', () => {
    it('should use different TTLs for different data types', async () => {
      const customCache = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace, {
        nodeTTL: 300, // 5 minutes
        traversalTTL: 600, // 10 minutes
      });

      mockKV.get.mockResolvedValue(null);

      (mockClient.getNode as ReturnType<typeof vi.fn>).mockResolvedValue({
        id: 123,
        labels: ['Person'],
        properties: {},
      });
      await customCache.getNode(123);
      expect(mockKV.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(String),
        { expirationTtl: 300 }
      );

      (mockClient.traverse as ReturnType<typeof vi.fn>).mockResolvedValue({
        nodes: [],
        count: 0,
        time: '0ms',
      });
      await customCache.traverse(123, ['TRUSTS'], 2, 'outgoing');
      expect(mockKV.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(String),
        { expirationTtl: 600 }
      );
    });
  });
});
