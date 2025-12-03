# GraphDB Packer Configuration for DigitalOcean Marketplace

This directory contains the Packer configuration to build a 1-click deployable DigitalOcean Marketplace image for GraphDB.

## Quick Start

```bash
# Set DO API token
export DIGITALOCEAN_API_TOKEN="your-token-here"

# Build the image
packer build graphdb.pkr.hcl

# Test the snapshot
doctl compute droplet create graphdb-test \
  --image <snapshot-id> \
  --size s-2vcpu-4gb \
  --region nyc3
```

## File Structure

```
packer/
├── graphdb.pkr.hcl                    # Main Packer template
├── scripts/
│   ├── install-graphdb.sh             # Install GraphDB and dependencies
│   ├── first-boot.sh                  # First-boot configuration
│   ├── graphdb-first-boot.service     # Systemd service for first boot
│   ├── security-hardening.sh          # Security configurations
│   ├── cleanup.sh                     # Cleanup before snapshot
│   ├── verify-installation.sh         # Verify installation
│   └── motd.txt                       # Message of the day
└── README.md                          # This file
```

## What Gets Installed

- **Base OS**: Ubuntu 22.04 LTS
- **Docker**: Latest stable version
- **GraphDB**: Community Edition (latest)
- **Monitoring**: node_exporter for Prometheus
- **Security**: fail2ban, UFW firewall, hardened SSH
- **Utilities**: curl, wget, jq, s3cmd, rsync
- **Scripts**: backup, restore, DR testing
- **Documentation**: Quick start guides and API docs

## Build Process

The Packer build performs these steps:

1. **System Update**: Updates all packages to latest versions
2. **Docker Installation**: Installs Docker and Docker Compose
3. **GraphDB Setup**: Pulls GraphDB image and creates docker-compose.yml
4. **Backup Scripts**: Installs backup/restore/DR testing scripts
5. **First-Boot Setup**: Configures first-boot service for instance-specific setup
6. **Security Hardening**: Applies SSH hardening, firewall rules, fail2ban
7. **Cleanup**: Removes temporary files, logs, and sensitive data
8. **Verification**: Checks all components are correctly installed
9. **Snapshot Creation**: Creates multi-region snapshot for marketplace

## Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `do_api_token` | env(DIGITALOCEAN_API_TOKEN) | DO API token |
| `region` | nyc3 | Build region |
| `size` | s-1vcpu-2gb | Temporary build droplet size |
| `graphdb_version` | latest | GraphDB version to install |
| `snapshot_name` | graphdb-marketplace-{timestamp} | Snapshot name |

## Custom Build

```bash
# Build with specific GraphDB version
packer build -var 'graphdb_version=1.0.0' graphdb.pkr.hcl

# Build in different region
packer build -var 'region=sfo3' graphdb.pkr.hcl

# Build with custom snapshot name
packer build -var 'snapshot_name=graphdb-custom' graphdb.pkr.hcl
```

## Testing

After building, test the image:

```bash
# 1. Create test droplet from snapshot
./test-snapshot.sh <snapshot-id>

# 2. Or manually:
doctl compute droplet create graphdb-test \
  --image <snapshot-id> \
  --size s-2vcpu-4gb \
  --region nyc3 \
  --ssh-keys $(doctl compute ssh-key list --format ID --no-header | head -1)

# 3. Wait for boot (2-3 minutes)
sleep 120

# 4. Get droplet IP
DROPLET_IP=$(doctl compute droplet list graphdb-test --format PublicIPv4 --no-header)

# 5. Test GraphDB
curl http://$DROPLET_IP:8080/health

# 6. SSH and verify
ssh root@$DROPLET_IP 'systemctl status graphdb'
ssh root@$DROPLET_IP 'cat /root/graphdb-docs/README.txt'

# 7. Cleanup
doctl compute droplet delete graphdb-test --force
```

## Marketplace Submission

See [MARKETPLACE-SUBMISSION.md](../MARKETPLACE-SUBMISSION.md) for complete submission guide.

Quick checklist:
- [ ] Build passes without errors
- [ ] Snapshot created in all regions
- [ ] Test droplet boots successfully
- [ ] GraphDB starts automatically
- [ ] Health endpoint responds
- [ ] Documentation accessible
- [ ] Security hardening applied
- [ ] No sensitive data in snapshot

## Troubleshooting

### Build Fails at SSH Connection
- **Cause**: Droplet not ready or SSH key issues
- **Fix**: Check SSH key is uploaded to DO: `doctl compute ssh-key list`

### GraphDB Doesn't Start
- **Cause**: Docker image pull failed or docker-compose.yml issues
- **Fix**: SSH to test droplet and check: `docker logs graphdb`

### Scripts Not Executable
- **Cause**: chmod command failed in provisioner
- **Fix**: Check `install-graphdb.sh` executed successfully

### Snapshot Too Large
- **Cause**: Build artifacts or Docker images taking space
- **Fix**: Enable zero-out in `cleanup.sh` or prune Docker images

## Support

- **Documentation**: [MARKETPLACE-SUBMISSION.md](../MARKETPLACE-SUBMISSION.md)
- **Issues**: https://github.com/dd0wney/graphdb/issues
- **Packer Docs**: https://www.packer.io/docs/builders/digitalocean

---

**Ready to build? Run `packer build graphdb.pkr.hcl` and you'll have a marketplace-ready snapshot in ~15 minutes!**
