package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initHTTPMetrics() {
	r.HTTPRequestsTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	r.HTTPRequestDuration = promauto.With(r.registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "graphdb_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	r.HTTPRequestsInFlight = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_http_requests_in_flight",
			Help: "Current number of HTTP requests being processed",
		},
	)

	r.HTTPResponseSizeBytes = promauto.With(r.registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "graphdb_http_response_size_bytes",
			Help:    "HTTP response size in bytes",
			Buckets: []float64{100, 1000, 10000, 100000, 1000000},
		},
		[]string{"method", "path"},
	)
}
