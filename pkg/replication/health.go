package replication

import (
	"fmt"
	"sync"
	"time"
)

// HealthStatus represents the health of a node or replica
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)

// HealthCheck performs health checks on replication
type HealthCheck struct {
	mu            sync.RWMutex
	checks        map[string]*CheckResult
	thresholds    HealthThresholds
	lastCheckTime time.Time
}

// CheckResult represents the result of a health check
type CheckResult struct {
	Name      string                 `json:"name"`
	Status    HealthStatus           `json:"status"`
	Message   string                 `json:"message"`
	CheckedAt time.Time              `json:"checked_at"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// HealthThresholds defines thresholds for health checks
type HealthThresholds struct {
	MaxReplicationLag  time.Duration // Maximum acceptable replication lag
	MaxLSNDifference   uint64        // Maximum LSN difference
	HeartbeatTimeout   time.Duration // Maximum time since last heartbeat
	MinHealthyReplicas int           // Minimum number of healthy replicas
}

// DefaultHealthThresholds returns default health thresholds
func DefaultHealthThresholds() HealthThresholds {
	return HealthThresholds{
		MaxReplicationLag:  10 * time.Second,
		MaxLSNDifference:   1000,
		HeartbeatTimeout:   5 * time.Second,
		MinHealthyReplicas: 0,
	}
}

// NewHealthCheck creates a new health check
func NewHealthCheck(thresholds HealthThresholds) *HealthCheck {
	return &HealthCheck{
		checks:     make(map[string]*CheckResult),
		thresholds: thresholds,
	}
}

// CheckPrimaryHealth checks health of primary node
func (hc *HealthCheck) CheckPrimaryHealth(state ReplicationState) HealthStatus {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.lastCheckTime = time.Now()
	overallStatus := HealthStatusHealthy

	// Check 1: Replica connectivity
	healthyReplicas := 0
	degradedReplicas := 0
	unhealthyReplicas := 0

	for _, replica := range state.Replicas {
		if !replica.Connected {
			unhealthyReplicas++
			continue
		}

		// Check heartbeat timeout
		timeSinceLastSeen := time.Since(replica.LastSeen)
		if timeSinceLastSeen > hc.thresholds.HeartbeatTimeout {
			degradedReplicas++
			hc.checks[fmt.Sprintf("replica_%s_heartbeat", replica.ReplicaID)] = &CheckResult{
				Name:      fmt.Sprintf("Replica %s Heartbeat", replica.ReplicaID),
				Status:    HealthStatusDegraded,
				Message:   fmt.Sprintf("No heartbeat for %v", timeSinceLastSeen),
				CheckedAt: time.Now(),
			}
			continue
		}

		// Check replication lag
		lagLSN := state.CurrentLSN - replica.LastAppliedLSN
		if lagLSN > hc.thresholds.MaxLSNDifference {
			degradedReplicas++
			hc.checks[fmt.Sprintf("replica_%s_lag", replica.ReplicaID)] = &CheckResult{
				Name:      fmt.Sprintf("Replica %s Lag", replica.ReplicaID),
				Status:    HealthStatusDegraded,
				Message:   fmt.Sprintf("LSN lag: %d entries", lagLSN),
				CheckedAt: time.Now(),
				Details: map[string]interface{}{
					"lag_lsn": lagLSN,
				},
			}
		} else {
			healthyReplicas++
		}
	}

	// Overall replica health check
	if len(state.Replicas) > 0 {
		if healthyReplicas < hc.thresholds.MinHealthyReplicas {
			overallStatus = HealthStatusUnhealthy
			hc.checks["replica_count"] = &CheckResult{
				Name:      "Replica Count",
				Status:    HealthStatusUnhealthy,
				Message:   fmt.Sprintf("Only %d healthy replicas (min: %d)", healthyReplicas, hc.thresholds.MinHealthyReplicas),
				CheckedAt: time.Now(),
			}
		} else if degradedReplicas > 0 {
			if overallStatus == HealthStatusHealthy {
				overallStatus = HealthStatusDegraded
			}
			hc.checks["replica_health"] = &CheckResult{
				Name:      "Replica Health",
				Status:    HealthStatusDegraded,
				Message:   fmt.Sprintf("%d degraded, %d unhealthy replicas", degradedReplicas, unhealthyReplicas),
				CheckedAt: time.Now(),
			}
		}
	}

	return overallStatus
}

// CheckReplicaHealth checks health of replica node
func (hc *HealthCheck) CheckReplicaHealth(state ReplicationState, currentLSN uint64) HealthStatus {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.lastCheckTime = time.Now()
	status := HealthStatusHealthy

	// Check replication lag
	lagLSN := currentLSN - state.CurrentLSN
	if lagLSN > hc.thresholds.MaxLSNDifference {
		status = HealthStatusDegraded
		hc.checks["replication_lag"] = &CheckResult{
			Name:      "Replication Lag",
			Status:    HealthStatusDegraded,
			Message:   fmt.Sprintf("Behind primary by %d LSN entries", lagLSN),
			CheckedAt: time.Now(),
			Details: map[string]interface{}{
				"lag_lsn": lagLSN,
			},
		}
	} else {
		hc.checks["replication_lag"] = &CheckResult{
			Name:      "Replication Lag",
			Status:    HealthStatusHealthy,
			Message:   fmt.Sprintf("Lag: %d LSN entries", lagLSN),
			CheckedAt: time.Now(),
		}
	}

	return status
}

// GetAllChecks returns all check results
func (hc *HealthCheck) GetAllChecks() []*CheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	results := make([]*CheckResult, 0, len(hc.checks))
	for _, check := range hc.checks {
		results = append(results, check)
	}

	return results
}

// GetCheckResult returns a specific check result
func (hc *HealthCheck) GetCheckResult(name string) *CheckResult {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return hc.checks[name]
}

// ClearChecks clears all check results
func (hc *HealthCheck) ClearChecks() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.checks = make(map[string]*CheckResult)
}

// HealthReport contains a comprehensive health report
type HealthReport struct {
	OverallStatus HealthStatus   `json:"overall_status"`
	Checks        []*CheckResult `json:"checks"`
	CheckedAt     time.Time      `json:"checked_at"`
	Summary       string         `json:"summary"`
}

// GenerateHealthReport generates a comprehensive health report
func (hc *HealthCheck) GenerateHealthReport(overallStatus HealthStatus) HealthReport {
	checks := hc.GetAllChecks()

	summary := fmt.Sprintf("System is %s", overallStatus)
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0

	for _, check := range checks {
		switch check.Status {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		}
	}

	if degradedCount > 0 || unhealthyCount > 0 {
		summary = fmt.Sprintf("System is %s (%d healthy, %d degraded, %d unhealthy checks)",
			overallStatus, healthyCount, degradedCount, unhealthyCount)
	}

	return HealthReport{
		OverallStatus: overallStatus,
		Checks:        checks,
		CheckedAt:     hc.lastCheckTime,
		Summary:       summary,
	}
}
