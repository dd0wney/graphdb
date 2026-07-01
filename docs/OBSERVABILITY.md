# Observability: SLOs, SLIs & Tracing

graphdb exposes three observability signals:

- **Metrics** — Prometheus at `/metrics`. Setup, dashboards, and the full metric
  catalogue live in [`MONITORING_SETUP.md`](./MONITORING_SETUP.md).
- **Traces** — OpenTelemetry (this document, [Tracing](#tracing)). **Off by default.**
- **Logs** — structured JSON (slog) to stdout.

This document defines the service-level indicators/objectives to alert on, and how to
turn on distributed tracing.

## SLIs & SLOs

SLIs are computed from the existing Prometheus metrics. The SLO targets below are
**suggested starting points** for a single-node deployment — tune them to your workload
and error budget before wiring alerts.

| SLI | Definition | Metric source | Suggested SLO |
|---|---|---|---|
| **Availability** | fraction of requests not returning 5xx | `graphdb_http_requests_total` | ≥ 99.9% over 30d |
| **Request latency** | p95 / p99 request duration | `graphdb_http_request_duration_seconds` | p95 < 100 ms, p99 < 500 ms |
| **Query latency** | p99 query execution duration | `graphdb_query_duration_seconds` | p99 < 250 ms |
| **Query error rate** | fraction of queries erroring | `graphdb_query_errors_total` vs `graphdb_active_queries` throughput | < 0.1% |
| **Saturation** | in-flight requests vs capacity | `graphdb_http_requests_in_flight` | headroom-dependent |

### PromQL

```promql
# Availability (last 5m): 1 - 5xx rate / total rate
1 - (
  sum(rate(graphdb_http_requests_total{status=~"5.."}[5m]))
  /
  sum(rate(graphdb_http_requests_total[5m]))
)

# Request latency p95 (last 5m)
histogram_quantile(0.95,
  sum(rate(graphdb_http_request_duration_seconds_bucket[5m])) by (le))

# Query latency p99 (last 5m)
histogram_quantile(0.99,
  sum(rate(graphdb_query_duration_seconds_bucket[5m])) by (le))

# Query error rate (last 5m)
sum(rate(graphdb_query_errors_total[5m]))
```

### Error budget

At a 99.9% availability SLO, the 30-day error budget is ~43m of full unavailability (or the
equivalent fraction of failed requests). Alert when the budget burns faster than the window
allows — e.g. page on a 2% budget burn in 1h (fast burn) and ticket on 5% in 6h (slow burn).

## Tracing

graphdb is instrumented with OpenTelemetry. A per-request **server root span** is created by
the API's tracing middleware, and the query executor's operator spans
(`Planner.PlanSub`, `NodeScanOperator.Open`, …) **nest under it automatically** because the
request context flows into `ExecuteWithContext`. Trace context is propagated via W3C
`traceparent`, so graphdb participates in a larger distributed trace when called by an
upstream service.

**Tracing is off by default** — no `TracerProvider` is installed and the spans cost nothing
unless you opt in.

### Enable

Set the master switch plus the standard OpenTelemetry environment variables:

| Env var | Purpose | Default |
|---|---|---|
| `GRAPHDB_TRACING_ENABLED` | master switch (`true`/`1`/`yes`/`on`) | off |
| `OTEL_TRACES_EXPORTER` | `otlp` (gRPC) or `console` (stdout) | `otlp` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector target, e.g. `localhost:4317` | SDK default |
| `OTEL_SERVICE_NAME` | resource `service.name` on every span | `graphdb` |

Notes:
- An explicit `OTEL_EXPORTER_OTLP_ENDPOINT` uses **insecure gRPC** (the local-collector /
  sidecar pattern). With no endpoint set, the OTLP exporter reads the full standard
  `OTEL_EXPORTER_OTLP_*` environment, which can configure TLS.
- Exporter init failure is **non-fatal**: the server logs and continues without tracing.

### Quick start with Jaeger

```bash
# 1. Run an all-in-one Jaeger with an OTLP/gRPC receiver on :4317
docker run --rm -p 16686:16686 -p 4317:4317 jaegertracing/all-in-one:latest

# 2. Start graphdb with tracing on
GRAPHDB_TRACING_ENABLED=true \
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
OTEL_SERVICE_NAME=graphdb \
  ./graphdb-server

# 3. Send a query, then open the Jaeger UI at http://localhost:16686 and select
#    service "graphdb". Each POST /query trace shows the request span with the
#    planner + operator spans nested inside.
```

For dev without a collector, `OTEL_TRACES_EXPORTER=console GRAPHDB_TRACING_ENABLED=true`
prints spans to stdout.

### What's traced

- **Server root span** per request: `"<METHOD> <path>"`, kind `server`, attributes
  `http.method`, `http.target`, `http.status_code` (status ≥ 500 marks the span an error).
- **Query executor spans**: planner and physical-operator `Open` calls.

Not yet traced (future work): storage read/write operations. Metrics remain
Prometheus-native; tracing does not replace them.
