/**
 * Cloudflare Worker Example: Concept Graph API for Syntopica
 *
 * This worker provides concept prerequisite traversal with:
 * - Learning path generation (prerequisite chains)
 * - Concept mastery tracking
 * - Related concept discovery
 * - KV caching for static knowledge graph data
 */

import { GraphDBClient } from '@graphdb/client';

interface Env {
  GRAPHDB_URL: string;
  GRAPHDB_API_KEY: string;
  CONCEPT_CACHE: KVNamespace;
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const path = url.pathname;

    const graphDB = new GraphDBClient({
      endpoint: env.GRAPHDB_URL,
      apiKey: env.GRAPHDB_API_KEY,
      timeout: 5000,
      retries: 2,
    });

    try {
      // GET /learning-path/:conceptId
      if (path.startsWith('/learning-path/')) {
        const conceptId = path.split('/')[2];
        return await getLearningPath(graphDB, env.CONCEPT_CACHE, conceptId);
      }

      // GET /related/:conceptId
      if (path.startsWith('/related/')) {
        const conceptId = path.split('/')[2];
        return await getRelatedConcepts(graphDB, env.CONCEPT_CACHE, conceptId);
      }

      // POST /mastery/:userId/:conceptId
      if (path.match(/\/mastery\/(.+)\/(.+)$/)) {
        const [_, userId, conceptId] = path.split('/');
        const level = parseFloat(url.searchParams.get('level') || '0.85');
        return await recordMastery(graphDB, userId, conceptId, level);
      }

      return new Response('Not found', { status: 404 });
    } catch (error) {
      console.error('Error:', error);
      return new Response('Internal error', { status: 500 });
    }
  },
};

/**
 * Get learning path (prerequisite chain) for a concept
 */
async function getLearningPath(
  graphDB: GraphDBClient,
  cache: KVNamespace,
  conceptId: string
): Promise<Response> {
  const cacheKey = `learning-path:${conceptId}`;

  // Try cache (7 day TTL - knowledge graph is mostly static)
  const cached = await cache.get(cacheKey, 'json');
  if (cached) {
    return new Response(JSON.stringify(cached), {
      headers: {
        'Content-Type': 'application/json',
        'X-Cache': 'HIT',
      },
    });
  }

  // Traverse prerequisites (incoming PREREQUISITE edges)
  const result = await graphDB.traverse({
    startNodeId: conceptId,
    edgeTypes: ['PREREQUISITE'],
    maxDepth: 3, // Go up to 3 levels deep
    direction: 'incoming', // Follow edges pointing TO this concept
    limit: 50,
  });

  // Build learning path (ordered by depth)
  const learningPath = {
    targetConcept: conceptId,
    prerequisites: result.nodes.map(node => ({
      id: node.id,
      name: node.properties.name,
      domain: node.properties.domain,
      difficulty: node.properties.difficulty,
    })),
    totalConcepts: result.nodes.length,
    paths: result.paths.map(path => ({
      concepts: path.nodes,
      depth: path.nodes.length,
    })),
  };

  // Cache for 7 days (knowledge graph rarely changes)
  await cache.put(cacheKey, JSON.stringify(learningPath), {
    expirationTtl: 604800, // 7 days
  });

  return new Response(JSON.stringify(learningPath), {
    headers: {
      'Content-Type': 'application/json',
      'X-Cache': 'MISS',
    },
  });
}

/**
 * Get related concepts (RELATED_TO edges)
 */
async function getRelatedConcepts(
  graphDB: GraphDBClient,
  cache: KVNamespace,
  conceptId: string
): Promise<Response> {
  const cacheKey = `related:${conceptId}`;

  // Try cache
  const cached = await cache.get(cacheKey, 'json');
  if (cached) {
    return new Response(JSON.stringify(cached), {
      headers: { 'Content-Type': 'application/json', 'X-Cache': 'HIT' },
    });
  }

  // Traverse related concepts (both directions)
  const result = await graphDB.traverse({
    startNodeId: conceptId,
    edgeTypes: ['RELATED_TO'],
    maxDepth: 1, // Only immediate neighbors
    direction: 'both',
    limit: 20,
  });

  const related = {
    conceptId,
    relatedConcepts: result.nodes.map(node => ({
      id: node.id,
      name: node.properties.name,
      domain: node.properties.domain,
      similarity: 0.8, // Could calculate from edge properties
    })),
  };

  // Cache for 7 days
  await cache.put(cacheKey, JSON.stringify(related), {
    expirationTtl: 604800,
  });

  return new Response(JSON.stringify(related), {
    headers: { 'Content-Type': 'application/json', 'X-Cache': 'MISS' },
  });
}

/**
 * Record concept mastery for a user
 */
async function recordMastery(
  graphDB: GraphDBClient,
  userId: string,
  conceptId: string,
  level: number
): Promise<Response> {
  // Create MASTERED edge with mastery level
  const edge = await graphDB.createEdge({
    type: 'MASTERED',
    source: userId,
    target: conceptId,
    properties: {
      level,
      timestamp: new Date().toISOString(),
    },
  });

  // Update user's total mastery count
  const user = await graphDB.getNode(userId);
  const masteryCount = (user.properties.masteryCount || 0) + 1;

  await graphDB.updateNode(userId, {
    properties: {
      masteryCount,
      lastActivity: new Date().toISOString(),
    },
  });

  return new Response(
    JSON.stringify({
      success: true,
      userId,
      conceptId,
      level,
      totalMastered: masteryCount,
    }),
    { headers: { 'Content-Type': 'application/json' } }
  );
}
