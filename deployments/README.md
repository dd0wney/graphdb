# GraphDB Monitoring Stack Deployment

Complete monitoring stack for GraphDB with Prometheus, Grafana, and Alertmanager.

## Quick Start

### Prerequisites

- Docker and Docker Compose installed
- Ports available: 3000 (Grafana), 8080 (GraphDB), 9090 (Metrics), 9091 (Prometheus), 9093 (Alertmanager)

### Start the Stack

```bash
cd deployments

# Build GraphDB (if needed)
cd .. && make build && cd deployments

# Start all services
docker-compose -f docker-compose.monitoring.yml up -d

# Validate the setup
chmod +x validate-monitoring.sh
./validate-monitoring.sh
```

### Access the Services

- **GraphDB API**: http://localhost:8080
- **GraphDB Metrics**: http://localhost:9090/metrics
- **GraphDB Health**: http://localhost:9090/health
- **Prometheus UI**: http://localhost:9091
- **Grafana**: http://localhost:3000 (default: admin/admin)
- **Alertmanager**: http://localhost:9093

## Architecture

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
       │ scrapes
       ▼
┌─────────────┐
│   GraphDB   │  Port 8080 - API
│  + Metrics  │  Port 9090 - Prometheus metrics
│   Plugin    │
└─────────────┘
```

## Monitoring Components

### 1. GraphDB Server

Enterprise edition with Prometheus metrics plugin enabled.

**Exposed Ports:**
- 8080: GraphDB REST/GraphQL API
- 9090: Prometheus metrics endpoint

**Key Endpoints:**
- `/metrics` - Prometheus metrics
- `/health` - Liveness check
- `/health/ready` - Readiness check

### 2. Prometheus

Time-series database for metrics collection.

**Configuration:**
- Scrapes GraphDB every 15s
- Loads alert rules from `prometheus/graphdb-alerts.yml`
- 24 alert rules across 5 categories

**Key Features:**
- Automatic service discovery
- Alert rule evaluation
- PromQL query interface

### 3. Grafana

Metrics visualization and dashboards.

**Pre-configured:**
- Prometheus datasource
- GraphDB overview dashboard (15 panels)
- Auto-provisioned dashboards

**Dashboard Panels:**
- Health status
- Graph size (nodes/edges)
- Query latency (p50, p95, p99)
- Cache hit rate
- System resources (CPU, memory, disk)
- Backup status

### 4. Alertmanager

Alert routing and notification.

**Alert Levels:**
- Critical: PagerDuty integration (configure in `alertmanager/config.yml`)
- Warning: Slack notifications (configure in `alertmanager/config.yml`)

## Configuration

### Environment Variables

Create a `.env` file in the `deployments/` directory:

```bash
# GraphDB
GRAPHDB_LICENSE_KEY=your-license-key
JWT_SECRET=your-jwt-secret
ADMIN_PASSWORD=your-admin-password

# Grafana
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=secure-password
```

### Prometheus Configuration

Edit `prometheus/prometheus.yml` to:
- Adjust scrape intervals
- Add more targets
- Configure external labels

### Alert Rules

Edit `prometheus/graphdb-alerts.yml` to customize:
- Alert thresholds
- Evaluation intervals
- Notification routing

Example alert:
```yaml
- alert: HighQueryLatency
  expr: histogram_quantile(0.95, sum(rate(graphdb_query_duration_seconds_bucket[5m])) by (le)) > 0.5
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "High query latency detected"
```

### Grafana Dashboards

Dashboards are auto-provisioned from `grafana/dashboards/`.

To add a new dashboard:
1. Create dashboard in Grafana UI
2. Export as JSON
3. Save to `grafana/dashboards/`
4. Restart Grafana container

## Validation

Run the validation script to verify everything is working:

```bash
./validate-monitoring.sh
```

This script:
1. ✓ Checks all services are healthy
2. ✓ Verifies metrics are exposed
3. ✓ Generates test traffic
4. ✓ Confirms Prometheus scraping
5. ✓ Validates alert rules loaded
6. ✓ Checks Grafana datasource
7. ✓ Tests health endpoints
8. ✓ Verifies histogram buckets

## Troubleshooting

### GraphDB not starting

```bash
# Check logs
docker-compose -f docker-compose.monitoring.yml logs graphdb

# Verify license key
docker exec -it graphdb-server env | grep GRAPHDB_LICENSE_KEY
```

### Prometheus not scraping

```bash
# Check targets
curl http://localhost:9091/api/v1/targets | jq

# Verify GraphDB metrics accessible
curl http://localhost:9090/metrics
```

### Grafana dashboard not loading

```bash
# Check datasource
curl -u admin:admin http://localhost:3000/api/datasources | jq

# Check dashboard provisioning
docker exec -it grafana ls -la /var/lib/grafana/dashboards/
```

### Alerts not firing

```bash
# Check alert rules
curl http://localhost:9091/api/v1/rules | jq

# Check Alertmanager
curl http://localhost:9093/api/v2/alerts | jq
```

## Metrics Reference

### Graph Metrics
- `graphdb_graph_nodes_total` - Total nodes in graph
- `graphdb_graph_edges_total` - Total edges in graph

### Query Metrics
- `graphdb_query_duration_seconds` - Query latency histogram (labels: query_type, complexity)
- `graphdb_query_cache_hits_total` - Cache hits counter
- `graphdb_query_cache_misses_total` - Cache misses counter
- `graphdb_query_errors_total` - Query errors counter
- `graphdb_slow_queries_total` - Slow queries counter
- `graphdb_active_queries` - Active queries gauge

### System Metrics
- `graphdb_system_cpu_percent` - CPU usage percentage
- `graphdb_system_memory_used_bytes` - Memory used
- `graphdb_system_memory_total_bytes` - Total memory
- `graphdb_system_disk_used_bytes` - Disk used
- `graphdb_system_disk_total_bytes` - Total disk

### Backup Metrics
- `graphdb_backup_last_run_timestamp_seconds` - Last backup start time
- `graphdb_backup_last_success_timestamp_seconds` - Last successful backup
- `graphdb_backup_size_bytes` - Backup size
- `graphdb_backup_success_total` - Successful backups counter
- `graphdb_backup_failure_total` - Failed backups counter
- `graphdb_backup_in_progress` - Backup in progress (0 or 1)
- `graphdb_backup_duration_seconds` - Backup duration histogram

### Info Metric
- `graphdb_info{version, edition}` - GraphDB version and edition

## Example PromQL Queries

### Cache Hit Rate
```promql
100 * (
  rate(graphdb_query_cache_hits_total[5m]) /
  (rate(graphdb_query_cache_hits_total[5m]) + rate(graphdb_query_cache_misses_total[5m]))
)
```

### Query Error Rate
```promql
100 * (
  rate(graphdb_query_errors_total[5m]) /
  (rate(graphdb_query_errors_total[5m]) + rate(graphdb_query_duration_seconds_count[5m]))
)
```

### p95 Query Latency
```promql
histogram_quantile(0.95,
  sum(rate(graphdb_query_duration_seconds_bucket[5m])) by (le)
)
```

### Memory Usage Percentage
```promql
100 * (
  graphdb_system_memory_used_bytes /
  graphdb_system_memory_total_bytes
)
```

## Production Checklist

Before deploying to production:

- [ ] Set strong passwords in `.env` file
- [ ] Configure TLS/SSL for all services
- [ ] Set up persistent volumes for data
- [ ] Configure backup retention policies
- [ ] Set up PagerDuty/Slack integrations in Alertmanager
- [ ] Review and adjust alert thresholds
- [ ] Set up monitoring for the monitoring stack itself
- [ ] Configure firewall rules (only expose necessary ports)
- [ ] Enable authentication on Prometheus
- [ ] Set up log aggregation (ELK/Loki)
- [ ] Document runbooks for alert responses

## Maintenance

### Update Dashboards

```bash
# Export from Grafana UI, then:
docker cp new-dashboard.json grafana:/var/lib/grafana/dashboards/
docker-compose -f docker-compose.monitoring.yml restart grafana
```

### Rotate Credentials

```bash
# Update .env file, then:
docker-compose -f docker-compose.monitoring.yml down
docker-compose -f docker-compose.monitoring.yml up -d
```

### Clean Up Old Data

```bash
# Prometheus data
docker-compose -f docker-compose.monitoring.yml down
docker volume rm deployments_prometheus-data
docker-compose -f docker-compose.monitoring.yml up -d
```

## Stopping the Stack

```bash
# Stop all services
docker-compose -f docker-compose.monitoring.yml down

# Stop and remove volumes (data loss!)
docker-compose -f docker-compose.monitoring.yml down -v
```

## References

- [GraphDB Monitoring Setup Guide](../docs/MONITORING_SETUP.md)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [Alertmanager Documentation](https://prometheus.io/docs/alerting/latest/alertmanager/)
