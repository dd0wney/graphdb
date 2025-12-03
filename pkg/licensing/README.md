# GraphDB Licensing Package

Server-based license validation with caching, background revalidation, and graceful degradation.

## Features

- **Server-based validation**: Validates licenses against license server
- **Multi-layer caching**: 24h primary cache + 7d fallback cache
- **Fail-open design**: Defaults to community tier if license server unreachable
- **Background revalidation**: Checks license every 24h without blocking startup
- **Feature gating**: Tier-based access control for Pro/Enterprise features
- **Zero-downtime**: License server outages don't brick customer instances

## Quick Start

### 1. Initialize License Manager

In your `main.go`:

```go
import "github.com/dd0wney/cluso-graphdb/pkg/licensing"

func main() {
    // Get license configuration from environment
    licenseKey := os.Getenv("GRAPHDB_LICENSE_KEY")
    licenseServerURL := os.Getenv("LICENSE_SERVER_URL")
    if licenseServerURL == "" {
        licenseServerURL = "https://license.graphdb.com"
    }

    // Initialize global license manager
    licensing.InitGlobal(licenseKey, licenseServerURL)

    // Get current license info
    license := licensing.Global().GetLicense()
    log.Printf("License tier: %s, valid: %v", license.Tier, license.IsValid())

    // ... rest of your application

    // Clean shutdown
    defer licensing.Global().Stop()
}
```

### 2. Gate Features in API Handlers

#### Simple feature check

```go
func HandlePageRank(w http.ResponseWriter, r *http.Request) {
    // Check if feature is available
    if !licensing.Global().HasFeature(licensing.FeaturePageRank) {
        http.Error(w, "PageRank requires Pro or Enterprise tier", http.StatusForbidden)
        return
    }

    // Feature is available, proceed
    results := runPageRank()
    json.NewEncoder(w).Encode(results)
}
```

#### Graceful error with upgrade link

```go
func HandleFraudDetection(w http.ResponseWriter, r *http.Request) {
    // CheckFeature returns helpful error message
    if err := licensing.Global().CheckFeature(licensing.FeatureFraudDetection); err != nil {
        // Error includes: "Feature 'fraud_detection' requires pro tier (current tier: community).
        // Upgrade at https://graphdb.com/pricing"
        http.Error(w, err.Error(), http.StatusForbidden)
        return
    }

    results := detectFraudRings()
    json.NewEncoder(w).Encode(results)
}
```

#### Tier-based feature fallback

```go
func HandleCommunityDetection(w http.ResponseWriter, r *http.Request) {
    var results interface{}

    if licensing.Global().HasFeature(licensing.FeatureCommunityDetection) {
        // Use advanced algorithm (Pro+)
        results = runLouvainCommunityDetection()
    } else {
        // Fall back to basic algorithm (Community)
        results = runSimpleClustering()
    }

    json.NewEncoder(w).Encode(results)
}
```

## License Tiers

### Community (Free)
- Basic graph queries (nodes, edges, traversal)
- Shortest path algorithm
- Breadth-first search (BFS)
- Depth-first search (DFS)

### Pro ($249/month)
All Community features plus:
- PageRank algorithm
- Community detection algorithms
- Trust and reputation scoring
- Fraud ring detection
- Time-based graph queries
- Comprehensive audit logging

### Enterprise ($999/month)
All Pro features plus:
- Role-based access control (RBAC)
- Single sign-on (SAML/OAuth)
- Priority email support (24h SLA)
- Multi-region replication

## Available Features

```go
// Community features
licensing.FeatureBasicQueries
licensing.FeatureShortestPath
licensing.FeatureBFS
licensing.FeatureDFS

// Pro features
licensing.FeaturePageRank
licensing.FeatureCommunityDetection
licensing.FeatureTrustScoring
licensing.FeatureFraudDetection
licensing.FeatureTemporalGraphs
licensing.FeatureAuditLogging

// Enterprise features
licensing.FeatureRBAC
licensing.FeatureSSO
licensing.FeaturePrioritySupport
licensing.FeatureMultiRegion
```

## Caching Strategy

The license client implements a resilient caching strategy:

1. **Primary cache (24h TTL)**: Valid license responses cached for 24 hours
2. **Fallback cache (7d TTL)**: Extended cache used when license server is down
3. **Fail-open**: If all caches expire and server unreachable, default to community tier
4. **Background revalidation**: License checked every 24h in background goroutine

### Cache Behavior

```
Startup → License Server Available?
   ├─ YES → Validate → Cache (24h) + Fallback (7d) → Success
   └─ NO  → Primary cache valid? (24h)
       ├─ YES → Use cached license
       └─ NO  → Fallback cache valid? (7d)
           ├─ YES → Use fallback license
           └─ NO  → Fail open to community tier
```

This ensures:
- **Zero downtime**: License server outages don't break customer instances
- **Grace period**: 7 days to fix license server before degrading to community
- **Automatic recovery**: When license server returns, next validation updates cache

## Environment Variables

- `GRAPHDB_LICENSE_KEY`: License key (format: `CGDB-XXXX-XXXX-XXXX-XXXX`)
- `LICENSE_SERVER_URL`: License server URL (default: `https://license.graphdb.com`)

## API Reference

### Manager Methods

```go
// Get current license info
license := licensing.Global().GetLicense()

// Check license tier
tier := licensing.Global().GetTier()
isValid := licensing.Global().IsValid()
isPro := licensing.Global().IsPro()
isEnterprise := licensing.Global().IsEnterprise()

// Feature checks
hasFeature := licensing.Global().HasFeature(licensing.FeaturePageRank)
err := licensing.Global().CheckFeature(licensing.FeaturePageRank)

// Require feature (panics if not available - use for critical startup checks)
licensing.Global().RequireFeature(licensing.FeatureRBAC)
```

### LicenseInfo Fields

```go
type LicenseInfo struct {
    Valid       bool          // Is license currently valid?
    Tier        LicenseTier   // community, pro, or enterprise
    Status      LicenseStatus // active, suspended, cancelled, expired
    ExpiresAt   *time.Time    // Expiration time (nil = never expires)
    MaxNodes    *int          // Maximum nodes allowed (nil = unlimited)
    ValidatedAt time.Time     // When license was last validated
    CachedUntil time.Time     // When cache expires
}

// Helper methods
license.IsValid()      // Checks Valid flag, expiration, and status
license.IsPro()        // True for Pro or Enterprise
license.IsEnterprise() // True for Enterprise only
license.HasFeature(feature) // True if tier supports feature
```

## Testing

See `example_test.go` for complete examples of:
- Basic feature checks
- Graceful error handling
- Tier-based fallback behavior
- License info endpoints
- Feature-based route registration

## License Server Protocol

### Validate Request

```json
POST /validate
{
  "licenseKey": "CGDB-XXXX-XXXX-XXXX-XXXX",
  "instanceId": "uuid-v4",
  "version": "1.0.0"
}
```

### Validate Response

```json
{
  "valid": true,
  "tier": "pro",
  "status": "active",
  "expiresAt": "2026-01-01T00:00:00Z",
  "maxNodes": null,
  "timestamp": "2025-11-19T02:00:00Z"
}
```

### Error Response

```json
{
  "valid": false,
  "error": "License is expired",
  "timestamp": "2025-11-19T02:00:00Z"
}
```

## Architecture Notes

- **Singleton pattern**: Global license manager initialized once at startup
- **Thread-safe**: All methods protected by RWMutex for concurrent access
- **Graceful shutdown**: Call `licensing.Global().Stop()` to stop background validation
- **No blocking**: Initial validation runs in background, doesn't block startup
- **Observability**: All validation events logged with structured logging

## Best Practices

1. **Initialize early**: Call `InitGlobal()` in `main()` before starting services
2. **Use CheckFeature**: Prefer `CheckFeature()` over `HasFeature()` for better error messages
3. **Fail gracefully**: Always provide fallback behavior for Pro/Enterprise features
4. **Log license info**: Log tier and validation status on startup for debugging
5. **Clean shutdown**: Call `Stop()` on shutdown to stop background goroutine
6. **Monitor cache hits**: Watch logs for validation failures and cache usage

## Troubleshooting

### License server unreachable

```
[License] Validation failed: http request: dial tcp: connection refused
[License] No valid cache, failing open to community tier
```

**Solution**: Check `LICENSE_SERVER_URL` is correct, or verify license server is running

### License validation fails

```
[License] Validation failed: invalid license: License is expired
```

**Solution**: Check license key is valid and not expired. Contact support to renew.

### Background validation stopped

```
[License] Background validation stopped
```

**Expected behavior**: Logged on clean shutdown when `Stop()` is called.
