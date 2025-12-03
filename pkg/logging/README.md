# Structured Logging Package

A lightweight, thread-safe structured logging package for GraphDB that outputs JSON logs for easy parsing by log aggregation tools like ELK, Splunk, or Grafana Loki.

## Features

- **Structured JSON Output**: All logs are emitted as JSON for easy parsing
- **Multiple Log Levels**: DEBUG, INFO, WARN, ERROR with filtering support
- **Contextual Fields**: Attach key-value pairs to log entries
- **Child Loggers**: Create loggers with preset fields using `With()`
- **Thread-Safe**: Safe for concurrent use across goroutines
- **Environment Configuration**: Control log level via `LOG_LEVEL` environment variable
- **Zero Dependencies**: Only uses Go standard library

## Quick Start

### Basic Usage

```go
import "github.com/dd0wney/cluso-graphdb/pkg/logging"

// Use the default global logger
logging.Info("server started",
    logging.Int("port", 8080),
    logging.String("version", "1.0.0"),
)
```

Output:
```json
{
  "time": "2025-11-19T10:30:00.123456789+11:00",
  "level": "INFO",
  "msg": "server started",
  "fields": {
    "port": 8080,
    "version": "1.0.0"
  }
}
```

### Log Levels

```go
// Different log levels
logging.Debug("verbose debug info", logging.String("detail", "value"))
logging.Info("informational message")
logging.Warn("warning message", logging.Error(err))
logging.ErrorLog("error occurred", logging.String("operation", "create"))
```

### Custom Logger

```go
import (
    "os"
    "github.com/dd0wney/cluso-graphdb/pkg/logging"
)

// Create a custom logger
logger := logging.NewJSONLogger(os.Stdout, logging.InfoLevel)

logger.Info("custom logger message")

// Change log level dynamically
logger.SetLevel(logging.DebugLevel)
logger.Debug("now debug is enabled")
```

### Child Loggers with Preset Fields

```go
// Create a child logger with preset fields
storageLogger := logging.With(
    logging.String("component", "storage"),
    logging.String("module", "graphdb"),
)

// All logs from this logger will include the preset fields
storageLogger.Info("node created",
    logging.Uint64("node_id", 12345),
    logging.Int("property_count", 5),
)
```

Output:
```json
{
  "time": "2025-11-19T10:30:01.123456789+11:00",
  "level": "INFO",
  "msg": "node created",
  "fields": {
    "component": "storage",
    "module": "graphdb",
    "node_id": 12345,
    "property_count": 5
  }
}
```

### Field Types

The package provides strongly-typed field constructors:

```go
import "time"

logging.Info("example of all field types",
    logging.String("str", "hello"),           // string
    logging.Int("count", 42),                  // int
    logging.Int64("id", 1234567890),          // int64
    logging.Uint64("node_id", 9999999999),    // uint64
    logging.Float64("ratio", 3.14159),        // float64
    logging.Bool("enabled", true),             // bool
    logging.Duration("timeout", 5*time.Second), // time.Duration
    logging.Error(err),                        // error
    logging.Any("custom", map[string]int{"a": 1}), // any type
)
```

## Environment Configuration

Set the `LOG_LEVEL` environment variable to control the default logger's level:

```bash
export LOG_LEVEL=DEBUG
./server  # Will log DEBUG and above

export LOG_LEVEL=WARN
./server  # Will only log WARN and ERROR
```

Supported values: `DEBUG`, `INFO`, `WARN`, `WARNING`, `ERROR` (case-insensitive)

## Use Cases

### API Request Logging

```go
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
    start := time.Now()

    requestLogger := logging.With(
        logging.String("method", r.Method),
        logging.String("path", r.URL.Path),
        logging.String("remote_addr", r.RemoteAddr),
    )

    requestLogger.Info("request started")

    // ... handle request ...

    requestLogger.Info("request completed",
        logging.Duration("duration", time.Since(start)),
        logging.Int("status", 200),
    )
}
```

### Error Logging

```go
node, err := storage.CreateNode(labels, properties)
if err != nil {
    logging.ErrorLog("failed to create node",
        logging.Error(err),
        logging.Any("labels", labels),
        logging.Int("property_count", len(properties)),
    )
    return err
}

logging.Info("node created successfully",
    logging.Uint64("node_id", node.ID),
    logging.Duration("duration", time.Since(start)),
)
```

### Component-Specific Logging

```go
// In storage package initialization
var storageLogger = logging.With(logging.String("component", "storage"))

func (gs *GraphStorage) CreateNode(...) {
    storageLogger.Debug("creating node",
        logging.Any("labels", labels),
        logging.Int("property_count", len(properties)),
    )

    // ... create node ...

    storageLogger.Info("node created",
        logging.Uint64("node_id", nodeID),
    )
}
```

### Cluster Events

```go
func (em *ElectionManager) becomeLeader() {
    logging.Info("became cluster leader",
        logging.Uint64("term", em.currentTerm),
        logging.Uint64("epoch", em.epoch),
        logging.Duration("election_duration", time.Since(em.electionStart)),
    )
}

func (cm *ClusterMembership) AddNode(node NodeInfo) {
    logging.Info("node added to cluster",
        logging.String("node_id", node.ID),
        logging.String("addr", node.Addr),
        logging.String("role", node.Role.String()),
        logging.Int("cluster_size", len(cm.nodes)),
    )
}
```

## Integration with Log Aggregation

### Elasticsearch/Logstash/Kibana (ELK)

Configure Filebeat or Logstash to parse the JSON logs:

```yaml
# filebeat.yml
filebeat.inputs:
- type: log
  paths:
    - /var/log/graphdb/*.log
  json.keys_under_root: true
  json.add_error_key: true
```

### Grafana Loki

```yaml
# promtail.yml
scrape_configs:
  - job_name: graphdb
    static_configs:
      - targets:
          - localhost
        labels:
          job: graphdb
          __path__: /var/log/graphdb/*.log
    pipeline_stages:
      - json:
          expressions:
            level: level
            msg: msg
```

### Querying Logs

With structured JSON logs, you can easily filter and search:

```bash
# Find all errors from storage component
jq 'select(.level == "ERROR" and .fields.component == "storage")' < logs.json

# Calculate average request duration
jq -s 'map(select(.fields.duration)) | map(.fields.duration | tonumber) | add/length' < logs.json

# Find slow queries (>1s)
jq 'select(.fields.duration and (.fields.duration | tonumber) > 1000000000)' < logs.json
```

## Performance

The logger uses buffered I/O and minimal allocations. Benchmarks on a typical workload:

```
BenchmarkJSONLogger_Info-8              500000    2847 ns/op    784 B/op    15 allocs/op
BenchmarkJSONLogger_InfoFiltered-8    50000000      32.4 ns/op     0 B/op     0 allocs/op
```

Filtered logs (below the configured level) have near-zero overhead.

## Best Practices

1. **Use appropriate log levels**:
   - DEBUG: Detailed diagnostic information
   - INFO: General informational messages
   - WARN: Warning messages for potentially harmful situations
   - ERROR: Error events that might still allow the application to continue

2. **Use child loggers for components**:
   ```go
   var componentLogger = logging.With(logging.String("component", "mycomponent"))
   ```

3. **Include context in fields, not in messages**:
   ```go
   // Good
   logging.Info("node created", logging.Uint64("node_id", id))

   // Bad
   logging.Info(fmt.Sprintf("node %d created", id))
   ```

4. **Log errors with context**:
   ```go
   logging.ErrorLog("operation failed",
       logging.Error(err),
       logging.String("operation", "create_node"),
       logging.Uint64("node_id", nodeID),
   )
   ```

5. **Use structured fields for filtering and aggregation**:
   ```go
   logging.Info("request completed",
       logging.String("method", "POST"),
       logging.String("path", "/api/nodes"),
       logging.Int("status", 201),
       logging.Duration("duration", elapsed),
   )
   ```

## Migration from Standard log Package

Replace:
```go
log.Printf("Node %d created with %d properties", nodeID, len(props))
```

With:
```go
logging.Info("node created",
    logging.Uint64("node_id", nodeID),
    logging.Int("property_count", len(props)),
)
```

This provides structured, queryable logs instead of unstructured text.
