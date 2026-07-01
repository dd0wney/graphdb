package api

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracingMiddleware starts a server-kind root span per request and injects it
// into the request context so downstream spans (notably the query executor's,
// which already run on r.Context()) nest under it.
//
// It uses the GLOBAL tracer/propagator, so it is a no-op unless a real
// TracerProvider has been installed (see pkg/tracing.Init) — the tracing this
// wires is off by default. Hand-rolled rather than pulling in otelhttp, to match
// the existing hand-rolled middleware stack and avoid another dependency.
//
// Placed outermost in the chain so the span covers the full request, including
// the other middlewares.
func tracingMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer("http")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Honour an inbound trace context (distributed tracing across services).
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.target", r.URL.Path),
			),
		)
		defer span.End()

		sw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))

		span.SetAttributes(attribute.Int("http.status_code", sw.statusCode))
		if sw.statusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(sw.statusCode))
		}
	})
}
