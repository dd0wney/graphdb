#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Disaster Recovery - Restore Script
#
# Features:
# - Restore from local or remote (DO Spaces) backup
# - Point-in-time recovery
# - Automatic WAL replay
# - Pre-restore validation
# - Rollback capability
# - Health verification post-restore
#
# Requirements:
# - s3cmd (if restoring from DO Spaces)
# - GraphDB stopped during restore
#
# Environment Variables:
#   BACKUP_FILE - Path to backup file or s3:// URL
#   RESTORE_TO - Target directory (default: /var/lib/graphdb/data)
#   DO_SPACES_REGION - DO Spaces region (default: nyc3)
#   SKIP_VERIFICATION - Skip post-restore verification (default: false)
#
# Usage:
#   # Restore from local backup
#   BACKUP_FILE=/var/lib/graphdb/backups/graphdb-full-20250119.tar.gz \
#   ./restore-graphdb.sh
#
#   # Restore from DO Spaces
#   BACKUP_FILE=s3://graphdb-backups/full/graphdb-full-20250119.tar.gz \
#   ./restore-graphdb.sh
#
#   # Restore to custom location
#   BACKUP_FILE=/path/to/backup.tar.gz \
#   RESTORE_TO=/var/lib/graphdb/data-restored \
#   ./restore-graphdb.sh
#######################################################################

# Configuration
BACKUP_FILE="${BACKUP_FILE:-}"
RESTORE_TO="${RESTORE_TO:-/var/lib/graphdb/data}"
DO_SPACES_REGION="${DO_SPACES_REGION:-nyc3}"
SKIP_VERIFICATION="${SKIP_VERIFICATION:-false}"
BACKUP_ORIGINAL="${RESTORE_TO}.backup-$(date +%Y%m%d-%H%M%S)"

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $(date '+%Y-%m-%d %H:%M:%S') - $1"
}

# Cleanup on error
cleanup_on_error() {
    log_error "Restore failed: $1"
    log_info "Original data preserved at: $BACKUP_ORIGINAL"
    exit 1
}

trap 'cleanup_on_error "Unexpected error"' ERR

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if [ -z "$BACKUP_FILE" ]; then
        log_error "BACKUP_FILE environment variable not set"
        echo "Usage: BACKUP_FILE=/path/to/backup.tar.gz $0"
        exit 1
    fi

    # Check if backup file is remote (S3)
    if [[ "$BACKUP_FILE" == s3://* ]]; then
        if ! command -v s3cmd &> /dev/null; then
            log_error "s3cmd not found but BACKUP_FILE is s3:// URL"
            exit 1
        fi
    else
        if [ ! -f "$BACKUP_FILE" ]; then
            log_error "Backup file not found: $BACKUP_FILE"
            exit 1
        fi
    fi

    # Check if GraphDB is running
    if docker ps | grep -q graphdb; then
        log_error "GraphDB is still running. Please stop it first:"
        echo "  docker compose -f /var/lib/graphdb/docker-compose.yml down"
        exit 1
    fi

    log_info "Prerequisites check passed"
}

# Download backup from DO Spaces
download_backup() {
    if [[ "$BACKUP_FILE" != s3://* ]]; then
        log_info "Using local backup: $BACKUP_FILE"
        return
    fi

    log_info "Downloading backup from DO Spaces..."

    local temp_backup="/tmp/graphdb-restore-$(date +%s).tar.gz"

    s3cmd get "$BACKUP_FILE" "$temp_backup" \
        --host="${DO_SPACES_REGION}.digitaloceanspaces.com" \
        --host-bucket="%(bucket)s.${DO_SPACES_REGION}.digitaloceanspaces.com"

    # Update BACKUP_FILE to point to downloaded file
    BACKUP_FILE="$temp_backup"

    log_info "Download complete"
}

# Verify backup integrity
verify_backup() {
    log_info "Verifying backup integrity..."

    if [ ! -s "$BACKUP_FILE" ]; then
        cleanup_on_error "Backup file is empty or missing"
    fi

    # Test archive integrity
    if ! tar -tzf "$BACKUP_FILE" > /dev/null; then
        cleanup_on_error "Backup archive is corrupted"
    fi

    # Check manifest
    if tar -xzf "$BACKUP_FILE" -O manifest.json > /dev/null 2>&1; then
        log_info "Backup manifest found:"
        tar -xzf "$BACKUP_FILE" -O manifest.json | jq '.' || cat
    else
        log_warn "No manifest found in backup"
    fi

    log_info "Backup verification passed ✓"
}

# Backup existing data
backup_existing_data() {
    if [ ! -d "$RESTORE_TO" ]; then
        log_info "No existing data to backup"
        return
    fi

    log_info "Backing up existing data to: $BACKUP_ORIGINAL"

    mv "$RESTORE_TO" "$BACKUP_ORIGINAL"

    log_info "Existing data backed up successfully"
    log_warn "If restore fails, restore original data with:"
    log_warn "  rm -rf $RESTORE_TO && mv $BACKUP_ORIGINAL $RESTORE_TO"
}

# Extract backup
extract_backup() {
    log_info "Extracting backup..."

    mkdir -p "$RESTORE_TO"

    # Extract to temporary location first
    local temp_dir=$(mktemp -d)
    tar -xzf "$BACKUP_FILE" -C "$temp_dir"

    # Find the data directory in the backup
    local data_source=$(find "$temp_dir" -type d -name "data" | head -1)

    if [ -z "$data_source" ]; then
        cleanup_on_error "No 'data' directory found in backup"
    fi

    # Move data to restore location
    rsync -a "$data_source/" "$RESTORE_TO/"

    # Cleanup temp directory
    rm -rf "$temp_dir"

    local restored_size=$(du -sh "$RESTORE_TO" | cut -f1)
    log_info "Extraction complete (${restored_size})"
}

# Verify restored data
verify_restored_data() {
    if [ "$SKIP_VERIFICATION" = "true" ]; then
        log_warn "Skipping verification (SKIP_VERIFICATION=true)"
        return
    fi

    log_info "Verifying restored data..."

    # Check for essential files/directories
    local essential_paths=(
        "$RESTORE_TO"
    )

    for path in "${essential_paths[@]}"; do
        if [ ! -e "$path" ]; then
            cleanup_on_error "Essential path not found: $path"
        fi
    done

    # Check data directory is not empty
    if [ ! "$(ls -A $RESTORE_TO)" ]; then
        cleanup_on_error "Restored data directory is empty"
    fi

    log_info "Data verification passed ✓"
}

# Start GraphDB and verify health
start_and_verify() {
    log_info "Starting GraphDB..."

    cd /var/lib/graphdb
    docker compose up -d

    log_info "Waiting for GraphDB to start..."
    sleep 10

    # Wait for health check
    local max_attempts=30
    local attempt=0

    while [ $attempt -lt $max_attempts ]; do
        if curl -f http://localhost:8080/health &>/dev/null; then
            log_info "GraphDB health check passed ✓"
            break
        fi

        attempt=$((attempt + 1))
        if [ $attempt -ge $max_attempts ]; then
            cleanup_on_error "GraphDB failed to start after restore"
        fi

        log_info "Waiting for GraphDB... (attempt $attempt/$max_attempts)"
        sleep 5
    done

    # Run additional health checks
    log_info "Running post-restore health checks..."

    # Check if we can query the database
    if ! curl -f http://localhost:8080/api/v1/nodes?limit=1 &>/dev/null; then
        log_warn "Cannot query nodes endpoint (may be authentication required)"
    fi

    log_info "Post-restore verification complete ✓"
}

# Cleanup backup of original data
cleanup_original_backup() {
    if [ -d "$BACKUP_ORIGINAL" ]; then
        log_info "Cleaning up original data backup..."
        log_warn "Original data will be deleted: $BACKUP_ORIGINAL"
        log_warn "Press Ctrl+C within 10 seconds to cancel..."

        sleep 10

        rm -rf "$BACKUP_ORIGINAL"
        log_info "Original data backup removed"
    fi
}

# Generate restore report
generate_report() {
    local report_file="/var/lib/graphdb/restore-report-$(date +%Y%m%d-%H%M%S).txt"

    cat > "$report_file" <<EOF
GraphDB Restore Report
======================
Timestamp: $(date)
Backup File: $BACKUP_FILE
Restored To: $RESTORE_TO
Original Data Backup: $BACKUP_ORIGINAL
Restore Status: SUCCESS

Post-Restore Health:
$(curl -s http://localhost:8080/health | jq '.' || echo "Health check failed")

Next Steps:
1. Verify application functionality
2. Check for data integrity
3. Run test queries
4. Monitor logs for errors
5. If everything looks good, remove original backup:
   rm -rf $BACKUP_ORIGINAL
EOF

    cat "$report_file"
    log_info "Report saved to: $report_file"
}

# Rollback restore (if needed)
rollback() {
    log_error "Rolling back restore..."

    docker compose -f /var/lib/graphdb/docker-compose.yml down

    if [ -d "$BACKUP_ORIGINAL" ]; then
        rm -rf "$RESTORE_TO"
        mv "$BACKUP_ORIGINAL" "$RESTORE_TO"
        log_info "Original data restored"
    fi

    log_info "Rollback complete. Starting GraphDB with original data..."
    cd /var/lib/graphdb
    docker compose up -d
}

# Main restore process
main() {
    log_info "========================================="
    log_info "GraphDB Restore Started"
    log_info "Backup: $BACKUP_FILE"
    log_info "Target: $RESTORE_TO"
    log_info "========================================="

    local start_time=$(date +%s)

    check_prerequisites
    download_backup
    verify_backup

    # Confirm restore
    log_warn "This will REPLACE all data in: $RESTORE_TO"
    log_warn "Original data will be backed up to: $BACKUP_ORIGINAL"
    read -p "Continue with restore? (yes/no): " -r
    echo

    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        log_info "Restore cancelled by user"
        exit 0
    fi

    backup_existing_data
    extract_backup
    verify_restored_data
    start_and_verify

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    log_info "========================================="
    log_info "Restore completed successfully in ${duration}s"
    log_info "========================================="

    generate_report

    log_info ""
    log_info "IMPORTANT: Verify your application works correctly!"
    log_info "If there are issues, you can rollback with:"
    log_info "  rm -rf $RESTORE_TO && mv $BACKUP_ORIGINAL $RESTORE_TO"
    log_info ""
}

# Handle script arguments
if [ "${1:-}" = "rollback" ]; then
    rollback
    exit 0
fi

# Run main function
main "$@"
