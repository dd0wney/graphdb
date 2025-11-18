#!/bin/bash
# GraphDB Backup Script
# Backs up PostgreSQL database and GraphDB data volumes

set -e

# Configuration
BACKUP_DIR="${BACKUP_DIR:-./backups}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"

# Create backup directory
mkdir -p "$BACKUP_DIR"

echo "========================================="
echo "GraphDB Backup Started: $TIMESTAMP"
echo "========================================="

# Backup PostgreSQL
echo "Backing up PostgreSQL database..."
POSTGRES_BACKUP="$BACKUP_DIR/postgres_$TIMESTAMP.sql"
docker-compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U graphdb graphdb_licenses > "$POSTGRES_BACKUP"
gzip "$POSTGRES_BACKUP"
echo "✓ PostgreSQL backup: $POSTGRES_BACKUP.gz"

# Backup GraphDB Community data volume
if docker volume ls | grep -q graphdb_community_data; then
  echo "Backing up GraphDB Community data..."
  docker run --rm \
    -v graphdb_community_data:/data \
    -v $(pwd)/$BACKUP_DIR:/backup \
    alpine tar czf /backup/graphdb-community-data_$TIMESTAMP.tar.gz /data
  echo "✓ Community data backup: $BACKUP_DIR/graphdb-community-data_$TIMESTAMP.tar.gz"
fi

# Backup GraphDB Enterprise data volume
if docker volume ls | grep -q graphdb_enterprise_data; then
  echo "Backing up GraphDB Enterprise data..."
  docker run --rm \
    -v graphdb_enterprise_data:/data \
    -v $(pwd)/$BACKUP_DIR:/backup \
    alpine tar czf /backup/graphdb-enterprise-data_$TIMESTAMP.tar.gz /data
  echo "✓ Enterprise data backup: $BACKUP_DIR/graphdb-enterprise-data_$TIMESTAMP.tar.gz"
fi

# Clean up old backups
echo "Cleaning up backups older than $RETENTION_DAYS days..."
find "$BACKUP_DIR" -name "*.sql.gz" -mtime +$RETENTION_DAYS -delete
find "$BACKUP_DIR" -name "*.tar.gz" -mtime +$RETENTION_DAYS -delete
echo "✓ Old backups removed"

# Calculate backup sizes
echo ""
echo "Backup Summary:"
echo "----------------------------------------"
ls -lh "$BACKUP_DIR"/*_$TIMESTAMP.* | awk '{print $9, "(" $5 ")"}'
echo "----------------------------------------"
echo "Total backups:"
du -sh "$BACKUP_DIR" | awk '{print $1}'

echo ""
echo "✓ Backup completed successfully!"
echo "Timestamp: $TIMESTAMP"
