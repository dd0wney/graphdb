/**
 * GraphDB KV Cache Wrapper
 *
 * Implements cache-aside pattern with Cloudflare KV for GraphDB queries
 */

import { GraphDBClient } from './client';
import type { TrustScore, Node, TraversalResult } from './types';

/**
 * Cache configuration options
 */
export interface CacheConfig {
  /** Default TTL in seconds (default: 3600 = 1 hour) */
  defaultTTL?: number;

  /** Trust score TTL in seconds (default: 3600 = 1 hour) */
  trustScoreTTL?: number;

  /** Node TTL in seconds (default: 300 = 5 minutes) */
  nodeTTL?: number;

  /** Traversal TTL in seconds (default: 600 = 10 minutes) */
  traversalTTL?: number;

  /** Fraud detection TTL in seconds (default: 86400 = 24 hours) */
  fraudTTL?: number;
}

/**
 * Cache statistics
 */
export interface CacheStats {
  hits: number;
  misses: number;
  hitRate: number;
}

/**
 * GraphDB Cache with KV backend
 *
 * Provides caching layer on top of GraphDB client using Cloudflare KV.
 * Implements cache-aside pattern for optimal performance.
 *
 * @example
 * ```typescript
 * const cache = new GraphDBCache(graphDB, env.TRUST_CACHE, {
 *   trustScoreTTL: 3600,  // 1 hour
 *   nodeTTL: 300,         // 5 minutes
 * });
 *
 * // Cache-aside pattern - automatic caching
 * const trustScore = await cache.getTrustScore('user-123');
 * ```
 */
export class GraphDBCache {
  private config: Required<CacheConfig>;
  private stats = {
    hits: 0,
    misses: 0,
  };

  constructor(
    private client: GraphDBClient,
    private kv: KVNamespace,
    config: CacheConfig = {}
  ) {
    this.config = {
      defaultTTL: config.defaultTTL || 3600,
      trustScoreTTL: config.trustScoreTTL || 3600,
      nodeTTL: config.nodeTTL || 300,
      traversalTTL: config.traversalTTL || 600,
      fraudTTL: config.fraudTTL || 86400,
    };
  }

  /**
   * Get trust score with caching (cache-aside pattern)
   */
  async getTrustScore(userId: string): Promise<TrustScore> {
    const cacheKey = this.generateKey('trust', userId);

    try {
      // Try cache first
      const cached = await this.kv.get(cacheKey, 'json');
      if (cached) {
        this.stats.hits++;
        return cached as TrustScore;
      }
    } catch (error) {
      // KV error - fall through to fetch from GraphDB
      console.warn('[GraphDBCache] KV get error:', error);
    }

    // Cache miss - fetch from GraphDB
    this.stats.misses++;
    const trustScore = await this.client.getTrustScore(userId);

    // Cache the result
    try {
      await this.kv.put(cacheKey, JSON.stringify(trustScore), {
        expirationTtl: this.config.trustScoreTTL,
      });
    } catch (error) {
      // KV put error - log but don't fail
      console.warn('[GraphDBCache] KV put error:', error);
    }

    return trustScore;
  }

  /**
   * Get node with caching
   */
  async getNode(nodeId: string): Promise<Node> {
    const cacheKey = this.generateKey('node', nodeId);

    try {
      const cached = await this.kv.get(cacheKey, 'json');
      if (cached) {
        this.stats.hits++;
        return cached as Node;
      }
    } catch (error) {
      console.warn('[GraphDBCache] KV get error:', error);
    }

    this.stats.misses++;
    const node = await this.client.getNode(nodeId);

    try {
      await this.kv.put(cacheKey, JSON.stringify(node), {
        expirationTtl: this.config.nodeTTL,
      });
    } catch (error) {
      console.warn('[GraphDBCache] KV put error:', error);
    }

    return node;
  }

  /**
   * Get traversal result with caching
   */
  async traverse(
    startNodeId: string,
    edgeTypes: string[],
    maxDepth: number,
    direction: 'outgoing' | 'incoming' | 'both'
  ): Promise<TraversalResult> {
    const cacheKey = this.generateTraversalKey(startNodeId, edgeTypes, maxDepth, direction);

    try {
      const cached = await this.kv.get(cacheKey, 'json');
      if (cached) {
        this.stats.hits++;
        return cached as TraversalResult;
      }
    } catch (error) {
      console.warn('[GraphDBCache] KV get error:', error);
    }

    this.stats.misses++;
    const result = await this.client.traverse({
      startNodeId,
      edgeTypes,
      maxDepth,
      direction,
    });

    try {
      await this.kv.put(cacheKey, JSON.stringify(result), {
        expirationTtl: this.config.traversalTTL,
      });
    } catch (error) {
      console.warn('[GraphDBCache] KV put error:', error);
    }

    return result;
  }

  /**
   * Invalidate trust score cache
   */
  async invalidateTrustScore(userId: string): Promise<void> {
    const cacheKey = this.generateKey('trust', userId);
    await this.kv.delete(cacheKey);
  }

  /**
   * Invalidate node cache
   */
  async invalidateNode(nodeId: string): Promise<void> {
    const cacheKey = this.generateKey('node', nodeId);
    await this.kv.delete(cacheKey);
  }

  /**
   * Invalidate multiple cache keys
   */
  async invalidateMultiple(keys: string[]): Promise<void> {
    await Promise.all(keys.map((key) => this.kv.delete(key)));
  }

  /**
   * Generate cache key
   */
  generateKey(type: string, id: string): string {
    return `${type}:${id}`;
  }

  /**
   * Generate traversal cache key
   */
  private generateTraversalKey(
    startNodeId: string,
    edgeTypes: string[],
    maxDepth: number,
    direction: string
  ): string {
    const edgeTypesStr = edgeTypes.sort().join(',');
    return `traversal:${startNodeId}:${edgeTypesStr}:${maxDepth}:${direction}`;
  }

  /**
   * Get cache statistics
   */
  getStats(): CacheStats {
    const total = this.stats.hits + this.stats.misses;
    const hitRate = total > 0 ? this.stats.hits / total : 0;

    return {
      hits: this.stats.hits,
      misses: this.stats.misses,
      hitRate,
    };
  }

  /**
   * Reset statistics
   */
  resetStats(): void {
    this.stats.hits = 0;
    this.stats.misses = 0;
  }
}
