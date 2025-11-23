# GraphDB Production Deployment Guide

Complete guide for deploying GraphDB to production with monitoring, security, and high availability.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Security Setup](#security-setup)
- [Monitoring Stack](#monitoring-stack)
- [API Documentation](#api-documentation)
- [Production Checklist](#production-checklist)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### Required

- Docker & Docker Compose (v20.10+)
- 4GB RAM minimum (8GB+ recommended)
- 20GB disk space minimum
- Ports available: 3000, 8080, 9090, 9091, 9093

### Optional

- Domain name with DNS access
- SSL/TLS certificates (or use auto-generation)
- SMTP server for alerting
- License key (for Enterprise features)

---

## Quick Start

### 1. Clone and Build

```bash
git clone https://github.com/dd0wney/graphdb.git
cd graphdb

# Build all binaries
make build-all
```

### 2. Initialize Security

```bash
# Generate master encryption key
./bin/graphdb-admin security init --generate-key --output=/etc/graphdb/master.key
chmod 600 /etc/graphdb/master.key

# Set environment variables
export ENCRYPTION_ENABLED=true
export ENCRYPTION_MASTER_KEY=$(cat /etc/graphdb/master.key)
export ADMIN_PASSWORD="YourSecurePassword123!"
export JWT_SECRET=$(openssl rand -hex 32)
```

### 3. Start with Monitoring

```bash
cd deployments

# Start the complete stack
docker-compose -f docker-compose.monitoring.yml up -d

# Wait for services to be healthy
./validate-monitoring.sh
```

### 4. Verify Deployment

```bash
# Check health
curl http://localhost:8080/health

# Login
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"YourSecurePassword123!"}' \
  | jq -r '.access_token')

# Check security health
./bin/graphdb-admin security health --token="$TOKEN"
```

---

## Security Setup

### Encryption at Rest

GraphDB supports AES-256-GCM encryption for all stored data.

**Environment Variables:**

```bash
ENCRYPTION_ENABLED=true
ENCRYPTION_MASTER_KEY=<64-character-hex-key>
ENCRYPTION_KEY_DIR=./data/keys  # Optional
```

**Key Management:**

```bash
# Generate master key
./bin/graphdb-admin security init --generate-key

# Rotate encryption keys (every 90 days recommended)
./bin/graphdb-admin security rotate-keys --token="$TOKEN"

# Check key information
./bin/graphdb-admin security key-info --token="$TOKEN"
```

### TLS/SSL Configuration

Enable TLS for encrypted network communication.

**Option 1: Auto-Generated Certificates (Development)**

```bash
export TLS_ENABLED=true
export TLS_AUTO_GENERATE=true
export TLS_HOSTS=localhost,graphdb.example.com
export TLS_ORGANIZATION="Your Company"
```

**Option 2: Custom Certificates (Production)**

```bash
export TLS_ENABLED=true
export TLS_CERT_FILE=/etc/graphdb/tls/cert.pem
export TLS_KEY_FILE=/etc/graphdb/tls/key.pem
export TLS_CA_FILE=/etc/graphdb/tls/ca.pem  # Optional
export TLS_MIN_VERSION=1.3  # TLS 1.3 recommended
```

### Authentication

GraphDB supports both JWT and API key authentication.

**JWT Configuration:**

```bash
export JWT_SECRET=<your-secret-key>
export JWT_EXPIRATION=24h  # Token expiration
```

**API Key Management:**

```bash
# Create API key via GraphDB admin API
curl -X POST http://localhost:8080/api/keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"production-api-key","permissions":["read","write"]}'
```

### Audit Logging

All requests are automatically logged for compliance.

**Export Audit Logs:**

```bash
# Export all logs
./bin/graphdb-admin security audit-export \
  --token="$TOKEN" \
  --output=audit-$(date +%Y-%m).json

# Export with filters
./bin/graphdb-admin security audit-export \
  --token="$TOKEN" \
  --user-id="alice@example.com" \
  --start-time="2025-11-01T00:00:00Z" \
  --end-time="2025-11-30T23:59:59Z" \
  --output=audit-november.json
```

---

## Monitoring Stack

### Architecture

```
┌─────────────┐
│   Grafana   │  Port 3000 - Visualization
└──────┬──────┘
       │
       ▼
┌─────────────┐     ┌──────────────┐
│ Prometheus  │────▶│ Alertmanager │  Port 9093 - Alerts
└──────┬──────┘     └──────────────┘
       │
       ▼
┌─────────────┐
│   GraphDB   │  Port 8080 - API
│             │  Port 9090 - Metrics
└─────────────┘
```

### Access Points

- **GraphDB API**: http://localhost:8080
- **GraphDB Metrics**: http://localhost:9090/metrics
- **Prometheus**: http://localhost:9091
- **Grafana**: http://localhost:3000 (admin/admin)
- **Alertmanager**: http://localhost:9093

### Alerts Configured

**36 total alerts across 6 categories:**

1. **Health** (2 alerts)
   - GraphDB instance down
   - Health check failing

2. **Performance** (6 alerts)
   - High/critical query latency
   - High/critical error rate
   - Slow queries
   - Low cache hit rate

3. **Resources** (6 alerts)
   - High/critical CPU usage
   - High/critical memory usage
   - High/critical disk usage

4. **Backup** (4 alerts)
   - Backup failures
   - Stale backups
   - Slow backups

5. **Data** (2 alerts)
   - Sudden graph size changes
   - Rapid growth detection

6. **Security** (12 alerts) - **NEW!**
   - Authentication failures (brute force detection)
   - Encryption status
   - Key rotation compliance
   - TLS certificate expiration
   - Audit log health
   - Suspicious activity detection
   - Unauthorized access attempts
   - Overall security health

### Configuring Alerts

Edit `deployments/alertmanager/config.yml` to configure notification channels:

```yaml
receivers:
  - name: 'email'
    email_configs:
      - to: 'ops@example.com'
        from: 'graphdb-alerts@example.com'
        smarthost: 'smtp.example.com:587'
        auth_username: 'alerts@example.com'
        auth_password: 'your-smtp-password'

  - name: 'slack'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/YOUR/SLACK/WEBHOOK'
        channel: '#graphdb-alerts'
        title: 'GraphDB Alert'

  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: 'your-pagerduty-service-key'
```

---

## API Documentation

### Access Documentation

GraphDB provides comprehensive API documentation with interactive explorers:

- **Documentation Portal**: https://your-domain.github.io/graphdb/
- **Swagger UI**: https://your-domain.github.io/graphdb/swagger.html
- **ReDoc**: https://your-domain.github.io/graphdb/redoc.html
- **OpenAPI Spec**: https://your-domain.github.io/graphdb/openapi.yaml

### Deploy to GitHub Pages

Documentation is automatically deployed when changes are pushed to the `docs/` directory.

**Manual Deployment:**

```bash
# GitHub Pages will automatically build from docs/ directory
# No additional steps required if .github/workflows/docs.yml is configured
```

### Local Testing

```bash
# Serve documentation locally
cd docs
python3 -m http.server 8888

# Access at http://localhost:8888
```

---

## Production Checklist

### Security ✅

- [ ] Change default admin password
- [ ] Generate unique JWT secret
- [ ] Enable encryption at rest
- [ ] Configure TLS/SSL with valid certificates
- [ ] Set up firewall rules (allow only 8080, 443)
- [ ] Enable audit logging
- [ ] Configure key rotation schedule
- [ ] Set up backup encryption
- [ ] Review security alert thresholds
- [ ] Implement rate limiting (if needed)

### Monitoring ✅

- [ ] Configure Prometheus retention
- [ ] Set up Alertmanager notifications (email/Slack/PagerDuty)
- [ ] Import Grafana dashboards
- [ ] Test alert routing
- [ ] Configure log retention policies
- [ ] Set up external monitoring (uptime checks)
- [ ] Create runbooks for alerts
- [ ] Test failover procedures

### Performance ✅

- [ ] Tune memory allocation (JVM/Go runtime)
- [ ] Configure cache sizes
- [ ] Set appropriate connection pool limits
- [ ] Enable compression for large responses
- [ ] Configure query timeout limits
- [ ] Set up CDN for static assets (if applicable)
- [ ] Test under expected load
- [ ] Benchmark query performance

### High Availability ✅

- [ ] Set up replication (if using HA mode)
- [ ] Configure automated backups
- [ ] Test backup restoration
- [ ] Set up health check endpoints
- [ ] Configure load balancer (if multi-node)
- [ ] Test failover scenarios
- [ ] Document recovery procedures
- [ ] Set up monitoring for replication lag

### Documentation ✅

- [ ] Deploy API documentation
- [ ] Create internal runbooks
- [ ] Document custom configurations
- [ ] Train operations team
- [ ] Create disaster recovery plan
- [ ] Document escalation procedures

---

## Troubleshooting

### Common Issues

#### GraphDB Won't Start

```bash
# Check logs
docker logs graphdb-server

# Common causes:
# 1. Port already in use
sudo lsof -i :8080

# 2. Invalid license key
# Check GRAPHDB_LICENSE_KEY environment variable

# 3. Encryption key mismatch
# Ensure ENCRYPTION_MASTER_KEY matches the key used to encrypt existing data
```

#### High Memory Usage

```bash
# Check current memory
docker stats graphdb-server

# Adjust memory limits in docker-compose.yml
services:
  graphdb:
    deploy:
      resources:
        limits:
          memory: 4G
        reservations:
          memory: 2G
```

#### Slow Queries

```bash
# Check query latency metrics
curl http://localhost:9090/metrics | grep query_duration

# Enable query logging
export GRAPHDB_SLOW_QUERY_THRESHOLD=100ms

# Check Grafana dashboard for bottlenecks
```

#### Authentication Failures

```bash
# Reset admin password
docker exec -it graphdb-server /bin/sh
# Inside container:
./bin/graphdb-admin security init --reset-admin-password

# Check JWT secret is set
echo $JWT_SECRET
```

#### Prometheus Not Scraping Metrics

```bash
# Check Prometheus targets
curl http://localhost:9091/targets

# Verify GraphDB metrics endpoint
curl http://localhost:9090/metrics

# Check network connectivity
docker exec prometheus ping graphdb
```

### Getting Help

- **Documentation**: https://your-domain.github.io/graphdb/
- **Issues**: https://github.com/dd0wney/graphdb/issues
- **Discussions**: https://github.com/dd0wney/graphdb/discussions
- **Email**: support@clusographdb.com

---

## Maintenance

### Regular Tasks

**Daily:**
- Monitor alert notifications
- Check Grafana dashboards
- Review error logs

**Weekly:**
- Export audit logs
- Review security alerts
- Check backup status

**Monthly:**
- Review and archive old audit logs
- Update dependencies
- Review and tune alert thresholds
- Performance testing

**Quarterly:**
- Rotate encryption keys
- Review TLS certificates
- Disaster recovery drills
- Security audit

### Backup Strategy

```bash
# Automated daily backups
0 2 * * * /opt/graphdb/scripts/backup-graphdb.sh

# Test restoration monthly
0 3 1 * * /opt/graphdb/scripts/test-dr.sh
```

---

## Next Steps

1. **Review** this guide and complete the production checklist
2. **Test** the deployment in a staging environment
3. **Monitor** for 24-48 hours before going live
4. **Document** any custom configurations
5. **Train** your operations team

**Need help?** Contact support@clusographdb.com or open an issue on GitHub.

---

**Last Updated**: 2025-11-23
**Version**: 1.0.0
**Tested With**: GraphDB v0.1.0
