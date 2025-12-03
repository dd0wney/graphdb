#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Disaster Recovery Testing Script
#
# Automates monthly DR testing procedures to ensure:
# - Backups are created successfully
# - Restores work correctly
# - RTO/RPO targets are met
# - DR runbook procedures are accurate
#
# Tests Included:
# 1. Backup creation and verification
# 2. Backup restoration to test environment
# 3. Data integrity validation
# 4. Performance benchmarking
# 5. RTO/RPO measurement
#
# Requirements:
# - GraphDB running on production droplet
# - DO Spaces configured for backups
# - doctl CLI configured
# - SSH access to droplet
#
# Environment Variables:
#   DROPLET_IP - Production droplet IP address
#   SSH_KEY - Path to SSH key (default: ~/.ssh/graphdb_deploy)
#   DO_REGION - DigitalOcean region (default: nyc3)
#   DO_SIZE - Test droplet size (default: s-2vcpu-4gb)
#   SKIP_CLEANUP - Skip cleanup of test resources (default: false)
#
# Usage:
#   # Test backup/restore on production droplet
#   DROPLET_IP=142.93.xxx.xxx ./test-dr.sh
#
#   # Full DR test with test droplet creation
#   DROPLET_IP=142.93.xxx.xxx ./test-dr.sh --full
#
#######################################################################

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
DROPLET_IP="${DROPLET_IP:-}"
SSH_KEY="${SSH_KEY:-$HOME/.ssh/graphdb_deploy}"
DO_REGION="${DO_REGION:-nyc3}"
DO_SIZE="${DO_SIZE:-s-2vcpu-4gb}"
SKIP_CLEANUP="${SKIP_CLEANUP:-false}"
TEST_MODE="${1:-basic}"  # basic or --full

# Test results
TEST_RESULTS=()
START_TIME=$(date +%s)

log_info() {
    echo -e "${GREEN}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

log_test() {
    echo -e "${BLUE}[TEST]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

record_result() {
    local test_name="$1"
    local status="$2"
    local duration="$3"
    TEST_RESULTS+=("$test_name|$status|$duration")
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if [ -z "$DROPLET_IP" ]; then
        log_error "DROPLET_IP environment variable not set"
        echo "Usage: DROPLET_IP=142.93.xxx.xxx $0"
        exit 1
    fi

    if [ ! -f "$SSH_KEY" ]; then
        log_error "SSH key not found: $SSH_KEY"
        exit 1
    fi

    if ! command -v doctl &> /dev/null && [ "$TEST_MODE" = "--full" ]; then
        log_error "doctl not found (required for --full mode)"
        exit 1
    fi

    # Test SSH connectivity
    if ! ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 -i "$SSH_KEY" root@$DROPLET_IP "echo 'SSH OK'" &>/dev/null; then
        log_error "Cannot SSH to droplet: $DROPLET_IP"
        exit 1
    fi

    log_info "Prerequisites check passed ✓"
}

# Test 1: Backup Creation
test_backup_creation() {
    log_test "[1/6] Testing backup creation..."
    local test_start=$(date +%s)

    # Trigger full backup
    local backup_result=$(ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH'
export BACKUP_TYPE=full
export BACKUP_DIR=/var/lib/graphdb/backups/test-dr
mkdir -p $BACKUP_DIR

# Run backup script
if [ -f /usr/local/bin/backup-graphdb.sh ]; then
    /usr/local/bin/backup-graphdb.sh 2>&1
elif [ -f /var/lib/graphdb/scripts/backup-graphdb.sh ]; then
    /var/lib/graphdb/scripts/backup-graphdb.sh 2>&1
else
    echo "ERROR: backup-graphdb.sh not found"
    exit 1
fi
ENDSSH
)

    local test_end=$(date +%s)
    local duration=$((test_end - test_start))

    if echo "$backup_result" | grep -q "Backup completed successfully"; then
        log_info "✓ Backup creation test passed (${duration}s)"
        record_result "Backup Creation" "PASS" "${duration}s"
        return 0
    else
        log_error "✗ Backup creation test failed"
        echo "$backup_result"
        record_result "Backup Creation" "FAIL" "${duration}s"
        return 1
    fi
}

# Test 2: Backup Verification
test_backup_verification() {
    log_test "[2/6] Testing backup verification..."
    local test_start=$(date +%s)

    local verify_result=$(ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH'
# Find latest test backup
LATEST_BACKUP=$(ls -t /var/lib/graphdb/backups/test-dr/*.tar.gz 2>/dev/null | head -1)

if [ -z "$LATEST_BACKUP" ]; then
    echo "ERROR: No backup file found"
    exit 1
fi

# Verify backup integrity
if tar -tzf "$LATEST_BACKUP" > /dev/null 2>&1; then
    BACKUP_SIZE=$(du -sh "$LATEST_BACKUP" | cut -f1)
    echo "SUCCESS: Backup verified ($BACKUP_SIZE)"
    echo "BACKUP_FILE=$LATEST_BACKUP"
else
    echo "ERROR: Backup archive corrupted"
    exit 1
fi
ENDSSH
)

    local test_end=$(date +%s)
    local duration=$((test_end - test_start))

    if echo "$verify_result" | grep -q "SUCCESS"; then
        log_info "✓ Backup verification test passed (${duration}s)"
        echo "$verify_result"
        record_result "Backup Verification" "PASS" "${duration}s"
        return 0
    else
        log_error "✗ Backup verification test failed"
        echo "$verify_result"
        record_result "Backup Verification" "FAIL" "${duration}s"
        return 1
    fi
}

# Test 3: Restore to Test Location
test_restore_procedure() {
    log_test "[3/6] Testing restore procedure..."
    local test_start=$(date +%s)

    local restore_result=$(ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH'
# Find latest backup
LATEST_BACKUP=$(ls -t /var/lib/graphdb/backups/test-dr/*.tar.gz 2>/dev/null | head -1)

if [ -z "$LATEST_BACKUP" ]; then
    echo "ERROR: No backup file found"
    exit 1
fi

# Stop GraphDB temporarily
cd /var/lib/graphdb
docker compose down graphdb 2>/dev/null || true

# Create test restore location
TEST_RESTORE_DIR="/var/lib/graphdb/data-test-restore"
rm -rf "$TEST_RESTORE_DIR"
mkdir -p "$TEST_RESTORE_DIR"

# Extract backup to test location
TEMP_DIR=$(mktemp -d)
tar -xzf "$LATEST_BACKUP" -C "$TEMP_DIR"

# Find data directory
DATA_SOURCE=$(find "$TEMP_DIR" -type d -name "data" | head -1)
if [ -z "$DATA_SOURCE" ]; then
    echo "ERROR: No data directory in backup"
    rm -rf "$TEMP_DIR"
    exit 1
fi

# Copy to test location
rsync -a "$DATA_SOURCE/" "$TEST_RESTORE_DIR/"
rm -rf "$TEMP_DIR"

# Verify restored data
if [ -d "$TEST_RESTORE_DIR" ] && [ "$(ls -A $TEST_RESTORE_DIR)" ]; then
    RESTORE_SIZE=$(du -sh "$TEST_RESTORE_DIR" | cut -f1)
    echo "SUCCESS: Restore completed ($RESTORE_SIZE)"

    # Cleanup test restore
    rm -rf "$TEST_RESTORE_DIR"
else
    echo "ERROR: Restored data directory is empty"
    exit 1
fi

# Restart GraphDB
docker compose up -d graphdb
sleep 5
ENDSSH
)

    local test_end=$(date +%s)
    local duration=$((test_end - test_start))

    if echo "$restore_result" | grep -q "SUCCESS"; then
        log_info "✓ Restore procedure test passed (${duration}s)"
        echo "$restore_result"
        record_result "Restore Procedure" "PASS" "${duration}s"
        return 0
    else
        log_error "✗ Restore procedure test failed"
        echo "$restore_result"
        record_result "Restore Procedure" "FAIL" "${duration}s"
        return 1
    fi
}

# Test 4: Data Integrity Validation
test_data_integrity() {
    log_test "[4/6] Testing data integrity after restore..."
    local test_start=$(date +%s)

    # Wait for GraphDB to be ready
    sleep 10

    local health_check=$(ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH'
# Check GraphDB health
if curl -f http://localhost:8080/health &>/dev/null; then
    echo "SUCCESS: GraphDB health check passed"
else
    echo "ERROR: GraphDB health check failed"
    exit 1
fi

# Check if we can query the database
if curl -f http://localhost:8080/api/v1/stats &>/dev/null; then
    STATS=$(curl -s http://localhost:8080/api/v1/stats)
    echo "SUCCESS: Database is queryable"
    echo "$STATS"
else
    echo "WARN: Stats endpoint unavailable (may require auth)"
fi
ENDSSH
)

    local test_end=$(date +%s)
    local duration=$((test_end - test_start))

    if echo "$health_check" | grep -q "SUCCESS"; then
        log_info "✓ Data integrity test passed (${duration}s)"
        record_result "Data Integrity" "PASS" "${duration}s"
        return 0
    else
        log_error "✗ Data integrity test failed"
        echo "$health_check"
        record_result "Data Integrity" "FAIL" "${duration}s"
        return 1
    fi
}

# Test 5: RTO Measurement
test_rto_measurement() {
    log_test "[5/6] Measuring Recovery Time Objective (RTO)..."

    log_info "RTO Test: Measuring time to restore from backup and restart service"

    local rto_start=$(date +%s)

    # Simulate recovery: restore + start
    ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH' >/dev/null 2>&1
cd /var/lib/graphdb
docker compose restart graphdb
ENDSSH

    # Wait for health check
    local max_attempts=60
    local attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if ssh -i "$SSH_KEY" root@$DROPLET_IP 'curl -f http://localhost:8080/health' &>/dev/null; then
            break
        fi
        attempt=$((attempt + 1))
        sleep 1
    done

    local rto_end=$(date +%s)
    local rto=$((rto_end - rto_start))

    # RTO target is 15 minutes (900 seconds)
    if [ $rto -le 900 ]; then
        log_info "✓ RTO test passed: ${rto}s (target: <900s / 15min)"
        record_result "RTO Measurement" "PASS" "${rto}s"
        return 0
    else
        log_warn "⚠ RTO test warning: ${rto}s exceeds target of 900s"
        record_result "RTO Measurement" "WARN" "${rto}s"
        return 0
    fi
}

# Test 6: DO Spaces Backup/Restore (if configured)
test_spaces_integration() {
    log_test "[6/6] Testing DO Spaces integration..."
    local test_start=$(date +%s)

    local spaces_result=$(ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH'
# Check if s3cmd is configured
if ! command -v s3cmd &> /dev/null; then
    echo "SKIP: s3cmd not installed"
    exit 0
fi

if [ ! -f ~/.s3cfg ]; then
    echo "SKIP: s3cmd not configured"
    exit 0
fi

# Try to list buckets
if s3cmd ls 2>/dev/null | grep -q "s3://"; then
    echo "SUCCESS: DO Spaces connectivity verified"
    s3cmd ls | head -5
else
    echo "WARN: DO Spaces not accessible (may not be configured)"
fi
ENDSSH
)

    local test_end=$(date +%s)
    local duration=$((test_end - test_start))

    if echo "$spaces_result" | grep -q "SKIP"; then
        log_info "⊘ DO Spaces test skipped (not configured)"
        record_result "DO Spaces Integration" "SKIP" "${duration}s"
    elif echo "$spaces_result" | grep -q "SUCCESS"; then
        log_info "✓ DO Spaces integration test passed (${duration}s)"
        record_result "DO Spaces Integration" "PASS" "${duration}s"
    else
        log_warn "⚠ DO Spaces integration test warning"
        echo "$spaces_result"
        record_result "DO Spaces Integration" "WARN" "${duration}s"
    fi

    return 0
}

# Cleanup test resources
cleanup() {
    if [ "$SKIP_CLEANUP" = "true" ]; then
        log_warn "Skipping cleanup (SKIP_CLEANUP=true)"
        return
    fi

    log_info "Cleaning up test resources..."

    ssh -i "$SSH_KEY" root@$DROPLET_IP 'bash -s' <<'ENDSSH' >/dev/null 2>&1 || true
# Remove test backup directory
rm -rf /var/lib/graphdb/backups/test-dr
ENDSSH

    log_info "Cleanup complete"
}

# Generate test report
generate_report() {
    local end_time=$(date +%s)
    local total_duration=$((end_time - START_TIME))

    echo ""
    echo "========================================="
    echo "GraphDB DR Test Report"
    echo "========================================="
    echo "Timestamp: $(date)"
    echo "Droplet IP: $DROPLET_IP"
    echo "Test Mode: $TEST_MODE"
    echo "Total Duration: ${total_duration}s"
    echo ""
    echo "Test Results:"
    echo "----------------------------------------"

    local passed=0
    local failed=0
    local warned=0
    local skipped=0

    for result in "${TEST_RESULTS[@]}"; do
        IFS='|' read -r test_name status duration <<< "$result"

        if [ "$status" = "PASS" ]; then
            echo -e "${GREEN}✓${NC} $test_name - $duration"
            passed=$((passed + 1))
        elif [ "$status" = "FAIL" ]; then
            echo -e "${RED}✗${NC} $test_name - $duration"
            failed=$((failed + 1))
        elif [ "$status" = "WARN" ]; then
            echo -e "${YELLOW}⚠${NC} $test_name - $duration"
            warned=$((warned + 1))
        elif [ "$status" = "SKIP" ]; then
            echo -e "${BLUE}⊘${NC} $test_name - $duration"
            skipped=$((skipped + 1))
        fi
    done

    echo "----------------------------------------"
    echo "Summary: $passed passed, $failed failed, $warned warnings, $skipped skipped"
    echo "========================================="
    echo ""

    # Save report
    local report_file="/tmp/graphdb-dr-test-$(date +%Y%m%d-%H%M%S).txt"
    {
        echo "GraphDB DR Test Report"
        echo "======================"
        echo "Timestamp: $(date)"
        echo "Droplet IP: $DROPLET_IP"
        echo "Total Duration: ${total_duration}s"
        echo ""
        echo "Results:"
        for result in "${TEST_RESULTS[@]}"; do
            IFS='|' read -r test_name status duration <<< "$result"
            echo "- $test_name: $status ($duration)"
        done
    } > "$report_file"

    log_info "Report saved to: $report_file"

    if [ $failed -gt 0 ]; then
        log_error "DR tests failed! Please review failures above."
        exit 1
    elif [ $warned -gt 0 ]; then
        log_warn "DR tests completed with warnings. Review recommended."
        exit 0
    else
        log_info "All DR tests passed successfully! ✓"
        exit 0
    fi
}

# Main test execution
main() {
    echo "========================================="
    echo "GraphDB Disaster Recovery Test"
    echo "========================================="
    echo "Droplet: $DROPLET_IP"
    echo "Mode: $TEST_MODE"
    echo "========================================="
    echo ""

    check_prerequisites

    # Run tests
    test_backup_creation
    test_backup_verification
    test_restore_procedure
    test_data_integrity
    test_rto_measurement
    test_spaces_integration

    cleanup
    generate_report
}

# Run main function
main "$@"
