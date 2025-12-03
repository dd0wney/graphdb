// Package middleware provides HTTP middleware components for the GraphDB API server.
//
// The middleware package is organized into separate files by concern:
//
//   - recovery.go: Panic recovery middleware
//   - logging.go: Request logging middleware
//   - cors.go: Cross-Origin Resource Sharing (CORS) middleware
//   - security_headers.go: Security headers middleware (XSS, clickjacking, etc.)
//   - body_limit.go: Request body size limiting middleware
//   - request_id.go: Request ID generation and tracking middleware
//   - ratelimit.go: Rate limiting middleware with token bucket algorithm
//   - input_validation.go: Input validation and sanitization middleware
//   - metrics.go: HTTP metrics collection middleware
//
// All middleware follows the standard pattern: func(http.Handler) http.Handler
// This allows easy chaining: handler = middleware1(middleware2(handler))
//
// Example usage:
//
//	mux := http.NewServeMux()
//	// ... register handlers ...
//
//	// Apply middleware chain
//	handler := middleware.PanicRecovery()(mux)
//	handler = middleware.RequestID()(handler)
//	handler = middleware.Logging(middleware.GetRequestID)(handler)
//	handler = middleware.CORS(middleware.DefaultCORSConfig())(handler)
//
//	http.ListenAndServe(":8080", handler)
package middleware
