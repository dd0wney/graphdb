# License Server Setup Guide

## Overview

The Cluso GraphDB license server handles commercial license generation and validation through Stripe integration.

## Architecture

```
Customer → Stripe Checkout → Webhook → License Server → License Generated
GraphDB → License Server → License Validated
```

## Quick Start

### Local Development

```bash
# Build the license server
go build -o bin/license-server ./cmd/license-server

# Run locally
./bin/license-server \
  --port 8080 \
  --data ./data/licenses \
  --stripe-key sk_test_xxx \
  --webhook-secret whsec_xxx
```

### Railway Deployment

1. **Create Railway Project**
   ```bash
   # Install Railway CLI
   npm install -g @railway/cli

   # Login
   railway login

   # Create project
   railway init

   # Link to repository
   railway link
   ```

2. **Configure Environment Variables**

   In Railway dashboard, set:
   ```
   STRIPE_SECRET_KEY=sk_live_xxxxxxxxxxxxx
   STRIPE_WEBHOOK_SECRET=whsec_xxxxxxxxxxxxx
   PORT=8080
   ```

3. **Deploy**
   ```bash
   railway up
   ```

4. **Get Public URL**
   ```bash
   railway domain
   ```

   Your license server will be available at: `https://your-app.railway.app`

## Stripe Setup

### 1. Create Stripe Products

```bash
# Professional Edition
Product Name: GraphDB Professional
Price: $49/month or $490/year
Metadata: type=professional

# Enterprise Edition
Product Name: GraphDB Enterprise
Price: $299/month or $2,990/year
Metadata: type=enterprise
```

### 2. Configure Webhook

1. Go to Stripe Dashboard → Developers → Webhooks
2. Add endpoint: `https://your-app.railway.app/stripe/webhook`
3. Select events:
   - `checkout.session.completed`
   - `customer.subscription.updated`
   - `customer.subscription.deleted`
4. Copy webhook signing secret to `STRIPE_WEBHOOK_SECRET`

### 3. Create Checkout Page

```html
<!DOCTYPE html>
<html>
<head>
    <title>Purchase Cluso GraphDB License</title>
    <script src="https://js.stripe.com/v3/"></script>
</head>
<body>
    <h1>Cluso GraphDB Licensing</h1>

    <div>
        <h2>Professional Edition - $49/month</h2>
        <button id="checkout-professional">Purchase</button>
    </div>

    <div>
        <h2>Enterprise Edition - $299/month</h2>
        <button id="checkout-enterprise">Purchase</button>
    </div>

    <script>
        const stripe = Stripe('pk_live_xxxxxxxxxxxxx');

        document.getElementById('checkout-professional').addEventListener('click', async () => {
            const { error } = await stripe.redirectToCheckout({
                lineItems: [{price: 'price_xxxxxxxxxxxxx', quantity: 1}],
                mode: 'subscription',
                successUrl: 'https://cluso-graphdb.com/success',
                cancelUrl: 'https://cluso-graphdb.com/pricing',
                metadata: {type: 'professional'}
            });
        });

        document.getElementById('checkout-enterprise').addEventListener('click', async () => {
            const { error } = await stripe.redirectToCheckout({
                lineItems: [{price: 'price_xxxxxxxxxxxxx', quantity: 1}],
                mode: 'subscription',
                successUrl: 'https://cluso-graphdb.com/success',
                cancelUrl: 'https://cluso-graphdb.com/pricing',
                metadata: {type: 'enterprise'}
            });
        });
    </script>
</body>
</html>
```

## API Endpoints

### Health Check

```bash
GET /health

Response:
{
  "status": "healthy",
  "time": 1699564800
}
```

### Validate License

```bash
POST /validate
Content-Type: application/json

{
  "license_key": "CGDB-XXXX-XXXX-XXXX-XXXX"
}

Response (valid):
{
  "valid": true,
  "type": "professional",
  "email": "user@example.com",
  "created_at": 1699564800,
  "expires_at": null
}

Response (invalid):
{
  "valid": false,
  "reason": "not_found"
}
```

### List Licenses (Admin)

```bash
GET /licenses

Response:
{
  "licenses": [
    {
      "id": "lic_xxxxxxxxxxxx",
      "key": "CGDB-XXXX-XXXX-XXXX-XXXX",
      "type": "professional",
      "email": "user@example.com",
      "status": "active",
      "created_at": "2024-11-16T10:00:00Z"
    }
  ],
  "count": 1
}
```

### Create License (Testing)

```bash
POST /licenses/create
Content-Type: application/json

{
  "email": "test@example.com",
  "type": "professional"
}

Response:
{
  "id": "lic_xxxxxxxxxxxx",
  "key": "CGDB-XXXX-XXXX-XXXX-XXXX",
  "type": "professional",
  "email": "test@example.com",
  "status": "active",
  "created_at": "2024-11-16T10:00:00Z"
}
```

## Testing the Flow

### 1. Create Test License

```bash
curl -X POST https://your-app.railway.app/licenses/create \
  -H "Content-Type: application/json" \
  -d '{
    "email": "test@example.com",
    "type": "professional"
  }'
```

Save the returned license key.

### 2. Validate License

```bash
curl -X POST https://your-app.railway.app/validate \
  -H "Content-Type: application/json" \
  -d '{
    "license_key": "CGDB-XXXX-XXXX-XXXX-XXXX"
  }'
```

Should return `{"valid": true, ...}`

### 3. Test Stripe Webhook (Local)

Use Stripe CLI for local testing:

```bash
# Install Stripe CLI
brew install stripe/stripe-cli/stripe

# Forward webhooks to local server
stripe listen --forward-to localhost:8080/stripe/webhook

# Trigger test event
stripe trigger checkout.session.completed
```

## GraphDB Integration

Add license validation to your GraphDB client:

```go
package main

import (
    "bytes"
    "encoding/json"
    "net/http"
)

func validateLicense(licenseKey string) (bool, error) {
    reqBody, _ := json.Marshal(map[string]string{
        "license_key": licenseKey,
    })

    resp, err := http.Post(
        "https://your-app.railway.app/validate",
        "application/json",
        bytes.NewBuffer(reqBody),
    )
    if err != nil {
        return false, err
    }
    defer resp.Body.Close()

    var result struct {
        Valid bool   `json:"valid"`
        Type  string `json:"type"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return false, err
    }

    return result.Valid, nil
}
```

## Production Checklist

- [ ] Set up Stripe account and products
- [ ] Configure webhook endpoint in Stripe
- [ ] Deploy license server to Railway
- [ ] Set environment variables (STRIPE keys)
- [ ] Test checkout flow end-to-end
- [ ] Set up email service for license delivery
- [ ] Add authentication to admin endpoints
- [ ] Set up monitoring and logging
- [ ] Configure database backups
- [ ] Test license validation from GraphDB

## Monitoring

Check license server logs in Railway:

```bash
railway logs
```

Monitor key metrics:
- License creation rate
- Validation request rate
- Failed validations
- Webhook processing errors

## Security Considerations

1. **Webhook Verification**: Verify Stripe signatures in production
2. **Admin Endpoints**: Add authentication to `/licenses` endpoint
3. **HTTPS Only**: Ensure all communication uses HTTPS
4. **Rate Limiting**: Add rate limiting to validation endpoint
5. **Data Backup**: Regularly backup license database

## Troubleshooting

**Webhook not receiving events:**
- Check webhook URL in Stripe dashboard
- Verify STRIPE_WEBHOOK_SECRET is set correctly
- Check Railway logs for errors

**License validation failing:**
- Verify license server is accessible
- Check license key format
- Ensure license status is "active"

**Docker build failing:**
- Ensure go.mod and go.sum are up to date
- Check Dockerfile path in railway.json

## Support

For issues or questions:
- GitHub Issues: https://github.com/darraghdowney/cluso-graphdb/issues
- Email: support@cluso-graphdb.com
