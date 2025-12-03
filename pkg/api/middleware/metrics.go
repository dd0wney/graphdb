package middleware

import (
	"net/http"
	"strconv"
	"time"
)

// MetricsRecorder is an interface for recording HTTP metrics
type MetricsRecorder interface {
	RecordHTTPRequest(method, path, status string, duration time.Duration)
	RecordResponseSize(method, path string, size float64)
	IncHTTPRequestsInFlight()
	DecHTTPRequestsInFlight()
}

// metricsResponseWriter wraps http.ResponseWriter to capture status code and bytes written
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *metricsResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += n
	return n, err
}

// Metrics creates middleware that tracks HTTP request metrics.
func Metrics(recorder MetricsRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if recorder == nil {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()

			// Track in-flight requests
			recorder.IncHTTPRequestsInFlight()
			defer recorder.DecHTTPRequestsInFlight()

			// Create a response writer wrapper to capture status code and size
			wrapper := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				bytesWritten:   0,
			}

			// Process request
			next.ServeHTTP(wrapper, r)

			// Record metrics
			duration := time.Since(start)
			statusStr := strconv.Itoa(wrapper.statusCode)

			recorder.RecordHTTPRequest(r.Method, r.URL.Path, statusStr, duration)
			recorder.RecordResponseSize(r.Method, r.URL.Path, float64(wrapper.bytesWritten))
		})
	}
}
