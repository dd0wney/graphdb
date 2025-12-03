package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func (r *Registry) initClusterMetrics() {
	r.ClusterNodesTotal = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_cluster_nodes_total",
			Help: "Total number of nodes in the cluster",
		},
	)

	r.ClusterHealthyNodesTotal = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_cluster_healthy_nodes_total",
			Help: "Number of healthy nodes in the cluster",
		},
	)

	r.ClusterHasQuorum = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_cluster_has_quorum",
			Help: "Whether the cluster has quorum (1=yes, 0=no)",
		},
	)

	r.ClusterElectionsTotal = promauto.With(r.registry).NewCounterVec(
		prometheus.CounterOpts{
			Name: "graphdb_cluster_elections_total",
			Help: "Total number of leader elections",
		},
		[]string{"result"}, // won, lost, timeout
	)

	r.ClusterElectionDuration = promauto.With(r.registry).NewHistogram(
		prometheus.HistogramOpts{
			Name:    "graphdb_cluster_election_duration_seconds",
			Help:    "Duration of leader elections in seconds",
			Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
	)

	r.ClusterEpoch = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_cluster_epoch",
			Help: "Current cluster epoch (generation number)",
		},
	)

	r.ClusterTerm = promauto.With(r.registry).NewGauge(
		prometheus.GaugeOpts{
			Name: "graphdb_cluster_term",
			Help: "Current election term",
		},
	)

	r.ClusterRole = promauto.With(r.registry).NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "graphdb_cluster_role",
			Help: "Node role in cluster (1 for current role, 0 otherwise)",
		},
		[]string{"role"}, // primary, replica, candidate
	)
}
