package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initReplicationMetrics() {
	r.ReplicationLagLSN = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_replication_lag_lsn",
			Help: "Replication lag in LSN (log sequence numbers)",
		},
	)

	r.ReplicationLagSeconds = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_replication_lag_seconds",
			Help: "Replication lag in seconds",
		},
	)

	r.ReplicationThroughputBytes = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_replication_throughput_bytes_total",
			Help: "Replication throughput in bytes",
		},
		[]string{"direction"}, // sent, received
	)

	r.ReplicationConnectedReplicas = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_replication_connected_replicas",
			Help: "Number of currently connected replicas",
		},
	)

	r.ReplicationWALEntriesTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_replication_wal_entries_total",
			Help: "Total number of WAL entries replicated",
		},
		[]string{"direction"}, // sent, received
	)

	r.ReplicationHeartbeatsTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_replication_heartbeats_total",
			Help: "Total number of replication heartbeats",
		},
		[]string{"direction"}, // sent, received
	)
}
