# GraphDB License Server

Cloudflare Workers-based license server for GraphDB with Stripe integration.

## Features

- ✅ **JWT-based license keys** - Cryptographically signed, tamper-proof
- ✅ **KV storage** - Fast, globally distributed license database
- ✅ **Stripe integration** - Automatic license creation/updates from subscriptions
- ✅ **Admin API** - Manage licenses programmatically
- ✅ **Admin CLI** - Command-line tool for manual license management
- ✅ **Edge-deployed** - 99.99% uptime, <50ms latency worldwide
- ✅ **Zero ops** - No servers to maintain

## Architecture

```
┌─────────────┐
│   GraphDB   │──validate──▶┌──────────────────┐
│   Instance  │              │  CF Workers      │
└─────────────┘              │  License Server  │
                             └──────────────────┘
                                      │
                        ┌─────────────┼────────────┐
                        │             │            │
                        ▼             ▼            ▼
                  ┌──────────┐  ┌─────────┐  ┌────────┐
                  │  KV      │  │ Stripe  │  │ Admin  │
                  │ Licenses │  │Webhooks │  │  API   │
                  └──────────┘  └─────────┘  └────────┘
```

## Quick Start

### 1. Install Dependencies

```bash
cd workers/license-server
pnpm install
```

### 2. Create KV Namespace

```bash
# Production
wrangler kv:namespace create LICENSES

# Preview (for development)
wrangler kv:namespace create LICENSES --preview
```

Copy the namespace IDs to `wrangler.toml`.

### 3. Set Secrets

```bash
# JWT secret for signing license keys (generate with: openssl rand -base64 32)
wrangler secret put JWT_SECRET

# Stripe secret key
wrangler secret put STRIPE_SECRET_KEY

# Stripe webhook secret (from Stripe Dashboard)
wrangler secret put STRIPE_WEBHOOK_SECRET

# Admin API key (generate with: openssl rand -base64 32)
wrangler secret put ADMIN_API_KEY
```

### 4. Deploy

```bash
# Development
pnpm run dev

# Production
pnpm run deploy
```

### 5. Configure Custom Domain (Optional)

```bash
# Add route to wrangler.toml
[env.production]
route = "license.graphdb.com/*"

# Then deploy
pnpm run deploy --env production
```

---

## API Documentation

### Public Endpoints

#### `POST /validate`

Validate a license key.

**Request:**
```json
{
  "licenseKey": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "instanceId": "uuid-optional",
  "version": "1.0.0-optional"
}
```

**Response (valid):**
```json
{
  "valid": true,
  "tier": "pro",
  "status": "active",
  "expiresAt": null,
  "maxNodes": null,
  "timestamp": "2025-01-19T12:00:00Z"
}
```

**Response (invalid):**
```json
{
  "valid": false,
  "error": "License expired",
  "timestamp": "2025-01-19T12:00:00Z"
}
```

#### `POST /webhooks/stripe`

Stripe webhook endpoint (used by Stripe, not called directly).

**Headers:**
- `stripe-signature`: Stripe webhook signature

### Admin Endpoints

All admin endpoints require authentication:
```
Authorization: Bearer <ADMIN_API_KEY>
```

#### `POST /admin/licenses`

Create a new license.

**Request:**
```json
{
  "email": "user@example.com",
  "name": "John Doe",
  "tier": "pro",
  "expiresAt": "2025-12-31T23:59:59Z",
  "maxNodes": 10000,
  "metadata": {
    "source": "manual"
  }
}
```

**Response:**
```json
{
  "key": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "tier": "pro",
  "status": "active",
  "email": "user@example.com",
  "name": "John Doe",
  "issuedAt": "2025-01-19T12:00:00Z",
  "expiresAt": "2025-12-31T23:59:59Z",
  "maxNodes": 10000,
  "validationCount": 0
}
```

#### `GET /admin/licenses/:email`

Get license by customer email.

**Response:** Same as create response.

#### `PATCH /admin/licenses/:email`

Update a license.

**Request:**
```json
{
  "tier": "enterprise",
  "status": "active",
  "expiresAt": "2026-12-31T23:59:59Z"
}
```

#### `DELETE /admin/licenses/:email`

Delete a license.

**Response:**
```json
{
  "success": true
}
```

---

## Admin CLI

The admin CLI makes license management easy from the command line.

### Setup

```bash
# Set environment variables
export LICENSE_SERVER_URL="https://license.graphdb.com"
export ADMIN_API_KEY="your-admin-api-key"
```

### Usage

**Create a license:**
```bash
pnpm tsx admin-cli.ts create user@example.com pro

# With expiration
pnpm tsx admin-cli.ts create user@example.com pro --expires=2025-12-31

# With all options
pnpm tsx admin-cli.ts create user@example.com enterprise \
  --expires=2025-12-31 \
  --name="Acme Corp" \
  --max-nodes=50000
```

**Get a license:**
```bash
pnpm tsx admin-cli.ts get user@example.com
```

**Update a license:**
```bash
# Upgrade tier
pnpm tsx admin-cli.ts update user@example.com --tier=enterprise

# Suspend license
pnpm tsx admin-cli.ts update user@example.com --status=suspended

# Extend expiration
pnpm tsx admin-cli.ts update user@example.com --expires=2026-12-31
```

**Delete a license:**
```bash
pnpm tsx admin-cli.ts delete user@example.com
```

**Test a license key:**
```bash
pnpm tsx admin-cli.ts test eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

---

## Stripe Integration

### Setup

1. **Create products in Stripe:**
   - GraphDB Pro ($249/month)
   - GraphDB Enterprise ($999/month)

2. **Configure webhook:**
   - URL: `https://license.graphdb.com/webhooks/stripe`
   - Events to listen for:
     - `checkout.session.completed`
     - `customer.subscription.created`
     - `customer.subscription.updated`
     - `customer.subscription.deleted`
     - `invoice.payment_failed`

3. **Update price mapping in `src/stripe.ts`:**
```typescript
private getTierFromPriceId(priceId?: string): LicenseTier {
  // Map your Stripe price IDs
  if (priceId === 'price_1ABC...') return 'pro';
  if (priceId === 'price_2XYZ...') return 'enterprise';
  return 'pro';
}
```

### Flow

1. Customer completes checkout on Stripe
2. Stripe sends `checkout.session.completed` webhook
3. License server creates license automatically
4. Customer receives email with license key (TODO: implement)
5. Customer uses license key in GraphDB

---

## License Tiers

| Tier | Features |
|------|----------|
| **Community** | Free tier (no license key required) |
| **Pro** | Advanced algorithms, fraud detection, audit logging |
| **Enterprise** | Everything in Pro + RBAC, SSO, priority support |

---

## Development

### Run Locally

```bash
pnpm run dev
```

Server runs at `http://localhost:8787`

### Test Locally

**Create a license:**
```bash
curl -X POST http://localhost:8787/admin/licenses \
  -H "Authorization: Bearer your-admin-api-key" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "tier": "pro"
  }'
```

**Validate a license:**
```bash
curl -X POST http://localhost:8787/validate \
  -H "Content-Type: application/json" \
  -d '{
    "licenseKey": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }'
```

### View Logs

```bash
pnpm run tail
```

---

## Deployment

### Staging

```bash
wrangler deploy --env staging
```

### Production

```bash
wrangler deploy --env production
```

### CI/CD (GitHub Actions)

```yaml
name: Deploy License Server

on:
  push:
    branches: [main]
    paths:
      - 'workers/license-server/**'

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: pnpm/action-setup@v2
      - run: pnpm install
      - run: pnpm run deploy --env production
        env:
          CLOUDFLARE_API_TOKEN: ${{ secrets.CLOUDFLARE_API_TOKEN }}
```

---

## Monitoring

### Metrics

View metrics in Cloudflare Dashboard:
- Request count
- Error rate
- Latency (p50, p99)
- KV read/write operations

### Alerts

Set up alerts for:
- Error rate > 1%
- Latency p99 > 500ms
- KV failures

### Logging

All errors are logged to Cloudflare Workers Logs (accessible via `wrangler tail`).

---

## Security

### JWT Signing

- **Algorithm**: HS256 (HMAC SHA-256)
- **Secret**: 256-bit random key (stored in Cloudflare Secrets)
- **Payload**: License ID, tier, email, expiration

### API Authentication

- Admin endpoints protected by API key in `Authorization` header
- Stripe webhooks verified using Stripe signature
- Public validation endpoint (intentionally public for GraphDB instances to use)

### Rate Limiting

Cloudflare provides DDoS protection by default. For additional rate limiting, use [Cloudflare Rate Limiting](https://developers.cloudflare.com/waf/rate-limiting-rules/).

---

## Troubleshooting

### License validation fails

**Check:**
1. License key format (should be JWT)
2. License exists in KV: `wrangler kv:key get --binding LICENSES "license:uuid"`
3. License status is "active"
4. License not expired

### Stripe webhook fails

**Check:**
1. Webhook signature is valid
2. Stripe webhook secret is correct
3. View logs: `wrangler tail`

### Admin API returns 401

**Check:**
1. `Authorization` header is present
2. API key is correct
3. API key secret is set: `wrangler secret list`

---

## Cost Estimate

### Cloudflare Workers

- **Free tier**: 100,000 requests/day
- **Paid**: $5/month + $0.50 per million requests

### KV Storage

- **Free tier**: 100,000 reads/day, 1,000 writes/day
- **Paid**: $0.50 per million reads, $5 per million writes

### Estimated Monthly Cost

| Usage | Cost |
|-------|------|
| < 100K requests/day | **$0** (free tier) |
| 1M validations/month | **$5.50** |
| 10M validations/month | **$10** |

**For comparison**: Running a t3.micro on AWS = $7.30/month (with 99.5% uptime, not 99.99%)

---

## Next Steps

1. ✅ Deploy license server
2. ⬜ Integrate with GraphDB (add license validation on startup)
3. ⬜ Set up Stripe products and webhook
4. ⬜ Create email templates for license delivery
5. ⬜ Add usage analytics (optional - track which features customers use)
6. ⬜ Build customer portal for self-service license management

---

## Support

- **Documentation**: This README
- **Issues**: https://github.com/dd0wney/graphdb/issues
- **License questions**: support@graphdb.com

---

**Built with Cloudflare Workers. Zero servers. Zero ops. Just works.**
