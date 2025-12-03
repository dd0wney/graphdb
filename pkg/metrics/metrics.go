package metrics

import (
	"time"
)

// RecordHTTPRequest records an HTTP request with its duration
func (r *Registry) RecordHTTPRequest(method, path, status string, duration time.Duration) {
	r.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	r.HTTPRequestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())
}

// RecordStorageOperation records a storage operation
func (r *Registry) RecordStorageOperation(operation, status string, duration time.Duration) {
	r.StorageOperationsTotal.WithLabelValues(operation, status).Inc()
	r.StorageOperationDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// RecordQuery records a query execution
func (r *Registry) RecordQuery(queryType, status string, duration time.Duration, nodesScanned, edgesScanned int) {
	r.QueriesTotal.WithLabelValues(queryType, status).Inc()
	r.QueryDuration.WithLabelValues(queryType).Observe(duration.Seconds())
	r.QueryNodesScanned.WithLabelValues(queryType).Observe(float64(nodesScanned))
	r.QueryEdgesScanned.WithLabelValues(queryType).Observe(float64(edgesScanned))

	if duration > time.Second {
		r.SlowQueries.WithLabelValues(queryType).Inc()
	}
}

// UpdateClusterMetrics updates cluster-related metrics
func (r *Registry) UpdateClusterMetrics(totalNodes, healthyNodes int, hasQuorum bool, epoch, term uint64) {
	r.ClusterNodesTotal.Set(float64(totalNodes))
	r.ClusterHealthyNodesTotal.Set(float64(healthyNodes))
	if hasQuorum {
		r.ClusterHasQuorum.Set(1)
	} else {
		r.ClusterHasQuorum.Set(0)
	}
	r.ClusterEpoch.Set(float64(epoch))
	r.ClusterTerm.Set(float64(term))
}

// SetClusterRole sets the current cluster role
func (r *Registry) SetClusterRole(role string) {
	// Reset all roles
	r.ClusterRole.WithLabelValues("primary").Set(0)
	r.ClusterRole.WithLabelValues("replica").Set(0)
	r.ClusterRole.WithLabelValues("candidate").Set(0)

	// Set current role
	r.ClusterRole.WithLabelValues(role).Set(1)
}
