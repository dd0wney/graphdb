package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// tracingMiddleware must (1) record a server-kind root span named "<METHOD> <path>"
// carrying the response status, and (2) inject that span into r.Context() so a
// downstream span nests under it — proving trace context propagates to handlers
// (and hence to the query executor, which already runs on r.Context()).
func TestTracingMiddleware_RecordsServerSpanAndPropagates(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(noop.NewTracerProvider()) // restore no-op; don't leak into other tests
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, child := otel.Tracer("child").Start(r.Context(), "child-op")
		child.End()
		w.WriteHeader(http.StatusCreated)
	})

	rr := httptest.NewRecorder()
	tracingMiddleware(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/nodes", nil))

	ended := sr.Ended()
	if len(ended) != 2 {
		t.Fatalf("got %d spans, want 2 (server + child)", len(ended))
	}

	var server, child sdktrace.ReadOnlySpan
	for _, s := range ended {
		switch s.Name() {
		case "GET /nodes":
			server = s
		case "child-op":
			child = s
		}
	}
	if server == nil {
		t.Fatal("no server span named \"GET /nodes\"")
	}
	if server.SpanKind() != trace.SpanKindServer {
		t.Errorf("server span kind = %v, want Server", server.SpanKind())
	}
	if got := statusAttr(server); got != 201 {
		t.Errorf("http.status_code attr = %d, want 201", got)
	}
	if child == nil {
		t.Fatal("no child span (context did not propagate)")
	}
	if child.Parent().SpanID() != server.SpanContext().SpanID() {
		t.Errorf("child parent %v != server span %v — not nested",
			child.Parent().SpanID(), server.SpanContext().SpanID())
	}
}

func statusAttr(s sdktrace.ReadOnlySpan) int64 {
	for _, kv := range s.Attributes() {
		if kv.Key == attribute.Key("http.status_code") {
			return kv.Value.AsInt64()
		}
	}
	return -1
}
