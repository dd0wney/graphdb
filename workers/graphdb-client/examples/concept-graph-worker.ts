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
        const conceptId = Number(path.split('/')[2]);
        return await getLearningPath(graphDB, env.CONCEPT_CACHE, conceptId);
      }

      // GET /related/:conceptId
      if (path.startsWith('/related/')) {
        const conceptId = Number(path.split('/')[2]);
        return await getRelatedConcepts(graphDB, env.CONCEPT_CACHE, conceptId);
      }

      // POST /mastery/:userId/:conceptId
      if (path.match(/\/mastery\/(.+)\/(.+)$/)) {
        const [, userIdRaw, conceptIdRaw] = path.split('/');
        const userId = Number(userIdRaw);
        const conceptId = Number(conceptIdRaw);
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
 * Get learning path (prerequisite chain) for a concept.
 *
 * POST /traverse only returns a flat node list — there is no `paths`
 * field on the response (pkg/api/handlers_algorithms_traversal.go
 * TraversalResponse) — so "the learning path" here is the reachable
 * prerequisite set, not an ordered chain of individual paths.
 */
async function getLearningPath(
  graphDB: GraphDBClient,
  cache: KVNamespace,
  conceptId: number
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
  });

  const learningPath = {
    targetConcept: conceptId,
    prerequisites: result.nodes.map((node) => ({
      id: node.id,
      name: node.properties.name,
      domain: node.properties.domain,
      difficulty: node.properties.difficulty,
    })),
    totalConcepts: result.nodes.length,
    truncated: result.truncated ?? false,
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
  conceptId: number
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
  });

  const related = {
    conceptId,
    relatedConcepts: result.nodes.map((node) => ({
      id: node.id,
      name: node.properties.name,
      domain: node.properties.domain,
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
  userId: number,
  conceptId: number,
  level: number
): Promise<Response> {
  // Create MASTERED edge with mastery level
  await graphDB.createEdge({
    type: 'MASTERED',
    from_node_id: userId,
    to_node_id: conceptId,
    properties: {
      level,
      timestamp: new Date().toISOString(),
    },
  });

  // Update user's total mastery count
  const user = await graphDB.getNode(userId);
  const masteryCount = Number(user.properties.masteryCount ?? 0) + 1;

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
