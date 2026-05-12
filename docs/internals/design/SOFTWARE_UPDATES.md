# Software Update System

GraphDB includes a modern, automated software update mechanism that allows for frictionless in-place updates of both the Server and Admin CLI.

## Overview

As of v1.0, GraphDB is officially a **single-node database**. The previous multi-node orchestration tools (`graphdb-upgrade`) and legacy primary/replica binaries have been retired. 

The new update system provides:
- **In-Place Updates**: Update the running binary with a single command.
- **Verification**: Automatic version comparison and asset matching for host OS/Architecture.
- **Graceful Lifecycle**: Integrated with systemd/Docker for automated restarts after update.

---

## Components

### 1. Admin CLI (`graphdb-admin update`)
The primary interface for managing software updates.

```bash
# Check for updates
graphdb-admin update --dry-run

# Apply latest stable update
graphdb-admin update

# Switch to beta channel
graphdb-admin update --channel beta
```

### 2. Admin API
The server exposes REST endpoints for checking and applying updates programmatically (requires admin permissions).

- `GET  /admin/update/check` - Returns available versions and release notes.
- `POST /admin/update/apply` - Fetches, verifies, and installs the update, then triggers a graceful restart.

### 3. Updater Core (`pkg/updater/`)
The shared library responsible for manifest resolution, asset downloading, and atomic binary swapping.

---

## Best Practices

✅ **Keep Backups**: Always take a snapshot before applying updates.
```bash
curl -X POST http://localhost:8080/snapshot
```

✅ **Verify Version**: Check your current version after update.
```bash
graphdb-admin version
```

✅ **Use Dry Run**: Preview changes before applying.
```bash
graphdb-admin update --dry-run
```

---

## Legacy Note
For information on the retired multi-node orchestration logic (A8.1 era), please consult the repository history. The `graphdb-upgrade` tool is no longer supported.

---

## Support
For help with software updates, please contact the GraphDB engineering team.
