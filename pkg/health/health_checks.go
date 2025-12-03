package health

import "time"

// Common health check functions

// SimpleCheck creates a simple health check that always returns healthy
func SimpleCheck(name string) Check {
	return Check{
		Name:        name,
		Status:      StatusHealthy,
		LastChecked: time.Now(),
	}
}

// DatabaseCheck creates a health check for database connectivity
func DatabaseCheck(pingFunc func() error) CheckFunc {
	return func() Check {
		check := Check{
			Name: "database",
		}

		if err := pingFunc(); err != nil {
			check.Status = StatusUnhealthy
			check.Message = err.Error()
		} else {
			check.Status = StatusHealthy
			check.Message = "Connected"
		}

		return check
	}
}

// ReplicationCheck creates a health check for replication status
func ReplicationCheck(getReplicationState func() (connected bool, lag int64, replicas int)) CheckFunc {
	return func() Check {
		check := Check{
			Name:    "replication",
			Details: make(map[string]any),
		}

		connected, lag, replicas := getReplicationState()

		check.Details["connected"] = connected
		check.Details["lag_lsn"] = lag
		check.Details["connected_replicas"] = replicas

		if !connected && replicas == 0 {
			// Standalone mode - healthy
			check.Status = StatusHealthy
			check.Message = "Standalone mode"
		} else if !connected {
			check.Status = StatusUnhealthy
			check.Message = "Not connected to primary"
		} else if lag > 1000 {
			check.Status = StatusDegraded
			check.Message = "High replication lag"
		} else {
			check.Status = StatusHealthy
			check.Message = "Replication healthy"
		}

		return check
	}
}

// ClusterCheck creates a health check for cluster status
func ClusterCheck(getClusterState func() (hasQuorum bool, healthyNodes, totalNodes int)) CheckFunc {
	return func() Check {
		check := Check{
			Name:    "cluster",
			Details: make(map[string]any),
		}

		hasQuorum, healthyNodes, totalNodes := getClusterState()

		check.Details["has_quorum"] = hasQuorum
		check.Details["healthy_nodes"] = healthyNodes
		check.Details["total_nodes"] = totalNodes

		if totalNodes == 0 {
			// Cluster mode not enabled
			check.Status = StatusHealthy
			check.Message = "Cluster mode disabled"
		} else if !hasQuorum {
			check.Status = StatusUnhealthy
			check.Message = "No quorum"
		} else if healthyNodes < totalNodes {
			check.Status = StatusDegraded
			check.Message = "Some nodes unhealthy"
		} else {
			check.Status = StatusHealthy
			check.Message = "Cluster healthy"
		}

		return check
	}
}

// DiskSpaceCheck creates a health check for disk space
func DiskSpaceCheck(getUsage func() (used, total uint64)) CheckFunc {
	return func() Check {
		check := Check{
			Name:    "disk_space",
			Details: make(map[string]any),
		}

		used, total := getUsage()

		usagePercent := float64(used) / float64(total) * 100

		check.Details["used_bytes"] = used
		check.Details["total_bytes"] = total
		check.Details["usage_percent"] = usagePercent

		if usagePercent > 95 {
			check.Status = StatusUnhealthy
			check.Message = "Critical disk space"
		} else if usagePercent > 80 {
			check.Status = StatusDegraded
			check.Message = "Low disk space"
		} else {
			check.Status = StatusHealthy
			check.Message = "Sufficient disk space"
		}

		return check
	}
}

// MemoryCheck creates a health check for memory usage
func MemoryCheck(getUsage func() (alloc, sys uint64)) CheckFunc {
	return func() Check {
		check := Check{
			Name:    "memory",
			Details: make(map[string]any),
		}

		alloc, sys := getUsage()

		check.Details["alloc_bytes"] = alloc
		check.Details["sys_bytes"] = sys

		// Consider degraded if allocated memory > 80% of system memory
		usagePercent := float64(alloc) / float64(sys) * 100

		if usagePercent > 90 {
			check.Status = StatusDegraded
			check.Message = "High memory usage"
		} else {
			check.Status = StatusHealthy
			check.Message = "Memory usage normal"
		}

		return check
	}
}
