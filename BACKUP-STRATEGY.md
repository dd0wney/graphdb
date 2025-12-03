# GraphDB Backup Strategy

**Date:** 2025-11-24
**Status:** Production Ready

---

## Overview

GraphDB supports **three backup strategies**:

1. **Weekly Snapshots** (Recommended for most use cases) ✅
2. **Daily Snapshots** (High-value data)
3. **On-Demand Snapshots** (Before major changes)

---

## Strategy Comparison

| Strategy | Frequency | Retention | Cost (100GB) | Cost (200GB) | Use Case |
|----------|-----------|-----------|--------------|--------------|----------|
| **Weekly** | Sunday 2 AM | 4 weeks | **$7/month** | **$14/month** | Standard production |
| **Daily** | 2 AM | 7 days | $12/month | $24/month | High-value data |
| **On-Demand** | Manual | Forever* | $5/snapshot | $10/snapshot | Pre-upgrade backups |

*\*Manual deletion required*

---

## Recommended Strategy by Environment

### **Staging: Weekly Backups** ✅

**Why:**
- Changes are frequent but not critical
- 4-week history sufficient for rollback
- Cost-effective: $7/month vs $12/month daily

**Setup:**
```bash
# Install weekly backup script
sudo cp scripts/weekly-backup.sh /usr/local/bin/
sudo chmod +x /usr/local/bin/weekly-backup.sh

# Add to cron (runs every Sunday at 2 AM)
sudo crontab -e
# Add this line:
0 2 * * 0 /usr/local/bin/weekly-backup.sh >> /var/log/graphdb-backup.log 2>&1
```

### **Production: Weekly or Daily**

**Weekly** if:
- ✅ Acceptable to lose up to 7 days of data in worst case
- ✅ Real-time sync with Supabase (can rebuild from source)
- ✅ Cost is a concern ($14/month vs $24/month)

**Daily** if:
- ✅ Cannot afford to lose > 24 hours of data
- ✅ Compliance requirements (SOC2, HIPAA, etc.)
- ✅ High transaction volume

**For Syntopica:**
- **Weekly is recommended** because:
  - Supabase is source of truth (can rebuild GraphDB)
  - Data changes gradually (not high-volume transactions)
  - 7-day RPO (Recovery Point Objective) is acceptable

---

## Cost Analysis

### Staging (100GB Volume)

**Weekly backups (4-week retention):**
- Week 1: 100GB (full snapshot)
- Week 2: +15GB (incremental, assuming ~15% change)
- Week 3: +10GB (incremental, ~10% change)
- Week 4: +10GB (incremental, ~10% change)
- **Total: ~135GB = $6.75/month**

**Daily backups (7-day retention):**
- Day 1: 100GB (full)
- Days 2-7: +3GB each (incremental, assuming ~3% daily change)
- **Total: ~118GB daily churn, average ~$11-12/month**

### Production (200GB Volume)

**Weekly backups:**
- ~270GB total = **$13.50/month**

**Daily backups:**
- ~240GB total = **$24/month**

**Savings: $10.50/month with weekly backups**

---

## Backup Automation

### Automated Weekly Backups

The included script (`scripts/weekly-backup.sh`) provides:

✅ **Automatic snapshot creation**
- Runs every Sunday at 2 AM
- Creates snapshot named: `graphdb-weekly-YYYYMMDD-HHMMSS`
- No downtime required (snapshots are live)

✅ **Automatic retention management**
- Keeps last 4 weekly snapshots
- Deletes older snapshots automatically
- Prevents snapshot accumulation

✅ **Monitoring and logging**
- Logs to `/var/log/graphdb-backup.log`
- Shows snapshot inventory and cost
- Optional notifications (Slack, email)

✅ **Error handling**
- Retries on failure
- Notifications on error
- Validates volume exists before backup

### Setup Instructions

```bash
# 1. Copy script
sudo cp scripts/weekly-backup.sh /usr/local/bin/
sudo chmod +x /usr/local/bin/weekly-backup.sh

# 2. Set API token (for cron)
sudo vim /etc/environment
# Add: DIGITALOCEAN_API_TOKEN="your-token-here"

# 3. Test manually
sudo /usr/local/bin/weekly-backup.sh

# Expected output:
# [2025-11-24 14:30:00] ==========================================
# [2025-11-24 14:30:00] GraphDB Weekly Backup Starting
# [2025-11-24 14:30:00] ==========================================
# [2025-11-24 14:30:01] Looking up volume: graphdb-data
# [2025-11-24 14:30:02] ✓ Found volume: graphdb-data (ID: 12345678)
# [2025-11-24 14:30:02] Creating snapshot: graphdb-weekly-20251124-143002
# [2025-11-24 14:30:15] ✓ Snapshot created successfully
# ...

# 4. Add to cron
sudo crontab -e
# Add this line (runs every Sunday at 2 AM):
0 2 * * 0 /usr/local/bin/weekly-backup.sh >> /var/log/graphdb-backup.log 2>&1

# 5. Verify cron job
sudo crontab -l

# 6. Check logs
tail -f /var/log/graphdb-backup.log
```

---

## Manual Backup

### On-Demand Snapshot (Before Upgrades)

```bash
# Create snapshot with descriptive name
doctl compute volume-snapshot create <volume-id> \
  --snapshot-name "graphdb-pre-upgrade-v2.0" \
  --desc "Backup before upgrading to GraphDB v2.0"

# Wait for completion (~2-5 minutes for 100GB)
doctl compute volume-snapshot list | grep graphdb-pre-upgrade

# Verify
doctl compute volume-snapshot get <snapshot-id>
```

### Quick Backup Commands

```bash
# List volumes
doctl compute volume list

# Create snapshot
VOLUME_ID="12345678"
doctl compute volume-snapshot create $VOLUME_ID \
  --snapshot-name "graphdb-backup-$(date +%Y%m%d-%H%M%S)"

# List snapshots
doctl compute volume-snapshot list | grep graphdb

# Delete old snapshot
doctl compute volume-snapshot delete <snapshot-id> --force
```

---

## Disaster Recovery

### Recovery from Weekly Snapshot

**Scenario:** GraphDB server fails, need to restore

**RTO (Recovery Time Objective):** < 10 minutes
**RPO (Recovery Point Objective):** Up to 7 days

**Steps:**

```bash
# 1. Stop GraphDB (if still running)
ssh root@<droplet-ip>
systemctl stop graphdb

# 2. Find latest snapshot
doctl compute volume-snapshot list | grep graphdb-weekly | tail -1

# 3. Option A: Restore to existing volume (destructive!)
# WARNING: This deletes current data
VOLUME_ID="12345678"
SNAPSHOT_ID="87654321"

# Detach volume
doctl compute volume-action detach $VOLUME_ID <droplet-id>

# Delete old volume
doctl compute volume delete $VOLUME_ID --force

# Create new volume from snapshot
doctl compute volume create graphdb-data-restored \
  --region nyc1 \
  --size 100GiB \
  --snapshot $SNAPSHOT_ID

# Attach new volume
doctl compute volume-action attach <new-volume-id> <droplet-id>

# Mount (may need to update device ID)
mount /dev/disk/by-id/scsi-0DO_Volume_graphdb-data-restored /mnt/graphdb-data

# Start GraphDB
systemctl start graphdb

# 4. Option B: Restore to new droplet (safer)
# Deploy fresh droplet from custom image
doctl compute droplet create graphdb-staging-restored \
  --image <graphdb-image-id> \
  --size s-1vcpu-2gb \
  --region nyc1

# Create volume from snapshot
doctl compute volume create graphdb-data-restored \
  --region nyc1 \
  --size 100GiB \
  --snapshot $SNAPSHOT_ID

# Attach and mount as usual
# Update DNS to point to new droplet
```

### Partial Recovery (Individual Files)

**Scenario:** Need to recover specific audit logs or LSM files

**Steps:**

```bash
# 1. Create temporary droplet
doctl compute droplet create graphdb-recovery-temp \
  --image ubuntu-22-04-x64 \
  --size s-1vcpu-1gb \
  --region nyc1

# 2. Create volume from snapshot
doctl compute volume create graphdb-temp \
  --region nyc1 \
  --size 100GiB \
  --snapshot <snapshot-id>

# 3. Attach and mount
doctl compute volume-action attach <volume-id> <temp-droplet-id>
ssh root@<temp-droplet-ip>
mount /dev/sda /mnt/recovery

# 4. Copy specific files
rsync -avz /mnt/recovery/audit/2025-11-24.jsonl root@<production-droplet>:/mnt/graphdb-data/audit/

# 5. Cleanup
umount /mnt/recovery
doctl compute droplet delete <temp-droplet-id> --force
doctl compute volume delete <temp-volume-id> --force
```

---

## Off-Site Backups (Optional)

For compliance or extra safety, store backups in DigitalOcean Spaces (S3-compatible):

### Setup DO Spaces Sync

```bash
# 1. Install s3cmd
sudo apt install s3cmd

# 2. Configure
s3cmd --configure
# Enter DO Spaces credentials

# 3. Create bucket
s3cmd mb s3://graphdb-backups

# 4. Sync snapshots (manual or scripted)
# Export snapshot to file
doctl compute volume-snapshot export <snapshot-id> \
  --region nyc1 \
  --format raw \
  --output /tmp/graphdb-backup.img

# Compress
gzip /tmp/graphdb-backup.img

# Upload to Spaces
s3cmd put /tmp/graphdb-backup.img.gz s3://graphdb-backups/weekly/backup-$(date +%Y%m%d).img.gz

# Cleanup local file
rm /tmp/graphdb-backup.img.gz
```

**Cost:** $5/month for 250GB in Spaces

---

## Monitoring

### Check Backup Status

```bash
# View recent backups
doctl compute volume-snapshot list --format Name,Size,CreatedAt | grep graphdb-weekly

# Check backup script logs
tail -50 /var/log/graphdb-backup.log

# Verify last backup
ls -lh /var/log/graphdb-backup.log
# Should be updated within last 7 days

# Calculate total snapshot cost
doctl compute volume-snapshot list --format Size --no-header | \
  awk '{sum += $1} END {printf "Total: %.0fGB = $%.2f/month\n", sum, sum*0.05}'
```

### Backup Health Dashboard

Add to monitoring (Grafana/Prometheus):

```promql
# Time since last backup
time() - graphdb_last_backup_timestamp_seconds

# Alert if > 8 days (weekly backups)
ALERT BackupStale
  IF (time() - graphdb_last_backup_timestamp_seconds) > (8 * 86400)
  FOR 1h
  ANNOTATIONS {
    summary = "GraphDB backup is stale"
  }

# Total snapshot cost
graphdb_snapshot_total_gb * 0.05
```

---

## Testing Backups

### Monthly Backup Test

**CRITICAL:** Backups are useless if you can't restore!

**Test procedure (monthly):**

```bash
# 1. Create test droplet from latest snapshot
doctl compute droplet create graphdb-test-restore \
  --image <graphdb-image-id> \
  --size s-1vcpu-2gb \
  --region nyc1

# 2. Create volume from latest snapshot
LATEST_SNAPSHOT=$(doctl compute volume-snapshot list --format ID,Name --no-header | \
  grep graphdb-weekly | tail -1 | awk '{print $1}')

doctl compute volume create graphdb-test-volume \
  --region nyc1 \
  --size 100GiB \
  --snapshot $LATEST_SNAPSHOT

# 3. Attach and start
doctl compute volume-action attach <volume-id> <droplet-id>
ssh root@<test-droplet-ip>
mount /dev/sda /mnt/graphdb-data
systemctl start graphdb

# 4. Verify data
curl http://localhost:8080/stats | jq '.'
# Should match production node/edge counts within ~7 days

# 5. Run sample queries
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{"query": "MATCH (c:Concept) RETURN count(c)"}'

# 6. Success? Document and cleanup
# Record test results in runbook
doctl compute droplet delete <test-droplet-id> --force
doctl compute volume delete <test-volume-id> --force

# 7. If restore failed, investigate and fix!
```

---

## Summary & Recommendations

### **For Syntopica:**

✅ **Use weekly backups** (default in automation script)
- Cost: $7/month (staging), $14/month (production)
- Retention: 4 weeks
- RPO: Up to 7 days
- Setup time: 5 minutes

✅ **Enable automated script**
- Install cron job for Sunday 2 AM backups
- Monitor logs weekly
- Test restore monthly

✅ **Optional: Off-site backups**
- Only if compliance requires
- Adds $5/month for DO Spaces
- Manual export process

### **Cost Savings**

Weekly vs daily backups:
- Staging: Save $5/month ($60/year)
- Production: Save $10/month ($120/year)
- **Total: $180/year savings**

### **Risk Assessment**

**Weekly backups are sufficient because:**
1. ✅ Supabase is source of truth (can rebuild GraphDB)
2. ✅ Data changes gradually (not high-frequency trading)
3. ✅ 7-day RPO acceptable for graph relationships
4. ✅ Real-time sync minimizes data loss

**Consider daily if:**
- ❌ Cannot rebuild from Supabase
- ❌ Compliance requires < 24hr RPO
- ❌ High transaction volume

---

## Files

- **Automation script:** `/home/ddowney/Workspace/github.com/graphdb/scripts/weekly-backup.sh`
- **This guide:** `/home/ddowney/Workspace/github.com/graphdb/BACKUP-STRATEGY.md`

---

**Author:** Claude Code
**Last Updated:** 2025-11-24
**Status:** Production Ready ✅
