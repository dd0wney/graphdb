package tracing

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Init installs a global OpenTelemetry TracerProvider from cfg and returns a
// shutdown func that flushes and stops it. When cfg.Enabled is false it installs
// nothing (the global provider stays a no-op) and returns a no-op shutdown, so
// tracing has zero cost unless explicitly turned on.
//
// The returned shutdown should be deferred by the caller (e.g. cmd/server) so
// buffered spans are flushed on exit.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build span exporter: %w", err)
	}
	return initProvider(ctx, cfg, sdktrace.NewBatchSpanProcessor(exporter))
}

// newExporter builds the span exporter selected by cfg.Exporter. An explicit
// OTLP endpoint uses insecure gRPC (the local-collector/sidecar pattern); with
// no endpoint the OTLP exporter reads the standard OTEL_EXPORTER_OTLP_* env,
// which can configure TLS.
func newExporter(ctx context.Context, cfg Config) (sdktrace.SpanExporter, error) {
	if cfg.Exporter == "console" {
		return stdouttrace.New()
	}
	var opts []otlptracegrpc.Option
	if cfg.Endpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint), otlptracegrpc.WithInsecure())
	}
	return otlptracegrpc.New(ctx, opts...)
}

// initProvider wires a TracerProvider around the given span processor, sets it
// global, and installs the W3C TraceContext + Baggage propagator so inbound
// trace headers are honoured. Split from Init — and taking a SpanProcessor
// rather than a SpanExporter — so tests can inject a synchronous
// tracetest.SpanRecorder instead of a batched exporter.
func initProvider(ctx context.Context, cfg Config, processor sdktrace.SpanProcessor) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", cfg.ServiceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(processor),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp.Shutdown, nil
}
