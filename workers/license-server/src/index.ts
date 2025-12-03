import { Hono } from 'hono';
import { cors } from 'hono/cors';
import type {
  Env,
  ValidateLicenseRequest,
  CreateLicenseRequest,
  UpdateLicenseRequest,
} from './types';
import { LicenseService } from './license';
import { StripeWebhookHandler } from './stripe';

const app = new Hono<{ Bindings: Env }>();

// CORS for API endpoints
app.use('/*', cors());

/**
 * Health check endpoint
 */
app.get('/health', (c) => {
  return c.json({
    status: 'healthy',
    service: 'graphdb-license-server',
    timestamp: new Date().toISOString(),
  });
});

/**
 * POST /validate
 *
 * Validate a license key
 *
 * Request body:
 * {
 *   "licenseKey": "eyJ...",
 *   "instanceId": "uuid" (optional),
 *   "version": "1.0.0" (optional)
 * }
 *
 * Response:
 * {
 *   "valid": true,
 *   "tier": "pro",
 *   "status": "active",
 *   "expiresAt": "2025-12-31T23:59:59Z" | null,
 *   "maxNodes": null,
 *   "timestamp": "2025-01-19T12:00:00Z"
 * }
 */
app.post('/validate', async (c) => {
  try {
    const body = await c.req.json<ValidateLicenseRequest>();

    if (!body.licenseKey) {
      return c.json(
        {
          valid: false,
          error: 'Missing licenseKey',
          timestamp: new Date().toISOString(),
        },
        400
      );
    }

    const licenseService = new LicenseService(c.env);
    const result = await licenseService.validate(
      body.licenseKey,
      body.instanceId
    );

    return c.json(result);
  } catch (error) {
    console.error('Validation error:', error);
    return c.json(
      {
        valid: false,
        error: 'Internal server error',
        timestamp: new Date().toISOString(),
      },
      500
    );
  }
});

/**
 * POST /webhooks/stripe
 *
 * Stripe webhook endpoint for subscription events
 */
app.post('/webhooks/stripe', async (c) => {
  try {
    const signature = c.req.header('stripe-signature');
    if (!signature) {
      return c.json({ error: 'Missing signature' }, 400);
    }

    const body = await c.req.text();
    const webhookHandler = new StripeWebhookHandler(c.env);

    // Verify webhook signature
    const event = await webhookHandler.verifyWebhook(body, signature);
    if (!event) {
      return c.json({ error: 'Invalid signature' }, 401);
    }

    // Handle event (async, best-effort)
    c.executionCtx.waitUntil(webhookHandler.handleEvent(event));

    return c.json({ received: true });
  } catch (error) {
    console.error('Webhook error:', error);
    return c.json({ error: 'Webhook processing failed' }, 500);
  }
});

/**
 * Admin API - Create license
 *
 * POST /admin/licenses
 *
 * Requires: Authorization: Bearer <ADMIN_API_KEY>
 */
app.post('/admin/licenses', async (c) => {
  // Check admin API key
  const authHeader = c.req.header('Authorization');
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return c.json({ error: 'Unauthorized' }, 401);
  }

  const apiKey = authHeader.substring(7);
  if (apiKey !== c.env.ADMIN_API_KEY) {
    return c.json({ error: 'Invalid API key' }, 403);
  }

  try {
    const body = await c.req.json<CreateLicenseRequest>();

    // Validate request
    if (!body.email || !body.tier) {
      return c.json({ error: 'Missing required fields: email, tier' }, 400);
    }

    const licenseService = new LicenseService(c.env);
    const license = await licenseService.create(body);

    return c.json(license, 201);
  } catch (error) {
    console.error('Create license error:', error);
    return c.json({ error: 'Failed to create license' }, 500);
  }
});

/**
 * Admin API - Get license by email
 *
 * GET /admin/licenses/:email
 */
app.get('/admin/licenses/:email', async (c) => {
  // Check admin API key
  const authHeader = c.req.header('Authorization');
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return c.json({ error: 'Unauthorized' }, 401);
  }

  const apiKey = authHeader.substring(7);
  if (apiKey !== c.env.ADMIN_API_KEY) {
    return c.json({ error: 'Invalid API key' }, 403);
  }

  try {
    const email = c.req.param('email');
    const licenseService = new LicenseService(c.env);
    const license = await licenseService.getByEmail(email);

    if (!license) {
      return c.json({ error: 'License not found' }, 404);
    }

    return c.json(license);
  } catch (error) {
    console.error('Get license error:', error);
    return c.json({ error: 'Failed to get license' }, 500);
  }
});

/**
 * Admin API - Update license
 *
 * PATCH /admin/licenses/:email
 */
app.patch('/admin/licenses/:email', async (c) => {
  // Check admin API key
  const authHeader = c.req.header('Authorization');
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return c.json({ error: 'Unauthorized' }, 401);
  }

  const apiKey = authHeader.substring(7);
  if (apiKey !== c.env.ADMIN_API_KEY) {
    return c.json({ error: 'Invalid API key' }, 403);
  }

  try {
    const email = c.req.param('email');
    const body = await c.req.json<UpdateLicenseRequest>();

    const licenseService = new LicenseService(c.env);
    const existing = await licenseService.getByEmail(email);

    if (!existing) {
      return c.json({ error: 'License not found' }, 404);
    }

    // Extract license ID from existing license
    const payload = await import('./jwt').then((m) =>
      m.verifyJWT(existing.key, c.env.JWT_SECRET)
    );

    if (!payload) {
      return c.json({ error: 'Invalid license key' }, 500);
    }

    const updated = await licenseService.update(payload.sub, body);

    if (!updated) {
      return c.json({ error: 'Failed to update license' }, 500);
    }

    return c.json(updated);
  } catch (error) {
    console.error('Update license error:', error);
    return c.json({ error: 'Failed to update license' }, 500);
  }
});

/**
 * Admin API - Delete license
 *
 * DELETE /admin/licenses/:email
 */
app.delete('/admin/licenses/:email', async (c) => {
  // Check admin API key
  const authHeader = c.req.header('Authorization');
  if (!authHeader || !authHeader.startsWith('Bearer ')) {
    return c.json({ error: 'Unauthorized' }, 401);
  }

  const apiKey = authHeader.substring(7);
  if (apiKey !== c.env.ADMIN_API_KEY) {
    return c.json({ error: 'Invalid API key' }, 403);
  }

  try {
    const email = c.req.param('email');
    const licenseService = new LicenseService(c.env);
    const existing = await licenseService.getByEmail(email);

    if (!existing) {
      return c.json({ error: 'License not found' }, 404);
    }

    // Extract license ID from existing license
    const payload = await import('./jwt').then((m) =>
      m.verifyJWT(existing.key, c.env.JWT_SECRET)
    );

    if (!payload) {
      return c.json({ error: 'Invalid license key' }, 500);
    }

    const deleted = await licenseService.delete(payload.sub);

    if (!deleted) {
      return c.json({ error: 'Failed to delete license' }, 500);
    }

    return c.json({ success: true });
  } catch (error) {
    console.error('Delete license error:', error);
    return c.json({ error: 'Failed to delete license' }, 500);
  }
});

export default app;
