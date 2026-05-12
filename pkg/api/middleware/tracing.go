package middleware

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// TraceMiddleware returns a middleware that instruments HTTP handlers with OpenTelemetry.
func TraceMiddleware(operation string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, operation)
	}
}
