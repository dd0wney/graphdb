/**
 * Cloudflare Workers - GraphDB Tunnel Router
 *
 * This Worker acts as a global edge router for GraphDB, providing:
 * - DDoS protection
 * - Rate limiting
 * - Edge caching for read-heavy queries
 * - Authentication/authorization
 * - Request routing to origin server via Cloudflare Tunnel
 */

export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);

    // CORS headers for all responses
    const corsHeaders = {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type, Authorization',
    };

    // Handle preflight requests
    if (request.method === 'OPTIONS') {
      return new Response(null, {
        headers: corsHeaders
      });
    }

    // Rate limiting (basic example - use Cloudflare Rate Limiting API for production)
    const clientIP = request.headers.get('CF-Connecting-IP');
    const rateLimitKey = `rate_limit:${clientIP}`;

    // Example: 100 requests per minute per IP
    // In production, use Cloudflare's Rate Limiting API or Durable Objects for distributed counting

    // Authentication (optional - for Enterprise features)
    const authHeader = request.headers.get('Authorization');
    if (authHeader) {
      // Validate JWT or API key
      // For Enterprise features, check license validity
      // const isValid = await validateAuth(authHeader, env);
      // if (!isValid) {
      //   return new Response('Unauthorized', { status: 401, headers: corsHeaders });
      // }
    }

    // Cache strategy for read-only queries
    const cacheKey = new Request(url.toString(), request);
    const cache = caches.default;

    // Check cache for GET requests (read queries)
    if (request.method === 'GET') {
      const cachedResponse = await cache.match(cacheKey);
      if (cachedResponse) {
        // Return cached response with cache hit header
        const response = new Response(cachedResponse.body, cachedResponse);
        response.headers.set('X-Cache', 'HIT');
        Object.keys(corsHeaders).forEach(key => response.headers.set(key, corsHeaders[key]));
        return response;
      }
    }

    // Route to origin server (via Cloudflare Tunnel)
    // The tunnel automatically routes to your Digital Ocean droplet
    const originResponse = await fetch(request);

    // Clone response for caching
    const response = new Response(originResponse.body, originResponse);

    // Add CORS headers
    Object.keys(corsHeaders).forEach(key => response.headers.set(key, corsHeaders[key]));

    // Add cache headers
    response.headers.set('X-Cache', 'MISS');

    // Cache successful GET responses
    if (request.method === 'GET' && response.status === 200) {
      // Cache for 5 minutes (adjust based on your needs)
      response.headers.set('Cache-Control', 'public, max-age=300');
      ctx.waitUntil(cache.put(cacheKey, response.clone()));
    }

    return response;
  }
};

// Helper function to validate authentication (example)
async function validateAuth(authHeader, env) {
  // In production, validate JWT or API key
  // Check against license server or Cloudflare KV
  // For Enterprise features, verify license is active

  if (authHeader.startsWith('Bearer ')) {
    const token = authHeader.substring(7);
    // Validate JWT token
    // const isValid = await verifyJWT(token, env.JWT_SECRET);
    // return isValid;
  }

  return false;
}
