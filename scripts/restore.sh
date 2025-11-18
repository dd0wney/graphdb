#!/bin/bash
# GraphDB Restore Script
# Restores PostgreSQL database and GraphDB data volumes from backups

set -e

# Configuration
BACKUP_DIR="${BACKUP_DIR:-./backups}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "========================================="
echo "GraphDB Restore Utility"
echo "========================================="

# Check if backup directory exists
if [ ! -d "$BACKUP_DIR" ]; then
  echo -e "${RED}Error: Backup directory not found: $BACKUP_DIR${NC}"
  exit 1
fi

# List available backups
echo ""
echo "Available PostgreSQL backups:"
echo "----------------------------------------"
ls -1t "$BACKUP_DIR"/postgres_*.sql.gz 2>/dev/null || echo "No PostgreSQL backups found"

echo ""
echo "Available GraphDB data backups:"
echo "----------------------------------------"
ls -1t "$BACKUP_DIR"/graphdb-*-data_*.tar.gz 2>/dev/null || echo "No data backups found"

echo ""
echo -e "${YELLOW}WARNING: This will overwrite existing data!${NC}"
echo -e "${YELLOW}Make sure to stop services before restoring.${NC}"
echo ""

# Confirm restore
read -p "Do you want to continue? (yes/no): " CONFIRM
if [ "$CONFIRM" != "yes" ]; then
  echo "Restore cancelled."
  exit 0
fi

# PostgreSQL restore
echo ""
read -p "PostgreSQL backup file (or 'skip'): " POSTGRES_FILE
if [ "$POSTGRES_FILE" != "skip" ]; then
  if [ ! -f "$BACKUP_DIR/$POSTGRES_FILE" ] && [ ! -f "$POSTGRES_FILE" ]; then
    echo -e "${RED}Error: File not found${NC}"
    exit 1
  fi
  
  POSTGRES_PATH="${BACKUP_DIR}/${POSTGRES_FILE}"
  [ -f "$POSTGRES_FILE" ] && POSTGRES_PATH="$POSTGRES_FILE"
  
  echo "Restoring PostgreSQL from $POSTGRES_PATH..."
  gunzip < "$POSTGRES_PATH" | \
    docker-compose -f "$COMPOSE_FILE" exec -T postgres \
      psql -U graphdb graphdb_licenses
  echo -e "${GREEN}✓ PostgreSQL restored successfully${NC}"
fi

# GraphDB Community data restore
echo ""
read -p "GraphDB Community data backup file (or 'skip'): " COMMUNITY_FILE
if [ "$COMMUNITY_FILE" != "skip" ]; then
  if [ ! -f "$BACKUP_DIR/$COMMUNITY_FILE" ] && [ ! -f "$COMMUNITY_FILE" ]; then
    echo -e "${RED}Error: File not found${NC}"
    exit 1
  fi
  
  COMMUNITY_PATH="${BACKUP_DIR}/${COMMUNITY_FILE}"
  [ -f "$COMMUNITY_FILE" ] && COMMUNITY_PATH="$COMMUNITY_FILE"
  
  echo "Restoring Community data from $COMMUNITY_PATH..."
  docker run --rm \
    -v graphdb_community_data:/data \
    -v $(pwd)/${COMMUNITY_PATH}:/backup.tar.gz \
    alpine sh -c "cd / && tar xzf /backup.tar.gz"
  echo -e "${GREEN}✓ Community data restored successfully${NC}"
fi

# GraphDB Enterprise data restore
echo ""
read -p "GraphDB Enterprise data backup file (or 'skip'): " ENTERPRISE_FILE
if [ "$ENTERPRISE_FILE" != "skip" ]; then
  if [ ! -f "$BACKUP_DIR/$ENTERPRISE_FILE" ] && [ ! -f "$ENTERPRISE_FILE" ]; then
    echo -e "${RED}Error: File not found${NC}"
    exit 1
  fi
  
  ENTERPRISE_PATH="${BACKUP_DIR}/${ENTERPRISE_FILE}"
  [ -f "$ENTERPRISE_FILE" ] && ENTERPRISE_PATH="$ENTERPRISE_FILE"
  
  echo "Restoring Enterprise data from $ENTERPRISE_PATH..."
  docker run --rm \
    -v graphdb_enterprise_data:/data \
    -v $(pwd)/${ENTERPRISE_PATH}:/backup.tar.gz \
    alpine sh -c "cd / && tar xzf /backup.tar.gz"
  echo -e "${GREEN}✓ Enterprise data restored successfully${NC}"
fi

echo ""
echo -e "${GREEN}=========================================${NC}"
echo -e "${GREEN}Restore completed successfully!${NC}"
echo -e "${GREEN}=========================================${NC}"
echo ""
echo "Next steps:"
echo "1. Start services: docker-compose -f $COMPOSE_FILE up -d"
echo "2. Verify health: curl http://localhost:8080/health"
echo "3. Check logs: docker-compose -f $COMPOSE_FILE logs -f"
