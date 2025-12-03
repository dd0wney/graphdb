/**
 * KV Cache Wrapper Tests (TDD)
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { GraphDBClient } from '../src/client';
import { GraphDBCache } from '../src/cache';
import type { TrustScore, Node } from '../src/types';

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
      getTrustScore: vi.fn(),
      getNode: vi.fn(),
      traverse: vi.fn(),
      findFraudRing: vi.fn(),
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
        trustScoreTTL: 1800,
      });
      expect(customCache).toBeInstanceOf(GraphDBCache);
    });
  });

  describe('getTrustScore with cache', () => {
    const userId = 'user-123';
    const mockTrustScore: TrustScore = {
      userId,
      score: 847,
      components: {
        verification: 0.9,
        activity: 0.85,
        reputation: 0.83,
      },
      lastUpdated: '2025-11-19T12:00:00Z',
    };

    it('should return cached trust score on cache hit', async () => {
      mockKV.get.mockResolvedValue(mockTrustScore); // Return parsed object

      const result = await cache.getTrustScore(userId);

      expect(result).toEqual(mockTrustScore);
      expect(mockKV.get).toHaveBeenCalledWith(`trust:${userId}`, 'json');
      expect(mockClient.getTrustScore).not.toHaveBeenCalled();
    });

    it('should fetch from GraphDB on cache miss', async () => {
      mockKV.get.mockResolvedValue(null); // Cache miss
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue(mockTrustScore);

      const result = await cache.getTrustScore(userId);

      expect(result).toEqual(mockTrustScore);
      expect(mockKV.get).toHaveBeenCalledWith(`trust:${userId}`, 'json');
      expect(mockClient.getTrustScore).toHaveBeenCalledWith(userId);
    });

    it('should cache result after fetch', async () => {
      mockKV.get.mockResolvedValue(null);
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue(mockTrustScore);

      await cache.getTrustScore(userId);

      expect(mockKV.put).toHaveBeenCalledWith(
        `trust:${userId}`,
        JSON.stringify(mockTrustScore),
        { expirationTtl: 3600 } // Default trust score TTL
      );
    });

    it('should use custom TTL if provided', async () => {
      const customCache = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace, {
        trustScoreTTL: 7200,
      });

      mockKV.get.mockResolvedValue(null);
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue(mockTrustScore);

      await customCache.getTrustScore(userId);

      expect(mockKV.put).toHaveBeenCalledWith(
        `trust:${userId}`,
        JSON.stringify(mockTrustScore),
        { expirationTtl: 7200 }
      );
    });
  });

  describe('getNode with cache', () => {
    const nodeId = 'node-123';
    const mockNode: Node = {
      id: nodeId,
      type: 'user',
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
  });

  describe('cache invalidation', () => {
    it('should invalidate trust score cache', async () => {
      const userId = 'user-123';

      await cache.invalidateTrustScore(userId);

      expect(mockKV.delete).toHaveBeenCalledWith(`trust:${userId}`);
    });

    it('should invalidate node cache', async () => {
      const nodeId = 'node-123';

      await cache.invalidateNode(nodeId);

      expect(mockKV.delete).toHaveBeenCalledWith(`node:${nodeId}`);
    });

    it('should invalidate multiple keys', async () => {
      await cache.invalidateMultiple(['trust:user-1', 'trust:user-2', 'node:node-1']);

      expect(mockKV.delete).toHaveBeenCalledTimes(3);
      expect(mockKV.delete).toHaveBeenCalledWith('trust:user-1');
      expect(mockKV.delete).toHaveBeenCalledWith('trust:user-2');
      expect(mockKV.delete).toHaveBeenCalledWith('node:node-1');
    });
  });

  describe('cache statistics', () => {
    it('should track cache hits and misses', async () => {
      const userId = 'user-123';
      const mockTrustScore: TrustScore = {
        userId,
        score: 850,
        components: { verification: 0.9, activity: 0.8, reputation: 0.85 },
        lastUpdated: '2025-11-19T12:00:00Z',
      };

      // First call - cache miss
      mockKV.get.mockResolvedValueOnce(null);
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue(mockTrustScore);
      await cache.getTrustScore(userId);

      // Second call - cache hit
      mockKV.get.mockResolvedValueOnce(mockTrustScore);
      await cache.getTrustScore(userId);

      const stats = cache.getStats();
      expect(stats.hits).toBe(1);
      expect(stats.misses).toBe(1);
      expect(stats.hitRate).toBeCloseTo(0.5);
    });

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
      mockKV.get.mockRejectedValue(new Error('KV error'));
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue({
        userId: 'user-123',
        score: 850,
        components: { verification: 0.9, activity: 0.8, reputation: 0.85 },
        lastUpdated: '2025-11-19T12:00:00Z',
      });

      // Should fall back to fetching from GraphDB
      const result = await cache.getTrustScore('user-123');

      expect(result).toBeDefined();
      expect(mockClient.getTrustScore).toHaveBeenCalled();
    });

    it('should handle KV put errors gracefully', async () => {
      mockKV.get.mockResolvedValue(null);
      mockKV.put.mockRejectedValue(new Error('KV put error'));
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue({
        userId: 'user-123',
        score: 850,
        components: { verification: 0.9, activity: 0.8, reputation: 0.85 },
        lastUpdated: '2025-11-19T12:00:00Z',
      });

      // Should still return result even if cache write fails
      const result = await cache.getTrustScore('user-123');

      expect(result).toBeDefined();
    });
  });

  describe('cache key generation', () => {
    it('should generate consistent cache keys', () => {
      const key1 = cache.generateKey('trust', 'user-123');
      const key2 = cache.generateKey('trust', 'user-123');

      expect(key1).toBe(key2);
      expect(key1).toBe('trust:user-123');
    });

    it('should generate different keys for different types', () => {
      const trustKey = cache.generateKey('trust', 'user-123');
      const nodeKey = cache.generateKey('node', 'user-123');

      expect(trustKey).not.toBe(nodeKey);
      expect(trustKey).toBe('trust:user-123');
      expect(nodeKey).toBe('node:user-123');
    });
  });

  describe('TTL configuration', () => {
    it('should use different TTLs for different data types', async () => {
      const customCache = new GraphDBCache(mockClient, mockKV as unknown as KVNamespace, {
        trustScoreTTL: 3600,      // 1 hour
        nodeTTL: 300,             // 5 minutes
        traversalTTL: 600,        // 10 minutes
      });

      mockKV.get.mockResolvedValue(null);

      // Trust score
      (mockClient.getTrustScore as ReturnType<typeof vi.fn>).mockResolvedValue({
        userId: 'user-123',
        score: 850,
        components: { verification: 0.9, activity: 0.8, reputation: 0.85 },
        lastUpdated: '2025-11-19T12:00:00Z',
      });
      await customCache.getTrustScore('user-123');
      expect(mockKV.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(String),
        { expirationTtl: 3600 }
      );

      // Node
      (mockClient.getNode as ReturnType<typeof vi.fn>).mockResolvedValue({
        id: 'node-123',
        type: 'user',
        properties: {},
      });
      await customCache.getNode('node-123');
      expect(mockKV.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.any(String),
        { expirationTtl: 300 }
      );
    });
  });
});
