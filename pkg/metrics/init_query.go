package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initQueryMetrics() {
	r.QueriesTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_queries_total",
			Help: "Total number of queries executed",
		},
		[]string{"query_type", "status"},
	)

	r.QueryDuration = promauto.With(r.registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "graphdb_query_duration_seconds",
			Help:    "Query execution duration in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0},
		},
		[]string{"query_type"},
	)

	r.QueryNodesScanned = promauto.With(r.registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "graphdb_query_nodes_scanned",
			Help:    "Number of nodes scanned per query",
			Buckets: []float64{10, 100, 1000, 10000, 100000},
		},
		[]string{"query_type"},
	)

	r.QueryEdgesScanned = promauto.With(r.registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "graphdb_query_edges_scanned",
			Help:    "Number of edges scanned per query",
			Buckets: []float64{10, 100, 1000, 10000, 100000},
		},
		[]string{"query_type"},
	)

	r.SlowQueries = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_slow_queries_total",
			Help: "Total number of slow queries (>1s)",
		},
		[]string{"query_type"},
	)
}
