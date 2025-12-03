# GraphDB Enterprise Plugin System

GraphDB uses a **hybrid open-core model** where Community features are open source and Enterprise features are delivered as binary plugins.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  GraphDB Architecture                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌────────────────────────────────────────────────────────┐ │
│  │   Open Source Core (Public GitHub)                     │ │
│  │   ✅ Graph database engine                             │ │
│  │   ✅ REST + GraphQL APIs                               │ │
│  │   ✅ Custom HNSW vector search                         │ │
│  │   ✅ All graph algorithms                              │ │
│  │   ✅ Query language                                    │ │
│  │   ✅ Authentication (JWT, API keys)                    │ │
│  │   ✅ License validation framework                      │ │
│  │   ✅ Plugin loading system                             │ │
│  └────────────────────────────────────────────────────────┘ │
│                            ▲                                  │
│                            │ Plugin Interface                 │
│                            │                                  │
│  ┌────────────────────────────────────────────────────────┐ │
│  │   Enterprise Plugins (Private / Binary Distribution)   │ │
│  │   🔒 Cloudflare Vectorize (.so plugin)                │ │
│  │   🔒 R2 Automated Backups (.so plugin)                │ │
│  │   🔒 Change Data Capture (.so plugin)                 │ │
│  │   🔒 Multi-Region Replication (.so plugin)            │ │
│  │   🔒 Advanced Monitoring (.so plugin)                 │ │
│  │   🔒 SAML/OIDC Auth (.so plugin)                      │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                               │
└─────────────────────────────────────────────────────────────┘
```

## How It Works

### 1. License Validation (Public Code)
- Open source code validates Enterprise licenses at startup
- License includes hardware fingerprinting to prevent sharing
- Without valid license, Enterprise edition won't start

### 2. Plugin Loading (Public Code)
- Plugin loader is open source (pkg/plugins/)
- Loads `.so` (Go plugin) files from `./plugins/` directory
- Only loads when valid Enterprise license is present
- Plugins have access to validated license information

### 3. Enterprise Features (Closed Source)
- Distributed as compiled `.so` files
- Not included in public GitHub repository
- Provided to paying Enterprise customers only
- Cryptographically signed for authenticity

## Plugin Interface

All Enterprise plugins implement the `EnterprisePlugin` interface:

```go
type EnterprisePlugin interface {
    Name() string
    Version() string
    RequiredFeatures() []string
    Initialize(ctx context.Context, license *licensing.License, config map[string]interface{}) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    HealthCheck(ctx context.Context) error
}
```

### Specialized Plugin Types

**BackupPlugin** - For backup/restore features:
```go
type BackupPlugin interface {
    EnterprisePlugin
    Backup(ctx context.Context, destination string) error
    Restore(ctx context.Context, source string) error
    ListBackups(ctx context.Context) ([]BackupInfo, error)
}
```

**StoragePlugin** - For storage integrations:
```go
type StoragePlugin interface {
    EnterprisePlugin
    AttachToStorage(storage *storage.GraphStorage) error
}
```

**APIPlugin** - For custom API endpoints:
```go
type APIPlugin interface {
    EnterprisePlugin
    RegisterRoutes() map[string]interface{}
}
```

## Building Enterprise Plugins

### Example: R2 Backup Plugin

```go
// File: enterprise-plugins/r2-backup/plugin.go
package main

import (
    "context"
    "github.com/dd0wney/cluso-graphdb/pkg/licensing"
    "github.com/dd0wney/cluso-graphdb/pkg/plugins"
)

type R2BackupPlugin struct {
    name    string
    version string
    license *licensing.License
    // Private implementation details
}

// Plugin is the exported symbol GraphDB looks for
var Plugin plugins.BackupPlugin = &R2BackupPlugin{
    name:    "r2-backup",
    version: "1.0.0",
}

func (p *R2BackupPlugin) Initialize(ctx context.Context, license *licensing.License, config map[string]interface{}) error {
    // Initialize R2 client with config
    // Verify license has r2_backups feature enabled
    return nil
}

// Implement other interface methods...
```

### Build the Plugin

```bash
# Build as Go plugin
go build -buildmode=plugin -o r2-backup.so plugin.go

# Sign the plugin (optional but recommended)
gpg --detach-sign r2-backup.so

# Distribute to customers
# r2-backup.so (binary plugin)
# r2-backup.so.sig (signature)
```

## Using Enterprise Plugins

### Installation

```bash
# 1. Receive plugin from GraphDB Enterprise
#    (distributed via private download link or package)

# 2. Place plugin in plugins directory
mkdir -p ./plugins
cp r2-backup.so ./plugins/

# 3. Set plugin directory (optional)
export GRAPHDB_PLUGIN_DIR=./plugins

# 4. Start GraphDB Enterprise with valid license
GRAPHDB_EDITION=enterprise \
GRAPHDB_LICENSE_KEY='CGDB-XXXX-XXXX-XXXX-XXXX' \
./bin/server
```

### Plugin Configuration

Plugins receive configuration via environment variables:

```bash
# R2 Backup Plugin example
export PLUGIN_R2_ACCOUNT_ID=your-account-id
export PLUGIN_R2_ACCESS_KEY=your-access-key
export PLUGIN_R2_BUCKET=graphdb-backups

# Cloudflare Vectorize Plugin example
export PLUGIN_VECTORIZE_API_TOKEN=your-token
export PLUGIN_VECTORIZE_INDEX_NAME=graphdb-vectors
```

## Security

### Plugin Verification

Plugins should be verified before loading:

```bash
# Verify plugin signature
gpg --verify r2-backup.so.sig r2-backup.so

# Check plugin hash matches official hash
sha256sum r2-backup.so
# Compare with hash from https://graphdb.dev/plugins/checksums.txt
```

### Sandboxing

Plugins run in the same process as GraphDB but:
- ✅ Only loaded with valid Enterprise license
- ✅ Have access to license information (can verify features)
- ✅ Can be unloaded/reloaded without restart
- ⚠️ Run with same permissions as main process
- ⚠️ Should be from trusted sources only

## Available Enterprise Plugins

### r2-backup.so
- **Feature**: Automated R2 backups
- **Version**: 1.0.0
- **Size**: ~5MB
- **Dependencies**: None

**Features:**
- Automatic hourly backups to Cloudflare R2
- Zero egress costs
- Point-in-time restore
- Incremental backups
- 30-day retention

**Configuration:**
```bash
PLUGIN_R2_ACCOUNT_ID=your-account-id
PLUGIN_R2_ACCESS_KEY=your-access-key
PLUGIN_R2_SECRET_KEY=your-secret-key
PLUGIN_R2_BUCKET=graphdb-backups
PLUGIN_R2_SCHEDULE="0 * * * *"  # Hourly
```

### cloudflare-vectorize.so
- **Feature**: Cloudflare Vectorize integration
- **Version**: 1.0.0
- **Size**: ~3MB
- **Dependencies**: None

**Features:**
- Vector search with 5M+ dimensions
- Global edge deployment
- Sub-millisecond queries
- Automatic scaling

**Configuration:**
```bash
PLUGIN_VECTORIZE_ACCOUNT_ID=your-account-id
PLUGIN_VECTORIZE_API_TOKEN=your-token
PLUGIN_VECTORIZE_INDEX_NAME=graphdb-vectors
PLUGIN_VECTORIZE_DIMENSIONS=768
```

### cdc-queues.so
- **Feature**: Change Data Capture
- **Version**: 1.0.0
- **Size**: ~4MB
- **Dependencies**: None

**Features:**
- Stream all graph changes to Cloudflare Queues
- Debezium-compatible format
- Exactly-once delivery
- Dead letter queue support

**Configuration:**
```bash
PLUGIN_CDC_ACCOUNT_ID=your-account-id
PLUGIN_CDC_API_TOKEN=your-token
PLUGIN_CDC_QUEUE_NAME=graphdb-changes
PLUGIN_CDC_FORMAT=debezium
```

## Distribution Model

### For Developers (Building Plugins)

Enterprise plugins are **closed source** and kept in a **separate private repository**:

```
Repository Structure:
├── graphdb/                    # Public (this repo) - MIT License
│   ├── pkg/plugins/           # Plugin interface (public)
│   └── pkg/licensing/         # License validation (public)
│
└── graphdb-enterprise/         # Private repo - Commercial License
    ├── prometheus-metrics/    # Advanced metrics plugin
    │   ├── plugin.go
    │   ├── metrics.go
    │   └── go.mod
    ├── r2-backup/             # R2 backup plugin
    │   ├── plugin.go
    │   ├── r2_client.go
    │   └── go.mod
    ├── Makefile
    └── README.md
```

```bash
# Clone enterprise repo (requires access)
git clone git@github.com:dd0wney/graphdb-enterprise.git

# Build all plugins
cd graphdb-enterprise
make build-all

# Output:
# plugins/prometheus-metrics.so
# plugins/r2-backup.so
```

Each plugin is a separate Go module that references the community `graphdb` repo
via `replace` directive for local development.

### For Customers

Enterprise customers receive plugins via:

1. **Download Portal** (preferred)
   - Login to https://enterprise.graphdb.dev
   - Download signed plugin binaries
   - Automatic updates available

2. **Package Manager**
   ```bash
   # GraphDB plugin manager
   graphdb plugin install r2-backup
   graphdb plugin install cloudflare-vectorize
   ```

3. **Direct Distribution**
   - Included with Enterprise Docker image
   - Pre-installed in Enterprise binaries

## Development Guidelines

### Creating a New Enterprise Plugin

1. **Define the interface**
   ```go
   // Choose appropriate plugin type
   type MyPlugin struct {
       plugins.EnterprisePlugin
   }
   ```

2. **Implement required methods**
   - Name(), Version(), RequiredFeatures()
   - Initialize(), Start(), Stop(), HealthCheck()

3. **Add business logic**
   - Use the validated license
   - Check required features are enabled
   - Implement core functionality

4. **Build and test**
   ```bash
   go build -buildmode=plugin -o my-plugin.so
   ```

5. **Distribute to customers**
   - Sign the binary
   - Upload to customer portal
   - Update documentation

### Best Practices

1. **License Validation**
   - Always verify required features in Initialize()
   - Check license hasn't expired
   - Respect hardware binding

2. **Graceful Degradation**
   - Don't crash main process on plugin failure
   - Log errors clearly
   - Provide helpful error messages

3. **Configuration**
   - Use environment variables for config
   - Provide sensible defaults
   - Document all configuration options

4. **Versioning**
   - Follow semantic versioning
   - Test compatibility with multiple GraphDB versions
   - Document breaking changes

## FAQ

**Q: Can Community users see Enterprise plugin code?**  
A: No. Enterprise plugins are closed source and distributed as compiled binaries only.

**Q: Can someone reverse engineer the plugins?**  
A: While theoretically possible, plugins:
- Require valid Enterprise license to load
- Use license server for ongoing validation
- Include hardware fingerprinting
- Are legally protected under Enterprise license agreement

**Q: Why not just use obfuscation?**  
A: We use a hybrid approach:
- Open source core builds trust and community
- Binary plugins protect commercial value
- License validation prevents unauthorized use
- Legal agreements provide additional protection

**Q: Can I build my own plugins?**  
A: Yes! The plugin interface is public. You can build custom plugins for your own use. Commercial distribution requires authorization.

**Q: How do updates work?**  
A: Enterprise customers receive plugin updates through the customer portal or package manager. GraphDB checks for updates automatically (opt-in).

## Support

**Enterprise Plugin Issues:**
- Email: enterprise-support@graphdb.dev
- Support Portal: https://support.graphdb.dev
- SLA: 24-hour response for Enterprise customers

**Plugin Development:**
- Documentation: https://docs.graphdb.dev/plugins
- SDK: https://github.com/dd0wney/graphdb-plugin-sdk (if created)
- Examples: Contact enterprise-support@graphdb.dev

---

**This plugin system allows GraphDB to be:**
- ✅ Open source and transparent
- ✅ Commercially viable
- ✅ Extensible and flexible
- ✅ Community-friendly while protecting Enterprise value
