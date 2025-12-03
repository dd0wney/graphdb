package api

import (
	"net/http"
	"runtime"
	"strconv"
	"time"
)

// metricsMiddleware tracks HTTP request metrics
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Track in-flight requests
		s.metricsRegistry.HTTPRequestsInFlight.Inc()
		defer s.metricsRegistry.HTTPRequestsInFlight.Dec()

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

		s.metricsRegistry.RecordHTTPRequest(r.Method, r.URL.Path, statusStr, duration)
		s.metricsRegistry.HTTPResponseSizeBytes.WithLabelValues(r.Method, r.URL.Path).Observe(float64(wrapper.bytesWritten))
	})
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

// updateMetricsPeriodically updates system metrics every 10 seconds
func (s *Server) updateMetricsPeriodically() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Update uptime
		s.metricsRegistry.UptimeSeconds.Set(time.Since(s.startTime).Seconds())

		// Update Go runtime metrics
		s.metricsRegistry.GoRoutines.Set(float64(runtime.NumGoroutine()))

		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		s.metricsRegistry.MemoryAllocBytes.Set(float64(m.Alloc))
		s.metricsRegistry.MemorySysBytes.Set(float64(m.Sys))

		// Update storage metrics
		stats := s.graph.GetStatistics()
		s.metricsRegistry.StorageNodesTotal.Set(float64(stats.NodeCount))
		s.metricsRegistry.StorageEdgesTotal.Set(float64(stats.EdgeCount))
	}
}
