#!/bin/bash
set -euo pipefail

#######################################################################
# GraphDB Weekly Backup Script
#
# Creates weekly volume snapshots with automatic retention management.
# Keeps last 4 weekly snapshots, deletes older ones.
#
# Usage:
#   ./weekly-backup.sh
#
# Setup (run once):
#   1. Install in cron for weekly execution:
#      sudo cp weekly-backup.sh /usr/local/bin/
#      sudo chmod +x /usr/local/bin/weekly-backup.sh
#
#   2. Add to crontab (runs every Sunday at 2 AM):
#      sudo crontab -e
#      0 2 * * 0 /usr/local/bin/weekly-backup.sh >> /var/log/graphdb-backup.log 2>&1
#
# Requirements:
#   - doctl CLI installed and authenticated
#   - DIGITALOCEAN_API_TOKEN environment variable (for cron)
#
#######################################################################

# Configuration
VOLUME_NAME="graphdb-data"
RETENTION_WEEKS=4
SNAPSHOT_PREFIX="graphdb-weekly"
LOG_FILE="/var/log/graphdb-backup.log"

# Ensure doctl is available
if ! command -v doctl &> /dev/null; then
    echo "ERROR: doctl CLI not found. Install from: https://github.com/digitalocean/doctl"
    exit 1
fi

# Ensure API token is set (for cron jobs)
if [ -z "${DIGITALOCEAN_API_TOKEN:-}" ]; then
    echo "WARNING: DIGITALOCEAN_API_TOKEN not set. Using doctl default auth."
fi

#######################################################################
# Functions
#######################################################################

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

get_volume_id() {
    local volume_name=$1
    doctl compute volume list --format ID,Name --no-header | \
        grep "$volume_name" | awk '{print $1}' | head -1
}

create_snapshot() {
    local volume_id=$1
    local snapshot_name="${SNAPSHOT_PREFIX}-$(date +'%Y%m%d-%H%M%S')"

    log "Creating snapshot: $snapshot_name"

    doctl compute volume-snapshot create "$volume_id" \
        --snapshot-name "$snapshot_name" \
        --desc "Automated weekly backup created on $(date +'%Y-%m-%d')"

    if [ $? -eq 0 ]; then
        log "✓ Snapshot created successfully: $snapshot_name"
        return 0
    else
        log "✗ Failed to create snapshot"
        return 1
    fi
}

cleanup_old_snapshots() {
    log "Cleaning up old snapshots (keeping last $RETENTION_WEEKS weeks)..."

    # Get all snapshots for this volume, sorted by creation date (oldest first)
    local snapshots=$(doctl compute volume-snapshot list \
        --format ID,Name,CreatedAt \
        --no-header | \
        grep "$SNAPSHOT_PREFIX" | \
        sort -k3)

    local total_snapshots=$(echo "$snapshots" | wc -l)
    local snapshots_to_delete=$((total_snapshots - RETENTION_WEEKS))

    if [ "$snapshots_to_delete" -le 0 ]; then
        log "✓ Only $total_snapshots snapshots exist. No cleanup needed."
        return 0
    fi

    log "Found $total_snapshots snapshots. Deleting oldest $snapshots_to_delete..."

    # Delete oldest snapshots
    echo "$snapshots" | head -n "$snapshots_to_delete" | while read -r line; do
        local snapshot_id=$(echo "$line" | awk '{print $1}')
        local snapshot_name=$(echo "$line" | awk '{print $2}')

        log "Deleting old snapshot: $snapshot_name (ID: $snapshot_id)"

        doctl compute volume-snapshot delete "$snapshot_id" --force

        if [ $? -eq 0 ]; then
            log "✓ Deleted: $snapshot_name"
        else
            log "✗ Failed to delete: $snapshot_name"
        fi
    done
}

get_snapshot_stats() {
    log "Current snapshot inventory:"

    doctl compute volume-snapshot list \
        --format Name,Size,CreatedAt \
        --no-header | \
        grep "$SNAPSHOT_PREFIX" | \
        sort -k3 -r | \
        while read -r line; do
            log "  $line"
        done

    # Calculate total storage used
    local total_gb=$(doctl compute volume-snapshot list \
        --format Size \
        --no-header | \
        grep -v "^$" | \
        awk '{sum += $1} END {print sum}')

    local total_cost=$(echo "$total_gb * 0.05" | bc)

    log "Total snapshot storage: ${total_gb}GB (~\$${total_cost}/month)"
}

send_notification() {
    local status=$1
    local message=$2

    # Optional: Send notification via webhook, email, etc.
    # Uncomment and configure for your notification service

    # Example: Send to Slack webhook
    # if [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
    #     curl -X POST "$SLACK_WEBHOOK_URL" \
    #         -H 'Content-Type: application/json' \
    #         -d "{\"text\": \"GraphDB Backup $status: $message\"}"
    # fi

    # Example: Send email via sendmail
    # if command -v sendmail &> /dev/null; then
    #     echo "Subject: GraphDB Backup $status" | \
    #         sendmail -v admin@yourdomain.com <<< "$message"
    # fi

    log "Notification: $status - $message"
}

#######################################################################
# Main Execution
#######################################################################

main() {
    log "=========================================="
    log "GraphDB Weekly Backup Starting"
    log "=========================================="

    # Step 1: Get volume ID
    log "Looking up volume: $VOLUME_NAME"
    VOLUME_ID=$(get_volume_id "$VOLUME_NAME")

    if [ -z "$VOLUME_ID" ]; then
        log "ERROR: Volume '$VOLUME_NAME' not found"
        send_notification "FAILED" "Volume not found: $VOLUME_NAME"
        exit 1
    fi

    log "✓ Found volume: $VOLUME_NAME (ID: $VOLUME_ID)"

    # Step 2: Create snapshot
    if create_snapshot "$VOLUME_ID"; then
        log "✓ Snapshot created successfully"
    else
        log "✗ Snapshot creation failed"
        send_notification "FAILED" "Snapshot creation failed for $VOLUME_NAME"
        exit 1
    fi

    # Step 3: Cleanup old snapshots
    cleanup_old_snapshots

    # Step 4: Show current stats
    get_snapshot_stats

    # Step 5: Send success notification
    send_notification "SUCCESS" "Weekly backup completed for $VOLUME_NAME"

    log "=========================================="
    log "GraphDB Weekly Backup Complete"
    log "=========================================="
    log ""
}

# Run main function
main

exit 0
