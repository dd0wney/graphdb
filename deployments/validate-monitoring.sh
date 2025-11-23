#!/bin/bash
# Monitoring Stack Validation Script
# Tests that Prometheus, Grafana, and GraphDB metrics are working correctly

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
GRAPHDB_URL="http://localhost:8080"
METRICS_URL="http://localhost:9090"
PROMETHEUS_URL="http://localhost:9091"
GRAFANA_URL="http://localhost:3000"
ALERTMANAGER_URL="http://localhost:9093"

RETRIES=30
RETRY_DELAY=2

# Utility functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

wait_for_service() {
    local url=$1
    local name=$2
    local retries=$RETRIES

    log_info "Waiting for $name to be ready..."

    while [ $retries -gt 0 ]; do
        if curl -s -f -o /dev/null "$url"; then
            log_info "$name is ready!"
            return 0
        fi

        retries=$((retries-1))
        if [ $retries -gt 0 ]; then
            echo -n "."
            sleep $RETRY_DELAY
        fi
    done

    log_error "$name failed to start after $RETRIES attempts"
    return 1
}

check_metric() {
    local metric_name=$1
    local url=$2

    if curl -s "$url" | grep -q "$metric_name"; then
        log_info "✓ Metric found: $metric_name"
        return 0
    else
        log_error "✗ Metric not found: $metric_name"
        return 1
    fi
}

query_prometheus() {
    local query=$1
    local url="${PROMETHEUS_URL}/api/v1/query?query=${query}"

    response=$(curl -s "$url")

    if echo "$response" | grep -q '"status":"success"'; then
        log_info "✓ PromQL query succeeded: $query"
        echo "$response" | jq -r '.data.result[0].value[1]' 2>/dev/null || echo "N/A"
        return 0
    else
        log_error "✗ PromQL query failed: $query"
        return 1
    fi
}

echo "========================================"
echo "GraphDB Monitoring Stack Validation"
echo "========================================"
echo ""

# Step 1: Wait for all services to be ready
log_info "Step 1: Checking service health..."
wait_for_service "${METRICS_URL}/health" "GraphDB metrics endpoint" || exit 1
wait_for_service "${PROMETHEUS_URL}/-/ready" "Prometheus" || exit 1
wait_for_service "${GRAFANA_URL}/api/health" "Grafana" || exit 1
wait_for_service "${ALERTMANAGER_URL}/-/ready" "Alertmanager" || exit 1
echo ""

# Step 2: Verify GraphDB metrics endpoint
log_info "Step 2: Verifying GraphDB metrics..."
metrics_count=0

check_metric "graphdb_graph_nodes_total" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_graph_edges_total" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_query_duration_seconds" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_query_cache_hits_total" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_query_cache_misses_total" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_query_errors_total" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_slow_queries_total" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_active_queries" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_system_cpu_percent" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_system_memory_used_bytes" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_system_memory_total_bytes" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_backup_last_run_timestamp_seconds" "$METRICS_URL/metrics" && ((metrics_count++))
check_metric "graphdb_info" "$METRICS_URL/metrics" && ((metrics_count++))

log_info "Found $metrics_count/13 expected metrics"
echo ""

# Step 3: Generate test traffic to GraphDB
log_info "Step 3: Generating test traffic..."

# Create some nodes
for i in {1..10}; do
    curl -s -X POST "${GRAPHDB_URL}/nodes" \
        -H "Content-Type: application/json" \
        -d "{\"type\":\"test\",\"properties\":{\"name\":\"node-$i\",\"test_run\":\"validation\"}}" \
        > /dev/null
done

# Query nodes
for i in {1..20}; do
    curl -s "${GRAPHDB_URL}/nodes?limit=100" > /dev/null
done

log_info "Generated test traffic: 10 writes, 20 reads"
echo ""

# Wait for metrics to propagate
log_info "Waiting 15s for metrics to propagate..."
sleep 15

# Step 4: Verify Prometheus is scraping metrics
log_info "Step 4: Verifying Prometheus scraping..."

# Check targets are up
targets_response=$(curl -s "${PROMETHEUS_URL}/api/v1/targets")
if echo "$targets_response" | grep -q '"health":"up"'; then
    log_info "✓ Prometheus targets are up"
else
    log_warn "⚠ Some Prometheus targets may be down"
fi

# Query some metrics
log_info "Querying metrics from Prometheus..."
query_prometheus "up{job=\"graphdb\"}"
query_prometheus "graphdb_graph_nodes_total"
query_prometheus "graphdb_graph_edges_total"
query_prometheus "graphdb_active_queries"
echo ""

# Step 5: Verify alert rules are loaded
log_info "Step 5: Verifying alert rules..."

rules_response=$(curl -s "${PROMETHEUS_URL}/api/v1/rules")
if echo "$rules_response" | grep -q '"name":"graphdb_health"'; then
    log_info "✓ Alert rules loaded: graphdb_health"
else
    log_error "✗ Alert rules not loaded properly"
fi

if echo "$rules_response" | grep -q '"name":"graphdb_performance"'; then
    log_info "✓ Alert rules loaded: graphdb_performance"
else
    log_error "✗ Alert rules not loaded properly"
fi

if echo "$rules_response" | grep -q '"name":"graphdb_resources"'; then
    log_info "✓ Alert rules loaded: graphdb_resources"
else
    log_error "✗ Alert rules not loaded properly"
fi
echo ""

# Step 6: Check Grafana datasource
log_info "Step 6: Verifying Grafana configuration..."

# Try to get datasources (requires auth)
datasources_response=$(curl -s -u admin:admin "${GRAFANA_URL}/api/datasources")
if echo "$datasources_response" | grep -q '"type":"prometheus"'; then
    log_info "✓ Grafana Prometheus datasource configured"
else
    log_warn "⚠ Could not verify Grafana datasource"
fi

# Check dashboards
dashboards_response=$(curl -s -u admin:admin "${GRAFANA_URL}/api/search?type=dash-db")
if echo "$dashboards_response" | grep -q "GraphDB"; then
    log_info "✓ GraphDB dashboard found in Grafana"
else
    log_warn "⚠ GraphDB dashboard not found - may need to import manually"
fi
echo ""

# Step 7: Test health endpoints
log_info "Step 7: Testing health endpoints..."

health_response=$(curl -s "${METRICS_URL}/health")
if echo "$health_response" | grep -q '"status":"ok"'; then
    log_info "✓ Liveness check passed"
else
    log_error "✗ Liveness check failed"
fi

ready_response=$(curl -s "${METRICS_URL}/health/ready")
if echo "$ready_response" | grep -q '"status":"ready"'; then
    log_info "✓ Readiness check passed"
else
    log_warn "⚠ Readiness check not passed"
fi
echo ""

# Step 8: Verify metrics histogram buckets
log_info "Step 8: Verifying histogram metrics..."

if curl -s "$METRICS_URL/metrics" | grep -q "graphdb_query_duration_seconds_bucket"; then
    bucket_count=$(curl -s "$METRICS_URL/metrics" | grep "graphdb_query_duration_seconds_bucket" | wc -l)
    log_info "✓ Query duration histogram with $bucket_count buckets"
else
    log_error "✗ Query duration histogram not found"
fi
echo ""

# Summary
echo "========================================"
echo "Validation Summary"
echo "========================================"
log_info "Services:"
log_info "  - GraphDB:       http://localhost:8080"
log_info "  - Metrics:       http://localhost:9090/metrics"
log_info "  - Prometheus UI: http://localhost:9091"
log_info "  - Grafana:       http://localhost:3000 (admin/admin)"
log_info "  - Alertmanager:  http://localhost:9093"
echo ""
log_info "Next steps:"
log_info "  1. Open Grafana: http://localhost:3000"
log_info "  2. View Prometheus targets: http://localhost:9091/targets"
log_info "  3. Check alert rules: http://localhost:9091/alerts"
log_info "  4. Query metrics: http://localhost:9091/graph"
echo ""
log_info "Validation complete! ✓"
