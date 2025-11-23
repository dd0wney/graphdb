# Security Features Quick Start Guide

This guide shows you how to use GraphDB's security features in production.

## Quick Start: Enable All Security Features

```bash
# 1. Generate master encryption key
export ENCRYPTION_MASTER_KEY=$(openssl rand -hex 32)
echo "Save this key securely: $ENCRYPTION_MASTER_KEY"

# 2. Generate JWT secret
export JWT_SECRET=$(openssl rand -base64 32)
echo "Save this secret securely: $JWT_SECRET"

# 3. Set admin password
export ADMIN_PASSWORD="YourSecurePassword123!"

# 4. Enable encryption and TLS
export ENCRYPTION_ENABLED=true
export TLS_ENABLED=true
export TLS_AUTO_GENERATE=true  # Auto-generate self-signed cert for testing

# 5. Start server
./bin/server
```

## Using Security Features

### 1. Authentication

#### Login and Get JWT Token

```bash
# Login
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "YourSecurePassword123!"
  }'

# Response:
{
  "access_token": "eyJhbGc...",
  "refresh_token": "eyJhbGc...",
  "user": {
    "id": "...",
    "username": "admin",
    "role": "admin"
  }
}
```

#### Use Token for Authenticated Requests

```bash
# Set token
export TOKEN="your-access-token-here"

# Make authenticated request
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/nodes
```

### 2. Encryption Management

#### Check Encryption Status

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/health
```

Response:
```json
{
  "timestamp": "2025-11-23T16:18:17Z",
  "status": "healthy",
  "components": {
    "encryption": {
      "enabled": true,
      "key_stats": {
        "total_keys": 1,
        "active_version": 1,
        "active_key_age": "1h30m"
      }
    },
    "tls": {
      "enabled": true
    },
    "audit": {
      "enabled": true,
      "event_count": 127
    }
  }
}
```

#### View Encryption Keys

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/keys/info
```

Response:
```json
{
  "statistics": {
    "total_keys": 2,
    "active_keys": 1,
    "rotated_keys": 1,
    "active_version": 2,
    "active_key_age": "30m"
  },
  "keys": [
    {
      "version": 1,
      "created_at": "2025-11-23T15:00:00Z",
      "status": "rotated",
      "algorithm": "AES-256-GCM"
    },
    {
      "version": 2,
      "created_at": "2025-11-23T16:00:00Z",
      "status": "active",
      "algorithm": "AES-256-GCM"
    }
  ]
}
```

#### Rotate Encryption Keys

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/keys/rotate
```

Response:
```json
{
  "message": "Key rotated successfully",
  "new_version": 3,
  "timestamp": "2025-11-23T17:00:00Z"
}
```

**Best Practice:** Rotate keys every 90 days for compliance.

### 3. Audit Logs

#### View Recent Audit Logs

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?limit=10"
```

#### Filter Audit Logs

```bash
# Filter by user
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?username=admin&limit=50"

# Filter by action
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?action=create&limit=50"

# Filter by resource type
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?resource_type=node&limit=50"

# Filter by status
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?status=failure&limit=50"

# Filter by time range
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?start_time=2025-11-23T00:00:00Z&end_time=2025-11-23T23:59:59Z"
```

#### Export Audit Logs

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -o audit-logs.json \
  http://localhost:8080/api/v1/security/audit/export
```

This downloads all audit logs as a JSON file.

### 4. TLS Configuration

#### Development (Self-Signed Certificates)

```bash
export TLS_ENABLED=true
export TLS_AUTO_GENERATE=true
export TLS_HOSTS="localhost,127.0.0.1"
./bin/server
```

Server will auto-generate a self-signed certificate.

#### Production (Your Own Certificates)

```bash
export TLS_ENABLED=true
export TLS_CERT_FILE=/path/to/cert.pem
export TLS_KEY_FILE=/path/to/key.pem
export TLS_MIN_VERSION=1.3
./bin/server
```

#### Let's Encrypt with Certbot

```bash
# 1. Get certificate
sudo certbot certonly --standalone -d your-domain.com

# 2. Configure GraphDB
export TLS_ENABLED=true
export TLS_CERT_FILE=/etc/letsencrypt/live/your-domain.com/fullchain.pem
export TLS_KEY_FILE=/etc/letsencrypt/live/your-domain.com/privkey.pem
./bin/server
```

## Environment Variables Reference

### Encryption

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `ENCRYPTION_ENABLED` | No | `false` | Enable encryption at rest |
| `ENCRYPTION_MASTER_KEY` | If enabled | Generated | 64-char hex string (32 bytes) |
| `ENCRYPTION_KEY_DIR` | No | `./data/keys` | Directory for key metadata |

### TLS/SSL

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TLS_ENABLED` | No | `false` | Enable TLS/HTTPS |
| `TLS_CERT_FILE` | If enabled | Auto-gen | Path to certificate file |
| `TLS_KEY_FILE` | If enabled | Auto-gen | Path to private key file |
| `TLS_CA_FILE` | No | - | Path to CA certificate |
| `TLS_AUTO_GENERATE` | No | `true` | Auto-generate self-signed cert |
| `TLS_HOSTS` | No | `localhost` | Comma-separated hostnames |
| `TLS_MIN_VERSION` | No | `1.2` | Minimum TLS version (1.2 or 1.3) |
| `TLS_CLIENT_AUTH` | No | `none` | Client auth: none, request, required |

### Authentication

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JWT_SECRET` | Recommended | Auto-gen | Secret for signing JWT tokens (32+ chars) |
| `ADMIN_PASSWORD` | Recommended | `admin123!` | Default admin password |

## Security Checklist for Production

- [ ] Generate and securely store master encryption key
- [ ] Generate and securely store JWT secret
- [ ] Set strong admin password
- [ ] Enable encryption at rest
- [ ] Enable TLS with valid certificates
- [ ] Use TLS 1.3 if possible
- [ ] Rotate encryption keys every 90 days
- [ ] Monitor audit logs for suspicious activity
- [ ] Export audit logs regularly for compliance
- [ ] Keep GraphDB and dependencies updated
- [ ] Use firewall to restrict access
- [ ] Use strong passwords for all users
- [ ] Backup master encryption key in secure location
- [ ] Test disaster recovery procedures

## Common Security Operations

### Monthly Security Review

```bash
#!/bin/bash
# Monthly security review script

TOKEN="your-token"

echo "=== Monthly Security Review ==="

# 1. Check security health
echo "1. Security Health:"
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/health | jq .

# 2. Check key age
echo "2. Encryption Key Age:"
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/keys/info | jq '.statistics.active_key_age'

# 3. Export audit logs
echo "3. Exporting audit logs..."
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -o "audit-logs-$(date +%Y-%m).json" \
  http://localhost:8080/api/v1/security/audit/export

# 4. Check failed login attempts
echo "4. Failed Login Attempts:"
curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/security/audit/logs?action=auth&status=failure&limit=100" \
  | jq '.count'

echo "Review complete!"
```

### Quarterly Key Rotation

```bash
#!/bin/bash
# Quarterly key rotation

TOKEN="your-token"

echo "=== Rotating Encryption Keys ==="

# Rotate keys
RESPONSE=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/keys/rotate)

echo "$RESPONSE" | jq .

NEW_VERSION=$(echo "$RESPONSE" | jq -r '.new_version')
echo "New key version: $NEW_VERSION"

# Verify rotation
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/security/keys/info | jq '.statistics'

echo "Key rotation complete!"
```

## Troubleshooting

### Encryption Not Working

```bash
# Check if encryption is enabled
curl http://localhost:8080/health | jq '.features'

# Should include "encryption" in features list
```

### TLS Certificate Issues

```bash
# Test TLS connection
openssl s_client -connect localhost:8080 -showcerts

# Check certificate expiration
echo | openssl s_client -connect localhost:8080 2>/dev/null | openssl x509 -noout -dates
```

### Authentication Failures

```bash
# Check JWT secret is set
echo $JWT_SECRET

# Verify admin user exists
curl http://localhost:8080/health

# Try login with default credentials
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123!"}'
```

## Support

For security issues or questions:
- Documentation: `/docs` directory
- Security summary: `SECURITY-INTEGRATION-SUMMARY.md`
- Report vulnerabilities: [GitHub Issues](https://github.com/dd0wney/graphdb/issues)

---

**Last Updated:** 2025-11-23
