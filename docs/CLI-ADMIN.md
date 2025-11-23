# GraphDB Admin CLI

The `graphdb-admin` command-line tool provides administrative capabilities for managing GraphDB security features, including encryption key management, audit log exports, and health monitoring.

## Table of Contents

- [Installation](#installation)
- [Global Configuration](#global-configuration)
- [Commands](#commands)
  - [security init](#security-init)
  - [security rotate-keys](#security-rotate-keys)
  - [security audit-export](#security-audit-export)
  - [security health](#security-health)
  - [security key-info](#security-key-info)
- [Examples](#examples)
- [Best Practices](#best-practices)

## Installation

Build the graphdb-admin CLI:

```bash
make build-all
# or
go build -o bin/graphdb-admin ./cmd/graphdb-admin
```

The binary will be created at `bin/graphdb-admin`.

## Global Configuration

The CLI supports authentication via JWT tokens or API keys. Configure authentication using:

1. **Command-line flags**:
   ```bash
   graphdb-admin security health --token="your-jwt-token"
   graphdb-admin security health --api-key="your-api-key"
   ```

2. **Environment variables**:
   ```bash
   export GRAPHDB_TOKEN="your-jwt-token"
   export GRAPHDB_SERVER_URL="http://localhost:8080"

   graphdb-admin security health
   ```

### Global Flags

All security subcommands support these flags:

- `--server-url URL`: GraphDB server URL (default: `http://localhost:8080`)
- `--token TOKEN`: JWT token for authentication (or set `GRAPHDB_TOKEN` env var)
- `--api-key KEY`: API key for authentication (or set `GRAPHDB_API_KEY` env var)

## Commands

### security init

Initialize security features or generate a master encryption key.

#### Usage

```bash
# Show security initialization guide
graphdb-admin security init

# Generate a new master encryption key
graphdb-admin security init --generate-key

# Generate key and save to file
graphdb-admin security init --generate-key --output=master.key
```

#### Flags

- `--generate-key`: Generate a new master encryption key
- `--key-length N`: Length of master key in bytes (default: 32 for AES-256)
- `--output FILE`: Output file for generated key (default: stdout)

#### Examples

```bash
# Generate a 256-bit master key
graphdb-admin security init --generate-key

# Save key to secure file
graphdb-admin security init --generate-key --output=/etc/graphdb/master.key
chmod 600 /etc/graphdb/master.key
```

#### Security Notes

- **Keep the master key secure!** Without it, encrypted data cannot be recovered.
- Store the key in a secure location (e.g., HashiCorp Vault, AWS Secrets Manager).
- Never commit the master key to version control.
- Use the `--output` flag to save keys directly to a file with restricted permissions.

---

### security rotate-keys

Rotate the encryption keys used for data-at-rest encryption.

#### Usage

```bash
graphdb-admin security rotate-keys --token="your-jwt-token"
```

#### Authentication

Requires admin authentication via `--token` or `--api-key`.

#### How It Works

1. Generates a new Key Encryption Key (KEK)
2. Assigns it a new version number
3. Retains old keys for decrypting existing data
4. All new data will be encrypted with the new key version

#### Examples

```bash
# Rotate keys using JWT token
export GRAPHDB_TOKEN="eyJhbGci..."
graphdb-admin security rotate-keys

# Rotate keys using API key
graphdb-admin security rotate-keys --api-key="your-api-key-here"

# Rotate keys on remote server
graphdb-admin security rotate-keys \
  --server-url="https://graphdb.example.com" \
  --token="$GRAPHDB_TOKEN"
```

#### Best Practices

- Rotate keys every 90 days for compliance (PCI-DSS, HIPAA)
- Always verify key rotation succeeded using `security key-info`
- Monitor audit logs for key rotation events
- Document key rotation in your change management system

---

### security audit-export

Export audit logs for compliance, analysis, or archival.

#### Usage

```bash
graphdb-admin security audit-export --token="your-jwt-token"
```

#### Flags

- `--output FILE`: Output file for audit logs (default: `audit-export.json`)
- `--user-id ID`: Filter logs by user ID
- `--action ACTION`: Filter logs by action (e.g., `create`, `read`, `update`, `delete`)
- `--start-time TIME`: Filter logs from this time (RFC3339 format)
- `--end-time TIME`: Filter logs until this time (RFC3339 format)

#### Examples

```bash
# Export all audit logs
graphdb-admin security audit-export \
  --token="$GRAPHDB_TOKEN" \
  --output=audit-2025-11.json

# Export logs for specific user
graphdb-admin security audit-export \
  --token="$GRAPHDB_TOKEN" \
  --user-id="alice@example.com" \
  --output=audit-alice.json

# Export logs for specific time range
graphdb-admin security audit-export \
  --token="$GRAPHDB_TOKEN" \
  --start-time="2025-11-01T00:00:00Z" \
  --end-time="2025-11-30T23:59:59Z" \
  --output=audit-november.json

# Export only write operations
graphdb-admin security audit-export \
  --token="$GRAPHDB_TOKEN" \
  --action="write" \
  --output=audit-writes.json
```

#### Output Format

The exported file is JSON containing:

```json
{
  "events": [
    {
      "id": "uuid",
      "timestamp": "2025-11-23T10:30:00Z",
      "user_id": "alice@example.com",
      "action": "write",
      "resource": "/nodes",
      "ip_address": "192.168.1.100",
      "user_agent": "curl/7.68.0",
      "status_code": 201
    }
  ],
  "total": 1234,
  "export_time": "2025-11-23T10:35:00Z"
}
```

---

### security health

Check the health status of all security components.

#### Usage

```bash
graphdb-admin security health --token="your-jwt-token"
```

#### Output

Displays the status of:

- **Encryption**: Enabled/disabled, key statistics
- **TLS**: Enabled/disabled
- **Audit Logging**: Enabled/disabled, event count
- **Authentication**: JWT and API key status

#### Examples

```bash
# Check security health on local server
graphdb-admin security health --token="$GRAPHDB_TOKEN"

# Check security health on remote server
graphdb-admin security health \
  --server-url="https://graphdb.example.com" \
  --token="$GRAPHDB_TOKEN"

# Use in monitoring scripts
if graphdb-admin security health --token="$TOKEN" | grep -q "Status: healthy"; then
  echo "Security OK"
else
  echo "Security issue detected!"
  exit 1
fi
```

#### Sample Output

```
=== Security Health Check ===

Status: healthy
Timestamp: 2025-11-23T10:00:00Z

Security Components:
  ✓ Encryption: Enabled
    - Total keys: 3
    - Active version: 3
  ✓ TLS: Enabled
  ✓ Audit Logging: Enabled
    - Total events: 15432
  ✓ JWT Authentication: Enabled
  ✓ API Key Authentication: Enabled
```

---

### security key-info

Display information about encryption keys.

#### Usage

```bash
graphdb-admin security key-info --token="your-jwt-token"
```

#### Output

Shows:

- Active key version
- Total number of keys
- Key history with creation timestamps
- Compliance recommendations

#### Examples

```bash
# View key information
graphdb-admin security key-info --token="$GRAPHDB_TOKEN"

# Check if key rotation is needed
graphdb-admin security key-info --token="$TOKEN" | grep "Version"
```

#### Sample Output

```
=== Encryption Key Information ===

Active Key Version: 3
Total Keys: 3

Key History:
  Version 1: Created 2025-08-01T00:00:00Z
  Version 2: Created 2025-10-01T00:00:00Z
  Version 3: Created 2025-11-01T00:00:00Z (active)

Note: Key rotation is recommended every 90 days for compliance.
```

---

## Complete Examples

### Setting Up a New GraphDB Instance with Encryption

```bash
# 1. Generate master encryption key
graphdb-admin security init --generate-key --output=/etc/graphdb/master.key
chmod 600 /etc/graphdb/master.key

# 2. Set environment variables
export ENCRYPTION_ENABLED=true
export ENCRYPTION_MASTER_KEY=$(cat /etc/graphdb/master.key)
export ADMIN_PASSWORD="SecureP@ssw0rd!"

# 3. Start the server
./bin/server --port=8080 --data=/var/lib/graphdb

# 4. Login and verify security
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"SecureP@ssw0rd!"}' \
  | jq -r '.access_token')

export GRAPHDB_TOKEN="$TOKEN"

# 5. Check security health
graphdb-admin security health
```

### Monthly Security Maintenance

```bash
#!/bin/bash
# Monthly security maintenance script

export GRAPHDB_TOKEN="your-token-here"
MONTH=$(date +%Y-%m)

# 1. Export audit logs
graphdb-admin security audit-export \
  --output="audit-$MONTH.json"

# 2. Check security health
graphdb-admin security health > "health-$MONTH.txt"

# 3. Rotate encryption keys (every 90 days)
LAST_ROTATION_DAYS_AGO=95
if [ $LAST_ROTATION_DAYS_AGO -ge 90 ]; then
  graphdb-admin security rotate-keys
  echo "Keys rotated on $(date)" >> key-rotation.log
fi

# 4. Archive logs
tar czf "security-reports-$MONTH.tar.gz" \
  audit-$MONTH.json \
  health-$MONTH.txt \
  key-rotation.log
```

### Incident Response: Export Specific User Activity

```bash
# Investigate suspicious activity by user
graphdb-admin security audit-export \
  --token="$GRAPHDB_TOKEN" \
  --user-id="suspicious@example.com" \
  --start-time="2025-11-20T00:00:00Z" \
  --end-time="2025-11-23T23:59:59Z" \
  --output="investigation-user-activity.json"

# Analyze the exported data
jq '.events[] | select(.status_code >= 400)' investigation-user-activity.json
```

---

## Best Practices

### Key Management

1. **Secure Storage**: Store master encryption keys in a secrets management system:
   - HashiCorp Vault
   - AWS Secrets Manager
   - Azure Key Vault
   - Google Cloud Secret Manager

2. **Key Rotation**: Rotate encryption keys regularly:
   - Every 90 days for compliance (PCI-DSS, HIPAA)
   - After suspected compromise
   - When team members with key access leave

3. **Backup**: Keep secure backups of master keys:
   - Multiple encrypted copies in different locations
   - Include keys in disaster recovery plan
   - Test key recovery procedures regularly

### Audit Logging

1. **Regular Exports**: Export audit logs regularly for:
   - Compliance requirements (SOC 2, ISO 27001)
   - Forensic analysis
   - Long-term archival

2. **Monitoring**: Set up alerts for:
   - Failed authentication attempts
   - Privilege escalation
   - Unusual access patterns
   - Key rotation events

3. **Retention**: Follow your organization's retention policy:
   - Common: 1 year for operational logs
   - Common: 7 years for compliance logs

### Health Monitoring

1. **Automated Checks**: Run `security health` periodically:
   - Every 5 minutes via monitoring system
   - Alert on status changes
   - Track historical trends

2. **Dashboard Integration**: Parse CLI output for dashboards:
   ```bash
   graphdb-admin security health --token="$TOKEN" | grep "Status:"
   ```

3. **Pre-deployment Validation**: Always check security health before deployments

---

## Troubleshooting

### Authentication Errors

```
Error: Authentication required. Provide --token or --api-key
```

**Solution**: Ensure you've provided authentication:

```bash
# Get a JWT token first
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"your-password"}' \
  | jq -r '.access_token')

# Use the token
graphdb-admin security health --token="$TOKEN"
```

### Connection Errors

```
Error: Failed to make request: dial tcp: connect: connection refused
```

**Solution**: Verify server URL and that the server is running:

```bash
# Check if server is running
curl http://localhost:8080/health

# Use correct server URL
graphdb-admin security health --server-url="http://localhost:8080"
```

### Permission Errors

```
API request failed (status 403): Forbidden
```

**Solution**: Ensure your user has admin privileges and valid authentication.

---

## Integration with CI/CD

### GitLab CI Example

```yaml
security-audit:
  stage: compliance
  script:
    - export GRAPHDB_TOKEN="${CI_GRAPHDB_TOKEN}"
    - ./bin/graphdb-admin security audit-export --output=audit.json
    - ./bin/graphdb-admin security health
  artifacts:
    paths:
      - audit.json
  only:
    - schedules
```

### GitHub Actions Example

```yaml
name: Security Audit
on:
  schedule:
    - cron: '0 0 * * 0'  # Weekly on Sunday

jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Export Audit Logs
        env:
          GRAPHDB_TOKEN: ${{ secrets.GRAPHDB_TOKEN }}
        run: |
          ./bin/graphdb-admin security audit-export
          ./bin/graphdb-admin security health
```

---

## See Also

- [Security Quick Start Guide](SECURITY-QUICKSTART.md)
- [Security Integration Summary](../SECURITY-INTEGRATION-SUMMARY.md)
- [Encryption Architecture](ENCRYPTION_ARCHITECTURE.md)
- [API Documentation](API.md)
