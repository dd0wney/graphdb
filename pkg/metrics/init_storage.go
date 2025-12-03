package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initStorageMetrics() {
	r.StorageNodesTotal = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_storage_nodes_total",
			Help: "Total number of nodes in the graph",
		},
	)

	r.StorageEdgesTotal = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_storage_edges_total",
			Help: "Total number of edges in the graph",
		},
	)

	r.StorageOperationsTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_storage_operations_total",
			Help: "Total number of storage operations",
		},
		[]string{"operation", "status"},
	)

	r.StorageOperationDuration = promauto.With(r.registry).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "graphdb_storage_operation_duration_seconds",
			Help:    "Storage operation duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0},
		},
		[]string{"operation"},
	)

	r.StorageDiskUsageBytes = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_storage_disk_usage_bytes",
			Help: "Disk space used by storage in bytes",
		},
	)
}
