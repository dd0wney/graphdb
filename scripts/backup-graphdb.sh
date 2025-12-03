#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Automated Backup Script
#
# Features:
# - Full database backup (data + WAL)
# - Incremental backups (WAL only)
# - Compression (gzip)
# - Upload to DigitalOcean Spaces (S3-compatible)
# - Retention policy (configurable)
# - Backup verification
# - Notifications (optional)
#
# Requirements:
# - s3cmd configured with DO Spaces credentials
# - GraphDB running in Docker
#
# Environment Variables:
#   BACKUP_TYPE - full or incremental (default: incremental)
#   RETENTION_DAYS - How long to keep backups (default: 30)
#   DO_SPACES_BUCKET - S3 bucket name (e.g., graphdb-backups)
#   DO_SPACES_REGION - DO Spaces region (e.g., nyc3)
#   GRAPHDB_DATA_DIR - GraphDB data directory (default: /var/lib/graphdb/data)
#   NOTIFICATION_WEBHOOK - Optional webhook for notifications
#
# Usage:
#   # Full backup
#   BACKUP_TYPE=full ./backup-graphdb.sh
#
#   # Incremental backup (default)
#   ./backup-graphdb.sh
#
# Cron Setup (daily full + hourly incremental):
#   0 2 * * * BACKUP_TYPE=full /path/to/backup-graphdb.sh
#   0 * * * * BACKUP_TYPE=incremental /path/to/backup-graphdb.sh
#######################################################################

# Configuration
BACKUP_TYPE="${BACKUP_TYPE:-incremental}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
GRAPHDB_DATA_DIR="${GRAPHDB_DATA_DIR:-/var/lib/graphdb/data}"
BACKUP_DIR="${BACKUP_DIR:-/var/lib/graphdb/backups}"
DO_SPACES_BUCKET="${DO_SPACES_BUCKET:-}"
DO_SPACES_REGION="${DO_SPACES_REGION:-nyc3}"
NOTIFICATION_WEBHOOK="${NOTIFICATION_WEBHOOK:-}"

TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_NAME="graphdb-${BACKUP_TYPE}-${TIMESTAMP}"
BACKUP_PATH="${BACKUP_DIR}/${BACKUP_NAME}"

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

# Send notification (optional)
send_notification() {
    local status=$1
    local message=$2

    if [ -n "$NOTIFICATION_WEBHOOK" ]; then
        curl -X POST "$NOTIFICATION_WEBHOOK" \
            -H "Content-Type: application/json" \
            -d "{\"status\":\"$status\",\"message\":\"$message\",\"timestamp\":\"$(date -Iseconds)\"}" \
            2>/dev/null || true
    fi
}

# Cleanup on error
cleanup_on_error() {
    log_error "Backup failed, cleaning up..."
    rm -rf "$BACKUP_PATH" "$BACKUP_PATH.tar.gz" 2>/dev/null || true
    send_notification "error" "GraphDB backup failed: $1"
    exit 1
}

trap 'cleanup_on_error "Unexpected error"' ERR

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v docker &> /dev/null; then
        log_error "Docker not found"
        exit 1
    fi

    if [ -n "$DO_SPACES_BUCKET" ] && ! command -v s3cmd &> /dev/null; then
        log_error "s3cmd not found but DO_SPACES_BUCKET is set. Install: apt-get install s3cmd"
        exit 1
    fi

    if [ ! -d "$GRAPHDB_DATA_DIR" ]; then
        log_error "GraphDB data directory not found: $GRAPHDB_DATA_DIR"
        exit 1
    fi

    # Create backup directory
    mkdir -p "$BACKUP_DIR"
    mkdir -p "$BACKUP_PATH"

    log_info "Prerequisites check passed"
}

# Stop writes (optional - for consistent backup)
# For production, use WAL-based backup instead
prepare_backup() {
    log_info "Preparing backup (type: $BACKUP_TYPE)..."

    # Create backup manifest
    cat > "$BACKUP_PATH/manifest.json" <<EOF
{
  "backup_type": "$BACKUP_TYPE",
  "timestamp": "$(date -Iseconds)",
  "hostname": "$(hostname)",
  "graphdb_version": "$(docker exec graphdb cat /app/VERSION 2>/dev/null || echo 'unknown')",
  "data_size_bytes": $(du -sb "$GRAPHDB_DATA_DIR" | cut -f1)
}
EOF

    log_info "Backup prepared"
}

# Perform full backup
backup_full() {
    log_info "Starting full backup..."

    # Copy entire data directory
    log_info "Copying data directory ($(du -sh "$GRAPHDB_DATA_DIR" | cut -f1))..."
    rsync -a --info=progress2 "$GRAPHDB_DATA_DIR/" "$BACKUP_PATH/data/"

    log_info "Full backup completed"
}

# Perform incremental backup (WAL files only)
backup_incremental() {
    log_info "Starting incremental backup..."

    # Find WAL files modified in the last 2 hours
    local wal_dir="$GRAPHDB_DATA_DIR/wal"

    if [ ! -d "$wal_dir" ]; then
        log_warn "WAL directory not found, falling back to full backup"
        backup_full
        return
    fi

    # Copy recent WAL files
    find "$wal_dir" -type f -mmin -120 -exec cp -p {} "$BACKUP_PATH/data/" \;

    local file_count=$(find "$BACKUP_PATH/data/" -type f | wc -l)
    log_info "Incremental backup completed ($file_count WAL files)"

    if [ "$file_count" -eq 0 ]; then
        log_warn "No WAL files found in last 2 hours, skipping backup"
        rm -rf "$BACKUP_PATH"
        exit 0
    fi
}

# Compress backup
compress_backup() {
    log_info "Compressing backup..."

    cd "$BACKUP_DIR"
    tar -czf "${BACKUP_NAME}.tar.gz" "$BACKUP_NAME/"

    local original_size=$(du -sh "$BACKUP_PATH" | cut -f1)
    local compressed_size=$(du -sh "${BACKUP_NAME}.tar.gz" | cut -f1)

    log_info "Compression complete: $original_size → $compressed_size"

    # Remove uncompressed backup
    rm -rf "$BACKUP_PATH"
}

# Upload to DigitalOcean Spaces
upload_to_spaces() {
    if [ -z "$DO_SPACES_BUCKET" ]; then
        log_info "DO_SPACES_BUCKET not set, skipping upload"
        return
    fi

    log_info "Uploading to DigitalOcean Spaces..."

    local backup_file="${BACKUP_DIR}/${BACKUP_NAME}.tar.gz"
    local s3_path="s3://${DO_SPACES_BUCKET}/${BACKUP_TYPE}/${BACKUP_NAME}.tar.gz"

    s3cmd put "$backup_file" "$s3_path" \
        --host="${DO_SPACES_REGION}.digitaloceanspaces.com" \
        --host-bucket="%(bucket)s.${DO_SPACES_REGION}.digitaloceanspaces.com"

    log_info "Upload complete: $s3_path"

    # Verify upload
    local remote_size=$(s3cmd ls "$s3_path" --host="${DO_SPACES_REGION}.digitaloceanspaces.com" | awk '{print $3}')
    local local_size=$(stat -f%z "$backup_file" 2>/dev/null || stat -c%s "$backup_file")

    if [ "$remote_size" != "$local_size" ]; then
        log_error "Upload verification failed: size mismatch"
        exit 1
    fi

    log_info "Upload verified ✓"
}

# Clean up old backups (retention policy)
cleanup_old_backups() {
    log_info "Cleaning up backups older than $RETENTION_DAYS days..."

    # Local cleanup
    find "$BACKUP_DIR" -name "graphdb-*.tar.gz" -mtime +$RETENTION_DAYS -delete

    local deleted_count=$(find "$BACKUP_DIR" -name "graphdb-*.tar.gz" -mtime +$RETENTION_DAYS | wc -l)
    log_info "Deleted $deleted_count local backups"

    # Spaces cleanup (if configured)
    if [ -n "$DO_SPACES_BUCKET" ]; then
        log_info "Cleaning up remote backups..."

        # Calculate cutoff date
        local cutoff_date=$(date -d "$RETENTION_DAYS days ago" +%Y%m%d 2>/dev/null || date -v-${RETENTION_DAYS}d +%Y%m%d)

        # List and delete old backups
        s3cmd ls "s3://${DO_SPACES_BUCKET}/${BACKUP_TYPE}/" \
            --host="${DO_SPACES_REGION}.digitaloceanspaces.com" \
            | awk '{print $4}' \
            | while read -r file; do
                # Extract date from filename
                local file_date=$(echo "$file" | grep -oP '\d{8}' | head -1)

                if [ "$file_date" -lt "$cutoff_date" ]; then
                    log_info "Deleting old backup: $file"
                    s3cmd del "$file" --host="${DO_SPACES_REGION}.digitaloceanspaces.com" || true
                fi
            done
    fi

    log_info "Cleanup complete"
}

# Verify backup integrity
verify_backup() {
    log_info "Verifying backup integrity..."

    local backup_file="${BACKUP_DIR}/${BACKUP_NAME}.tar.gz"

    # Check if file exists and is not empty
    if [ ! -s "$backup_file" ]; then
        log_error "Backup file is empty or missing"
        exit 1
    fi

    # Test archive integrity
    if ! tar -tzf "$backup_file" > /dev/null; then
        log_error "Backup archive is corrupted"
        exit 1
    fi

    log_info "Backup verification passed ✓"
}

# Generate backup report
generate_report() {
    local backup_file="${BACKUP_DIR}/${BACKUP_NAME}.tar.gz"
    local backup_size=$(du -sh "$backup_file" | cut -f1)
    local data_size=$(du -sh "$GRAPHDB_DATA_DIR" | cut -f1)

    cat > "${BACKUP_DIR}/${BACKUP_NAME}.report.txt" <<EOF
GraphDB Backup Report
=====================
Backup Type: $BACKUP_TYPE
Timestamp: $(date)
Backup File: ${BACKUP_NAME}.tar.gz
Backup Size: $backup_size
Data Size: $data_size
Retention: $RETENTION_DAYS days
Uploaded to Spaces: $([ -n "$DO_SPACES_BUCKET" ] && echo "Yes (${DO_SPACES_BUCKET})" || echo "No")
Status: SUCCESS
EOF

    cat "${BACKUP_DIR}/${BACKUP_NAME}.report.txt"
}

# Main backup process
main() {
    log_info "========================================="
    log_info "GraphDB Backup Started"
    log_info "Type: $BACKUP_TYPE"
    log_info "========================================="

    local start_time=$(date +%s)

    check_prerequisites
    prepare_backup

    # Perform backup based on type
    if [ "$BACKUP_TYPE" = "full" ]; then
        backup_full
    else
        backup_incremental
    fi

    compress_backup
    verify_backup
    upload_to_spaces
    cleanup_old_backups
    generate_report

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    log_info "========================================="
    log_info "Backup completed successfully in ${duration}s"
    log_info "Backup: ${BACKUP_NAME}.tar.gz"
    log_info "========================================="

    send_notification "success" "GraphDB backup completed: ${BACKUP_NAME}.tar.gz (${duration}s)"
}

# Run main function
main "$@"
