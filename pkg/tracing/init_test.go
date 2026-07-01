package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// serviceName extracts the service.name attribute from a span's resource.
func serviceName(res *resource.Resource) string {
	for _, kv := range res.Attributes() {
		if string(kv.Key) == "service.name" {
			return kv.Value.AsString()
		}
	}
	return ""
}

// Disabled config must install no provider and hand back a no-op shutdown, so
// tracing is free unless explicitly enabled.
func TestInit_DisabledIsNoop(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{Enabled: false})
	if err != nil {
		t.Fatalf("Init(disabled) error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init(disabled) returned nil shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown returned error: %v", err)
	}
}

// initProvider must register a working global provider so a span created via
// the global otel.Tracer is recorded, carrying the configured service.name.
func TestInitProvider_ExportsSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder() // records synchronously on span End

	shutdown, err := initProvider(context.Background(),
		Config{Enabled: true, ServiceName: "svc-test"}, sr)
	if err != nil {
		t.Fatalf("initProvider error: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	_, span := otel.Tracer("test").Start(context.Background(), "op")
	span.End()

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("got %d recorded spans, want 1", len(ended))
	}
	if ended[0].Name() != "op" {
		t.Errorf("span name = %q, want %q", ended[0].Name(), "op")
	}
	if got := serviceName(ended[0].Resource()); got != "svc-test" {
		t.Errorf("resource service.name = %q, want %q", got, "svc-test")
	}
}
