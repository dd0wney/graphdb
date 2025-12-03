# GraphDB Disaster Recovery Runbook

Complete disaster recovery strategy for GraphDB on Digital Ocean.

---

## Table of Contents

1. [Overview](#overview)
2. [DR Strategy](#dr-strategy)
3. [Backup Solution](#backup-solution)
4. [Restore Procedures](#restore-procedures)
5. [High Availability](#high-availability)
6. [Disaster Scenarios](#disaster-scenarios)
7. [Testing & Drills](#testing--drills)
8. [Costs](#costs)

---

## Overview

### Recovery Objectives

| Metric | Target | Notes |
|--------|--------|-------|
| **RPO** (Recovery Point Objective) | 1 hour | Maximum data loss acceptable |
| **RTO** (Recovery Time Objective) | 15 minutes | Maximum downtime acceptable |
| **Data Retention** | 30 days | Backup retention period |
| **Geographic Redundancy** | Multi-region | Backups in different DO regions |

### DR Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Primary Region (NYC3)                     │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │   GraphDB    │───▶│ DO Snapshots │───▶│  DO Spaces   │  │
│  │   Droplet    │    │   (Daily)    │    │  (Off-site)  │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
│         │                                         │          │
│         │                                         │          │
│         ▼                                         ▼          │
│  ┌──────────────┐                        ┌──────────────┐  │
│  │ Volume Block │                        │ DO Spaces    │  │
│  │   Storage    │                        │  Replication │  │
│  │  (Attached)  │                        │  (SFO3)      │  │
│  └──────────────┘                        └──────────────┘  │
└─────────────────────────────────────────────────────────────┘
                               │
                               ▼
                  ┌────────────────────────┐
                  │  Secondary Region      │
                  │  (SFO3 - Standby)      │
                  └────────────────────────┘
```

---

## DR Strategy

### Multi-Layered Backup Approach

We use **4 layers** of redundancy:

#### Layer 1: Application-Level Backups ✅ IMPLEMENTED
- **What**: GraphDB native backups (data + WAL files)
- **Frequency**: Hourly incremental, Daily full
- **Location**: `/var/lib/graphdb/backups`
- **Retention**: 7 days local
- **Script**: `scripts/backup-graphdb.sh`

#### Layer 2: DigitalOcean Snapshots
- **What**: Complete droplet image snapshots
- **Frequency**: Daily (automated)
- **Location**: DO Snapshots service
- **Retention**: 30 days
- **RTO**: 5-10 minutes
- **Cost**: $0.06/GB/month

#### Layer 3: DO Spaces (S3-Compatible)
- **What**: Off-site backup storage
- **Frequency**: Hourly sync from Layer 1
- **Location**: DO Spaces (primary + replicated region)
- **Retention**: 30 days (with versioning)
- **RTO**: 15 minutes
- **Cost**: $5/month for 250GB + $0.02/GB

#### Layer 4: Volume Block Storage
- **What**: Persistent data volumes (attached storage)
- **Frequency**: Continuous (data persists beyond droplet)
- **Location**: DO Volumes (can be detached/reattached)
- **Retention**: Permanent until deleted
- **RTO**: 2-5 minutes (volume reattach)
- **Cost**: $0.10/GB/month

### Why This Approach?

| Disaster Scenario | Recovery Method | RTO |
|-------------------|-----------------|-----|
| Accidental data deletion | Layer 1: Application backup | 5 min |
| Database corruption | Layer 1 or 2: Restore from backup/snapshot | 10 min |
| Droplet failure | Layer 2 or 4: Snapshot or volume reattach | 10 min |
| Region outage | Layer 3: Restore from Spaces in different region | 15 min |
| Complete infrastructure loss | Layer 3: Full rebuild from Spaces | 20 min |

---

## Backup Solution

### Automated Backup Setup

**1. Application-Level Backups (scripts/backup-graphdb.sh)**

```bash
# Install on droplet
ssh root@YOUR_DROPLET_IP 'bash -s' <<'EOF'
# Download backup script
curl -o /usr/local/bin/backup-graphdb.sh \
  https://raw.githubusercontent.com/yourusername/graphdb/main/scripts/backup-graphdb.sh

chmod +x /usr/local/bin/backup-graphdb.sh

# Set up cron jobs
cat > /etc/cron.d/graphdb-backup <<'CRON'
# Full backup daily at 2 AM
0 2 * * * root BACKUP_TYPE=full /usr/local/bin/backup-graphdb.sh >> /var/log/graphdb-backup.log 2>&1

# Incremental backup every hour
0 * * * * root BACKUP_TYPE=incremental /usr/local/bin/backup-graphdb.sh >> /var/log/graphdb-backup.log 2>&1
CRON

# Test backup immediately
BACKUP_TYPE=full /usr/local/bin/backup-graphdb.sh
EOF
```

**2. Configure DO Spaces for Off-Site Backups**

```bash
# On your droplet, install s3cmd
apt-get update && apt-get install -y s3cmd

# Configure s3cmd for DO Spaces
cat > ~/.s3cfg <<EOF
[default]
access_key = YOUR_SPACES_ACCESS_KEY
secret_key = YOUR_SPACES_SECRET_KEY
host_base = nyc3.digitaloceanspaces.com
host_bucket = %(bucket)s.nyc3.digitaloceanspaces.com
use_https = True
EOF

# Create Spaces bucket (do this once)
s3cmd mb s3://graphdb-backups --host=nyc3.digitaloceanspaces.com

# Enable in backup script
export DO_SPACES_BUCKET="graphdb-backups"
export DO_SPACES_REGION="nyc3"
```

**3. Enable DO Droplet Snapshots**

```bash
# Using doctl CLI
doctl compute droplet-action snapshot YOUR_DROPLET_ID --snapshot-name "graphdb-snapshot-$(date +%Y%m%d)"

# Or via DO web interface:
# Droplets → Your Droplet → Snapshots → Take Snapshot
```

**4. Attach DO Volume for Data Persistence**

```bash
# Create volume (100GB)
doctl compute volume create graphdb-data \
  --region nyc3 \
  --size 100GiB \
  --desc "GraphDB persistent data volume"

# Attach to droplet
doctl compute volume-action attach VOLUME_ID DROPLET_ID

# Mount on droplet
ssh root@YOUR_DROPLET_IP 'bash -s' <<'EOF'
# Format (first time only)
mkfs.ext4 /dev/disk/by-id/scsi-0DO_Volume_graphdb-data

# Mount
mkdir -p /mnt/graphdb-volume
mount -o discard,defaults /dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-volume

# Add to fstab for auto-mount
echo '/dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-volume ext4 defaults,nofail,discard 0 0' >> /etc/fstab

# Move GraphDB data to volume
systemctl stop graphdb
rsync -av /var/lib/graphdb/data/ /mnt/graphdb-volume/
rm -rf /var/lib/graphdb/data
ln -s /mnt/graphdb-volume /var/lib/graphdb/data
systemctl start graphdb
EOF
```

---

## Restore Procedures

### Scenario 1: Accidental Data Deletion (RPO: <1 hour, RTO: 5 min)

**Symptoms**: Specific graph data deleted, application reports missing nodes/edges

**Solution**: Application-level restore from most recent backup

```bash
# 1. List available backups
ssh root@YOUR_DROPLET_IP 'ls -lh /var/lib/graphdb/backups/'

# 2. Stop GraphDB
ssh root@YOUR_DROPLET_IP 'cd /var/lib/graphdb && docker compose down'

# 3. Restore from backup
ssh root@YOUR_DROPLET_IP 'bash -s' <<'EOF'
export BACKUP_FILE="/var/lib/graphdb/backups/graphdb-full-YYYYMMDD-HHMMSS.tar.gz"
curl -o /tmp/restore.sh https://raw.githubusercontent.com/yourusername/graphdb/main/scripts/restore-graphdb.sh
chmod +x /tmp/restore.sh
/tmp/restore.sh
EOF

# Estimated time: 5 minutes
```

### Scenario 2: Database Corruption (RPO: 24h, RTO: 10 min)

**Symptoms**: GraphDB won't start, corrupted index errors

**Solution**: Restore from DO Snapshot

```bash
# 1. List available snapshots
doctl compute snapshot list --resource droplet

# 2. Create new droplet from snapshot
doctl compute droplet create graphdb-restored \
  --image SNAPSHOT_ID \
  --region nyc3 \
  --size s-2vcpu-4gb \
  --ssh-keys YOUR_SSH_KEY_ID \
  --wait

# 3. Get new droplet IP
NEW_IP=$(doctl compute droplet list graphdb-restored --format PublicIPv4 --no-header)

# 4. Update DNS/Cloudflare Tunnel to point to new IP
ssh root@$NEW_IP 'cd /var/lib/graphdb && docker compose up -d'

# 5. Verify health
curl https://graphdb.yourdomain.com/health

# 6. Delete old droplet
doctl compute droplet delete OLD_DROPLET_ID

# Estimated time: 10 minutes
```

### Scenario 3: Droplet Failure (RPO: 0, RTO: 5 min)

**Symptoms**: Droplet unresponsive, SSH timeout, hardware failure

**Solution**: Detach volume and attach to new droplet

```bash
# 1. Create new droplet
doctl compute droplet create graphdb-new \
  --image ubuntu-22-04-x64 \
  --region nyc3 \
  --size s-2vcpu-4gb \
  --ssh-keys YOUR_SSH_KEY_ID \
  --wait

# 2. Detach volume from failed droplet
doctl compute volume-action detach VOLUME_ID FAILED_DROPLET_ID

# 3. Attach volume to new droplet
NEW_DROPLET_ID=$(doctl compute droplet list graphdb-new --format ID --no-header)
doctl compute volume-action attach VOLUME_ID $NEW_DROPLET_ID

# 4. Mount volume and start GraphDB
NEW_IP=$(doctl compute droplet list graphdb-new --format PublicIPv4 --no-header)

ssh root@$NEW_IP 'bash -s' <<'EOF'
# Run deployment script
curl -o /tmp/setup.sh https://raw.githubusercontent.com/yourusername/graphdb/main/deployments/digitalocean/setup.sh
bash /tmp/setup.sh

# Mount volume
mkdir -p /mnt/graphdb-volume
mount -o discard,defaults /dev/disk/by-id/scsi-0DO_Volume_graphdb-data /mnt/graphdb-volume
ln -s /mnt/graphdb-volume /var/lib/graphdb/data

# Start GraphDB
cd /var/lib/graphdb && docker compose up -d
EOF

# Estimated time: 5 minutes
```

### Scenario 4: Region Outage (RPO: 1h, RTO: 15 min)

**Symptoms**: All resources in NYC3 region unavailable

**Solution**: Restore from DO Spaces in different region

```bash
# 1. Deploy new droplet in SFO3
export DO_REGION="sfo3"
export DOMAIN="graphdb-failover.yourdomain.com"
./deploy.sh

# 2. Restore from Spaces backup
ssh root@NEW_DROPLET_IP 'bash -s' <<'EOF'
export BACKUP_FILE="s3://graphdb-backups/full/graphdb-full-LATEST.tar.gz"
export DO_SPACES_REGION="nyc3"  # Original backup location
curl -o /tmp/restore.sh https://raw.githubusercontent.com/yourusername/graphdb/main/scripts/restore-graphdb.sh
chmod +x /tmp/restore.sh
/tmp/restore.sh
EOF

# 3. Update Cloudflare Tunnel to new droplet
cloudflared tunnel route dns YOUR_TUNNEL_ID graphdb.yourdomain.com

# Estimated time: 15 minutes
```

---

## High Availability

### Multi-Region Setup (Optional - Enterprise)

For mission-critical deployments, run **active-passive** or **active-active** across regions:

```
Primary (NYC3)          Secondary (SFO3)
┌──────────────┐       ┌──────────────┐
│   GraphDB    │──────▶│   GraphDB    │
│   (Active)   │ Async │  (Standby)   │
│              │ Repl  │              │
└──────────────┘       └──────────────┘
       │                      │
       ▼                      ▼
┌──────────────┐       ┌──────────────┐
│   Cloudflare │       │  Cloudflare  │
│     Tunnel   │       │    Tunnel    │
│   (Primary)  │       │  (Failover)  │
└──────────────┘       └──────────────┘
       │                      │
       └──────────┬───────────┘
                  │
                  ▼
         ┌────────────────┐
         │   DO Global    │
         │  Load Balancer │
         └────────────────┘
                  │
                  ▼
         graphdb.yourdomain.com
```

**Cost**: ~$48/month (2 droplets) + $20/month (load balancer) = $68/month

---

## Disaster Scenarios

### Runbook: Complete Data Center Failure

**Detection**:
- All monitoring alerts fail
- Cannot SSH to droplet
- Cloudflare Tunnel shows as down

**Response** (15-minute RTO):

```bash
# Minute 0-2: Assess situation
doctl compute droplet list  # Check droplet status
doctl compute region list   # Check region status

# Minute 2-5: Deploy to new region
export DO_REGION="sfo3"  # Switch region
export DOMAIN="graphdb.yourdomain.com"
./deploy.sh  # Automated deployment

# Minute 5-12: Restore from Spaces
# (Script prompts handled automatically)
ssh root@NEW_IP '/path/to/restore-graphdb.sh'

# Minute 12-15: Verify and switch traffic
curl https://graphdb.yourdomain.com/health
doctl compute domain records list yourdomain.com
# Update DNS if needed
```

---

## Testing & Drills

### Automated DR Testing

**Use the automated DR test script (recommended)**:
```bash
# Run automated DR tests (includes backup, restore, RTO measurement)
DROPLET_IP=YOUR_DROPLET_IP ./scripts/test-dr.sh

# Example output:
# ✓ Backup creation test passed (45s)
# ✓ Backup verification test passed (3s)
# ✓ Restore procedure test passed (38s)
# ✓ Data integrity test passed (12s)
# ✓ RTO test passed: 52s (target: <900s / 15min)
# ⊘ DO Spaces integration test skipped (not configured)
#
# Summary: 5 passed, 0 failed, 0 warnings, 1 skipped
```

The automated test script (`scripts/test-dr.sh`) performs:
1. **Backup Creation Test** - Triggers full backup and verifies completion
2. **Backup Verification** - Validates backup file integrity
3. **Restore Procedure Test** - Restores to test location and verifies
4. **Data Integrity Test** - Confirms database health after restore
5. **RTO Measurement** - Measures actual recovery time vs 15min target
6. **DO Spaces Integration** - Verifies off-site backup connectivity

**Schedule**: Run automatically monthly via cron:
```bash
# Add to crontab on your local machine
0 3 1 * * DROPLET_IP=YOUR_DROPLET_IP /path/to/scripts/test-dr.sh >> /var/log/graphdb-dr-test.log 2>&1
```

### Manual DR Tests (Optional)

**Test 1: Backup Restoration** (every month)
```bash
# 1. Create test droplet
# 2. Restore latest backup
# 3. Verify data integrity
# 4. Delete test droplet
```

**Test 2: Failover Drill** (quarterly)
```bash
# 1. Simulate primary region failure
# 2. Execute failover to secondary region
# 3. Verify application functionality
# 4. Fail back to primary
```

**Test 3: Volume Detach/Reattach** (monthly)
```bash
# 1. Detach volume from running droplet
# 2. Attach to new droplet
# 3. Verify data persistence
# 4. Document time taken
```

---

## Costs

### DR Infrastructure Costs (Monthly)

| Component | Size | Cost/Month | Notes |
|-----------|------|------------|-------|
| **Primary Droplet** | 2vCPU/4GB | $24 | Production instance |
| **Volume Storage** | 100GB | $10 | Persistent data |
| **DO Snapshots** | ~100GB | $6 | 30-day retention |
| **DO Spaces** | 250GB | $5 + $5 | Primary + replica |
| **Cloudflare Tunnel** | N/A | $0 | Free tier |
| **Secondary Droplet** (optional) | 2vCPU/4GB | $24 | High availability |
| **Load Balancer** (optional) | N/A | $20 | Multi-region |
| | | | |
| **Basic DR** | | **$45/mo** | Single region |
| **Full HA** | | **$89/mo** | Multi-region |

---

## Emergency Contacts

| Role | Contact | Responsibility |
|------|---------|----------------|
| On-Call Engineer | [Your contact] | DR execution |
| Backup Admin | [Contact] | Backup verification |
| DigitalOcean Support | support.digitalocean.com | Infrastructure issues |
| Cloudflare Support | support.cloudflare.com | Tunnel issues |

---

## Next Steps

1. ✅ **Set up automated backups** (use scripts provided)
2. ✅ **Enable DO Snapshots** (via DO dashboard)
3. ✅ **Configure DO Spaces** (create bucket, configure s3cmd)
4. ⬜ **Attach DO Volume** (for data persistence)
5. ⬜ **Run first DR test** (restore from backup)
6. ⬜ **Document your RTO/RPO** (measure actual times)
7. ⬜ **Schedule monthly DR drills** (calendar reminder)

---

**Your data is your business. Test your backups regularly. Don't learn about backup issues during an actual disaster.** ⚠️
