# GraphDB - DigitalOcean Marketplace Submission Guide

Complete guide to submitting GraphDB to the DigitalOcean Marketplace as a 1-Click App.

---

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Building the Marketplace Image](#building-the-marketplace-image)
4. [Testing the Image](#testing-the-image)
5. [Marketplace Submission](#marketplace-submission)
6. [Post-Submission](#post-submission)

---

## Overview

### Why DigitalOcean Marketplace?

| Benefit | Value |
|---------|-------|
| **Discovery** | 600K+ DO customers searching for graph databases |
| **Credibility** | Official DO badge validates your product |
| **Zero Cost** | Free distribution channel (no marketplace fees) |
| **Co-marketing** | DO promotes apps in newsletters and blog |
| **Partner Program** | Access to DO Hatch, credits, technical support |

### What We're Building

A **1-Click App** that:
- ✅ Deploys GraphDB with one click
- ✅ Pre-configured with all dependencies (Docker, monitoring, backups)
- ✅ Automatic service start on boot
- ✅ Security hardened (SSH, firewall, fail2ban)
- ✅ Health monitoring and logging
- ✅ Backup/restore scripts included
- ✅ Professional welcome message and documentation

---

## Prerequisites

### 1. Install Packer

```bash
# macOS
brew tap hashicorp/tap
brew install hashicorp/tap/packer

# Linux
curl -fsSL https://apt.releases.hashicorp.com/gpg | sudo apt-key add -
sudo apt-add-repository "deb [arch=amd64] https://apt.releases.hashicorp.com $(lsb_release -cs) main"
sudo apt-get update && sudo apt-get install packer
```

### 2. Install doctl (DigitalOcean CLI)

```bash
# macOS
brew install doctl

# Linux
cd /tmp
wget https://github.com/digitalocean/doctl/releases/download/v1.104.0/doctl-1.104.0-linux-amd64.tar.gz
tar xf doctl-*.tar.gz
sudo mv doctl /usr/local/bin

# Authenticate
doctl auth init
```

### 3. Get DigitalOcean API Token

1. Go to https://cloud.digitalocean.com/account/api/tokens
2. Click "Generate New Token"
3. Name: "Packer GraphDB Marketplace"
4. Scopes: Read and Write
5. Copy the token

### 4. Set Environment Variables

```bash
export DIGITALOCEAN_API_TOKEN="your-do-api-token"
```

---

## Building the Marketplace Image

### Step 1: Navigate to Packer Directory

```bash
cd packer/
```

### Step 2: Initialize Packer

```bash
packer init .
```

### Step 3: Validate Packer Template

```bash
packer validate graphdb.pkr.hcl
```

Expected output:
```
The configuration is valid.
```

### Step 4: Build the Image

```bash
# Standard build
packer build graphdb.pkr.hcl

# Build with specific GraphDB version
packer build -var 'graphdb_version=1.0.0' graphdb.pkr.hcl

# Build in specific region
packer build -var 'region=sfo3' graphdb.pkr.hcl
```

Build process takes ~10-15 minutes:

```
==> digitalocean.graphdb: Creating temporary SSH key...
==> digitalocean.graphdb: Creating droplet...
==> digitalocean.graphdb: Waiting for droplet to become active...
==> digitalocean.graphdb: Waiting for SSH to become available...
==> digitalocean.graphdb: Connected to SSH!
==> digitalocean.graphdb: Provisioning with shell script: /tmp/packer-shell123456
==> digitalocean.graphdb: [1/8] Installing Docker...
==> digitalocean.graphdb: ✓ Docker installed successfully
==> digitalocean.graphdb: [2/8] Installing system dependencies...
...
==> digitalocean.graphdb: Creating snapshot...
==> digitalocean.graphdb: Snapshot created: graphdb-marketplace-20250119123456
==> digitalocean.graphdb: Destroying droplet...
Build 'digitalocean.graphdb' finished after 14 minutes 32 seconds.
```

### Step 5: Get Snapshot ID

```bash
doctl compute snapshot list --resource droplet --format ID,Name,Created
```

Example output:
```
ID          Name                                    Created At
123456789   graphdb-marketplace-20250119123456      2025-01-19T12:34:56Z
```

Save the **Snapshot ID** for testing.

---

## Testing the Image

### Test 1: Create Droplet from Snapshot

```bash
# Replace 123456789 with your snapshot ID
doctl compute droplet create graphdb-test-1 \
  --image 123456789 \
  --size s-2vcpu-4gb \
  --region nyc3 \
  --ssh-keys $(doctl compute ssh-key list --format ID --no-header | head -1) \
  --wait

# Get droplet IP
DROPLET_IP=$(doctl compute droplet list graphdb-test-1 --format PublicIPv4 --no-header)
echo "Droplet IP: $DROPLET_IP"
```

### Test 2: Wait for First Boot

```bash
# Wait 2-3 minutes for first-boot script to complete
sleep 120

# SSH and check services
ssh root@$DROPLET_IP 'systemctl status graphdb'
```

### Test 3: Verify GraphDB is Running

```bash
# Check health endpoint
curl http://$DROPLET_IP:8080/health

# Expected output:
# {"status":"healthy","uptime":120,"version":"1.0.0"}
```

### Test 4: Test API

```bash
# Create a test node
curl -X POST http://$DROPLET_IP:8080/api/v1/nodes \
  -H "Content-Type: application/json" \
  -d '{
    "type": "test",
    "properties": {"name": "Test Node"}
  }'

# Query nodes
curl http://$DROPLET_IP:8080/api/v1/nodes?limit=10
```

### Test 5: Verify Documentation

```bash
ssh root@$DROPLET_IP 'cat /root/graphdb-docs/README.txt'
```

### Test 6: Verify Backups

```bash
ssh root@$DROPLET_IP 'BACKUP_TYPE=full /usr/local/bin/backup-graphdb.sh'
```

### Test 7: Cleanup Test Droplet

```bash
doctl compute droplet delete graphdb-test-1 --force
```

---

## Marketplace Submission

### Step 1: Prepare Submission Materials

#### A. Application Information

- **App Name**: GraphDB
- **App Category**: Databases
- **Tagline**: High-performance graph database for trust scoring, fraud detection, and knowledge graphs
- **Short Description** (150 chars):
  > Graph database with 430K writes/sec, built-in trust scoring, fraud detection, and Cypher-like queries. Edge-ready with Cloudflare Workers SDK.

- **Long Description**:
```markdown
# GraphDB - High-Performance Graph Database

GraphDB is a graph database built for modern applications requiring trust networks, fraud detection, and knowledge graphs.

## Features

- **High Performance**: 430K writes/second with LSM-tree storage
- **Built-in Algorithms**: PageRank, community detection, shortest path, trust scoring
- **Cypher-like Queries**: Familiar syntax for graph traversal
- **Edge Computing**: Native Cloudflare Workers SDK for edge deployment
- **Fraud Detection**: Built-in algorithms for identifying fraud rings and suspicious patterns
- **Easy to Deploy**: One-click deployment with automated backups and disaster recovery

## Perfect For

- **Trust & Reputation Systems**: Social networks, marketplaces, rating systems
- **Fraud Detection**: Banking, e-commerce, insurance, identity verification
- **Knowledge Graphs**: Content recommendations, research databases, enterprise knowledge
- **Recommendation Engines**: Product recommendations, content discovery, social connections

## What's Included

- GraphDB Community Edition (pre-installed)
- Docker and Docker Compose (configured)
- Automated backup scripts
- Disaster recovery procedures
- Health monitoring (node_exporter)
- Security hardening (fail2ban, UFW firewall)
- Complete documentation

## Getting Started

After deploying:

1. Access GraphDB API at `http://your-droplet-ip:8080`
2. View documentation: `cat /root/graphdb-docs/README.txt`
3. Create your first node: See `/root/graphdb-docs/QUICK-START.md`

## Support

- Documentation: https://github.com/dd0wney/graphdb
- Issues: https://github.com/dd0wney/graphdb/issues
- Community: Coming soon

## Pricing

GraphDB Community Edition is **free and open source**. Enterprise Edition with advanced features available separately.
```

#### B. Technical Information

- **Minimum Droplet Size**: s-1vcpu-2gb ($12/month)
- **Recommended Size**: s-2vcpu-4gb ($24/month)
- **Ports Used**:
  - 8080/tcp - GraphDB API (HTTP)
  - 9100/tcp - Metrics (optional, node_exporter)
  - 22/tcp - SSH (standard)

- **Software Stack**:
  - Ubuntu 22.04 LTS
  - Docker 24.x
  - GraphDB latest
  - node_exporter 1.7.0

- **Snapshot ID**: (Your snapshot ID from build)

#### C. Visual Assets

**Required**:
1. **App Icon** (200x200px PNG)
   - GraphDB logo on transparent background
   - High contrast, visible at small sizes

2. **Screenshots** (1200x800px PNG, at least 3):
   - Screenshot 1: GraphDB API response (health endpoint)
   - Screenshot 2: Example query result
   - Screenshot 3: Documentation/welcome screen

3. **Banner** (1920x400px PNG) - Optional but recommended
   - GraphDB branding with tagline

#### D. Support Information

- **Support URL**: https://github.com/dd0wney/graphdb/issues
- **Documentation URL**: https://github.com/dd0wney/graphdb
- **Support Type**: Community (free) / Enterprise (paid)
- **Support Email**: support@yourdomain.com (or GitHub issues)

### Step 2: Submit Application

1. Go to **https://marketplace.digitalocean.com/vendors**
2. Click **"Become a Vendor"** or **"Submit Your App"**
3. Fill out vendor profile:
   - Company name
   - Contact information
   - Company website
4. Submit 1-Click App:
   - Upload application information (from Step 1)
   - Provide snapshot ID
   - Upload visual assets
   - Submit for review

### Step 3: DigitalOcean Review Process

**Timeline**: 2-4 weeks

**What DO Reviews**:
- ✅ Security (no hardcoded credentials, proper firewall)
- ✅ Stability (app starts reliably, handles restarts)
- ✅ Documentation quality
- ✅ User experience (clear welcome message, easy to use)
- ✅ Image cleanliness (no leftover build artifacts)
- ✅ Compliance with DO Marketplace requirements

**Common Rejection Reasons**:
- Hardcoded passwords or API keys
- Missing documentation
- Services don't start on boot
- Security vulnerabilities (open ports, weak SSH config)
- Poor user experience

### Step 4: Respond to Feedback

DO may request changes:
- Make requested changes to Packer template
- Rebuild image: `packer build graphdb.pkr.hcl`
- Submit new snapshot ID
- Resubmit for review

---

## Post-Submission

### Once Approved

1. **Your app goes live** at `https://marketplace.digitalocean.com/apps/graphdb`
2. **Customers can deploy** with one click
3. **You get analytics** on deployments and usage
4. **DO promotes** your app in newsletters, blog posts, social media

### Marketing Opportunities

1. **Blog Announcement**:
   - "GraphDB Now Available on DigitalOcean Marketplace"
   - Share deployment guide and use cases

2. **DO Co-Marketing**:
   - Request co-marketing from DO partner team
   - Joint webinar or blog post

3. **Community Engagement**:
   - Answer questions on DO community forums
   - Share success stories from users

### Ongoing Maintenance

**Monthly Tasks**:
- Update image with security patches
- Test new GraphDB versions
- Monitor customer feedback/issues

**Update Process**:
```bash
# Update Packer template with new version
packer build -var 'graphdb_version=1.1.0' graphdb.pkr.hcl

# Submit new snapshot to DO
# DO reviews and approves update
# New version goes live
```

---

## Checklist

Before submitting to marketplace:

- [ ] Packer template builds successfully
- [ ] All scripts are executable and work
- [ ] Security hardening applied (SSH, firewall, fail2ban)
- [ ] Documentation created and accessible
- [ ] First-boot script tested and working
- [ ] GraphDB starts automatically on boot
- [ ] Health endpoint responds correctly
- [ ] Backup scripts included and functional
- [ ] No sensitive data in snapshot (API keys, passwords)
- [ ] Visual assets created (icon, screenshots, banner)
- [ ] Support information documented
- [ ] Tested on multiple droplet sizes

---

## Resources

- **DO Marketplace Docs**: https://docs.digitalocean.com/products/marketplace/
- **Packer DO Builder**: https://www.packer.io/docs/builders/digitalocean
- **Marketplace Requirements**: https://marketplace.digitalocean.com/vendors
- **DO Community**: https://www.digitalocean.com/community

---

## Support

Issues with the Packer build or submission process:
- GitHub: https://github.com/dd0wney/graphdb/issues
- Tag: `marketplace` or `packer`

---

**Ready to submit? Follow the steps above and GraphDB will be live on DO Marketplace in 2-4 weeks!**
