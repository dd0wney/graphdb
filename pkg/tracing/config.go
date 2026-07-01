// Package tracing wires an OpenTelemetry TracerProvider for graphdb. It is
// off by default: unless GRAPHDB_TRACING_ENABLED is set, no global provider is
// installed and the spans the query layer already creates stay no-ops.
package tracing

import "os"

// Config is the resolved tracing configuration. It is a plain value so tests
// can compare it directly and callers can construct it without env.
type Config struct {
	// Enabled is the master switch (GRAPHDB_TRACING_ENABLED). When false, Init
	// installs no provider and tracing costs nothing.
	Enabled bool
	// Exporter selects the span exporter: "otlp" (gRPC to a collector) or
	// "console" (stdout, for dev/demo). Unknown values fall back to "otlp".
	Exporter string
	// Endpoint is the OTLP target (OTEL_EXPORTER_OTLP_ENDPOINT); empty uses the
	// OTLP SDK default.
	Endpoint string
	// ServiceName tags every span's resource (OTEL_SERVICE_NAME).
	ServiceName string
}

// ConfigFromEnv resolves Config from environment variables, reusing OpenTelemetry's
// own conventions (OTEL_*) so operator knowledge transfers, with a GRAPHDB_-scoped
// master switch.
func ConfigFromEnv() Config {
	cfg := Config{
		Enabled:     isTruthy(os.Getenv("GRAPHDB_TRACING_ENABLED")),
		Exporter:    normalizeExporter(os.Getenv("OTEL_TRACES_EXPORTER")),
		Endpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName: os.Getenv("OTEL_SERVICE_NAME"),
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "graphdb"
	}
	return cfg
}

// isTruthy accepts the common affirmative spellings so operators aren't tripped
// by "1" vs "true".
func isTruthy(v string) bool {
	switch v {
	case "true", "True", "TRUE", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// normalizeExporter maps the exporter env to a known value, defaulting to "otlp".
func normalizeExporter(v string) string {
	switch v {
	case "console", "stdout":
		return "console"
	default:
		return "otlp"
	}
}
