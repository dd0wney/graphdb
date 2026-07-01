# Design: OpenTelemetry tracing + SLO/SLI docs (v1.1)

**Task**: `v1.1-otel-tracing` (coord seed / `ROADMAP_post_1.0.md` v1.1.0 "Validate & observe").
**Status**: approved 2026-07-01. Additive; pairs with the existing Prometheus metrics.

## Problem

The query layer already creates spans via the global tracer
(`otel.Tracer("query").Start(...)` in `pkg/query/{planner,physical_ops_call,physical_ops_scan}.go`),
but **no `TracerProvider` is configured** — the OTel SDK and exporters are absent from
`go.sum`, so those spans go to the no-op global tracer and vanish. HTTP requests create no
root span, so even if a provider existed there would be nothing for the query spans to nest
under.

The task is to make the existing tracing observable: wire a real, env-configurable
`TracerProvider` (off by default), add an HTTP root-span middleware, and document SLO/SLIs.

## Goals / non-goals

**Goals**
- Configurable `TracerProvider` (OTLP/gRPC + stdout exporters), **off by default**, zero
  overhead when disabled.
- HTTP root span per request; existing query spans nest under it (context already threads
  `r.Context()` → `ExecuteWithContext`, so propagation needs no handler changes).
- SLO/SLI documentation mapped to existing Prometheus metrics + the new traces.

**Non-goals (YAGNI)**
- Storage-op spans (`GetNode`/`CreateNode`/snapshot).
- Emitting metrics via OTel (Prometheus stays authoritative).
- Trace↔log correlation.

## Architecture

### 1. `pkg/tracing` (new package)

```
Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error)
```

- **Off by default**: `Config.Enabled` (from `GRAPHDB_TRACING_ENABLED`) false → set no global
  provider (global stays no-op), return a no-op `shutdown`. Existing spans stay free.
- **Enabled**: build an SDK `TracerProvider` with a batch span processor + the configured
  exporter; register via `otel.SetTracerProvider`; set `otel.SetTextMapPropagator`
  (W3C TraceContext + Baggage). Return a `shutdown` that flushes/stops the provider.
- **Config from env** (reuse OTel's own conventions so ops knowledge transfers):
  - `GRAPHDB_TRACING_ENABLED` (bool, master switch, default false)
  - `OTEL_TRACES_EXPORTER` = `otlp` | `console` (default `otlp` when enabled)
  - `OTEL_EXPORTER_OTLP_ENDPOINT` (OTLP target)
  - `OTEL_SERVICE_NAME` (default `graphdb`), `OTEL_TRACES_SAMPLER` (default parentbased/always)
- **Resource**: service.name, service.version (build version), host attributes.
- Dependencies added: `go.opentelemetry.io/otel/sdk`,
  `.../exporters/otlp/otlptrace/otlptracegrpc`, `.../exporters/stdout/stdouttrace`.

### 2. `tracingMiddleware` (in `pkg/api`)

Hand-rolled (~40 lines, core OTel API — matches the existing hand-rolled middleware stack;
no `otelhttp` contrib dep):
- Extract inbound context from headers via the global propagator.
- Start a server-kind root span named `"<METHOD> <route>"`; set http.method / http.target /
  http.status_code attributes.
- Inject the span context into `r.Context()` so downstream query spans nest.
- On completion set span status from the response code; `End()`.
- Slotted **outermost** in the chain in `server.go` so it wraps every other middleware.

### 3. Wire-up in `cmd/server/main.go`

`tracing.Init(ctx, tracing.ConfigFromEnv())` early in `main()`; `defer shutdown(...)` alongside
the existing graceful-shutdown defers.

### 4. Docs — `docs/OBSERVABILITY.md` (or extend `docs/MONITORING_SETUP.md`)

- SLIs: request latency p50/p95/p99, error rate, availability — mapped to existing Prometheus
  metrics (names from `pkg/api/metrics*.go`).
- Example SLOs (targets) + how to compute from the metrics.
- Tracing section: enable flags, env vars, Jaeger/Tempo quick-start, how query spans nest
  under request spans.

## Testing

- `pkg/tracing`: table tests for `ConfigFromEnv` parsing; `Init` disabled → no-op shutdown +
  global provider unchanged; `Init` enabled(stdout) → working shutdown.
- `pkg/api`: middleware test using an in-memory `tracetest.SpanRecorder` (SDK, no network) —
  assert one server span is recorded for a request, has the right name/attrs, and that a
  child span started from `r.Context()` nests under it (proves propagation).

## Risks

- **Dependency footprint**: OTLP/gRPC pulls gRPC + protobuf. Accepted (user decision); OTel
  API already direct-vendored, SDK/exporters are the net-new tree.
- **Overhead when enabled**: batch processor + sampling keep it bounded; default-off means no
  cost unless opted in.
- **Middleware ordering**: must be outermost to capture full request latency including other
  middleware; verified by placement in the `server.go` chain.
