package middleware

import (
	"log"
	"net/http"
	"runtime/debug"
)

// PanicRecovery creates middleware that recovers from panics in HTTP handlers.
// This prevents server crashes and returns a proper error response.
// Internal details are logged but not exposed to clients.
func PanicRecovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic with stack trace (for debugging)
					stack := debug.Stack()
					log.Printf("PANIC in HTTP handler [%s %s]: %v\n%s",
						r.Method, r.URL.Path, err, stack)

					// Return generic error to client (don't expose internal details)
					http.Error(w,
						"Internal server error",
						http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
