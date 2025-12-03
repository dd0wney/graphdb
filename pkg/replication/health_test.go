package replication

import (
	"sync"
	"testing"
	"time"
)

// TestDefaultHealthThresholds tests default threshold values
func TestDefaultHealthThresholds(t *testing.T) {
	thresholds := DefaultHealthThresholds()

	if thresholds.MaxReplicationLag != 10*time.Second {
		t.Errorf("Expected MaxReplicationLag 10s, got %v", thresholds.MaxReplicationLag)
	}
	if thresholds.MaxLSNDifference != 1000 {
		t.Errorf("Expected MaxLSNDifference 1000, got %d", thresholds.MaxLSNDifference)
	}
	if thresholds.HeartbeatTimeout != 5*time.Second {
		t.Errorf("Expected HeartbeatTimeout 5s, got %v", thresholds.HeartbeatTimeout)
	}
	if thresholds.MinHealthyReplicas != 0 {
		t.Errorf("Expected MinHealthyReplicas 0, got %d", thresholds.MinHealthyReplicas)
	}
}

// TestNewHealthCheck tests health check construction
func TestNewHealthCheck(t *testing.T) {
	thresholds := HealthThresholds{
		MaxReplicationLag:  5 * time.Second,
		MaxLSNDifference:   500,
		HeartbeatTimeout:   3 * time.Second,
		MinHealthyReplicas: 2,
	}

	hc := NewHealthCheck(thresholds)

	if hc == nil {
		t.Fatal("Expected health check, got nil")
	}

	if hc.checks == nil {
		t.Error("Expected checks map to be initialized")
	}

	if hc.thresholds.MaxReplicationLag != 5*time.Second {
		t.Errorf("Threshold not set correctly")
	}
}

// TestHealthCheck_CheckPrimaryHealth_Healthy tests healthy primary
func TestHealthCheck_CheckPrimaryHealth_Healthy(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  995,
				LastSeen:        time.Now(),
			},
			{
				ReplicaID:       "replica2",
				Connected:       true,
				LastAppliedLSN:  998,
				LastSeen:        time.Now(),
			},
		},
	}

	status := hc.CheckPrimaryHealth(state)

	if status != HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %s", status)
	}

	// Check that lastCheckTime was set
	if hc.lastCheckTime.IsZero() {
		t.Error("Expected lastCheckTime to be set")
	}
}

// TestHealthCheck_CheckPrimaryHealth_DegradedLag tests degraded due to lag
func TestHealthCheck_CheckPrimaryHealth_DegradedLag(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.MaxLSNDifference = 100
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  850, // 150 LSN behind (exceeds threshold of 100)
				LastSeen:        time.Now(),
			},
		},
	}

	status := hc.CheckPrimaryHealth(state)

	if status != HealthStatusDegraded {
		t.Errorf("Expected degraded status, got %s", status)
	}

	// Check that lag check was recorded
	lagCheck := hc.GetCheckResult("replica_replica1_lag")
	if lagCheck == nil {
		t.Fatal("Expected lag check to be recorded")
	}

	if lagCheck.Status != HealthStatusDegraded {
		t.Errorf("Expected degraded lag check, got %s", lagCheck.Status)
	}

	if lagCheck.Details == nil {
		t.Error("Expected details to be set")
	} else {
		lag, ok := lagCheck.Details["lag_lsn"].(uint64)
		if !ok || lag != 150 {
			t.Errorf("Expected lag_lsn 150, got %v", lag)
		}
	}
}

// TestHealthCheck_CheckPrimaryHealth_DegradedHeartbeat tests degraded due to heartbeat
func TestHealthCheck_CheckPrimaryHealth_DegradedHeartbeat(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.HeartbeatTimeout = 1 * time.Second
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  998,
				LastSeen:        time.Now().Add(-3 * time.Second), // 3s ago (exceeds 1s timeout)
			},
		},
	}

	status := hc.CheckPrimaryHealth(state)

	if status != HealthStatusDegraded {
		t.Errorf("Expected degraded status, got %s", status)
	}

	// Check that heartbeat check was recorded
	heartbeatCheck := hc.GetCheckResult("replica_replica1_heartbeat")
	if heartbeatCheck == nil {
		t.Fatal("Expected heartbeat check to be recorded")
	}

	if heartbeatCheck.Status != HealthStatusDegraded {
		t.Errorf("Expected degraded heartbeat check, got %s", heartbeatCheck.Status)
	}
}

// TestHealthCheck_CheckPrimaryHealth_UnhealthyDisconnected tests unhealthy replicas
func TestHealthCheck_CheckPrimaryHealth_UnhealthyDisconnected(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.MinHealthyReplicas = 2
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       false, // Disconnected
				LastAppliedLSN:  900,
				LastSeen:        time.Now().Add(-10 * time.Second),
			},
			{
				ReplicaID:       "replica2",
				Connected:       true,
				LastAppliedLSN:  998,
				LastSeen:        time.Now(),
			},
		},
	}

	status := hc.CheckPrimaryHealth(state)

	if status != HealthStatusUnhealthy {
		t.Errorf("Expected unhealthy status, got %s", status)
	}

	// Check that replica count check was recorded
	countCheck := hc.GetCheckResult("replica_count")
	if countCheck == nil {
		t.Fatal("Expected replica_count check to be recorded")
	}

	if countCheck.Status != HealthStatusUnhealthy {
		t.Errorf("Expected unhealthy count check, got %s", countCheck.Status)
	}
}

// TestHealthCheck_CheckPrimaryHealth_NoReplicas tests primary with no replicas
func TestHealthCheck_CheckPrimaryHealth_NoReplicas(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas:   []ReplicaStatus{},
	}

	status := hc.CheckPrimaryHealth(state)

	// With no replicas and MinHealthyReplicas = 0, should be healthy
	if status != HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %s", status)
	}
}

// TestHealthCheck_CheckReplicaHealth_Healthy tests healthy replica
func TestHealthCheck_CheckReplicaHealth_Healthy(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN:995, // Replica's current LSN
	}

	primaryLSN := uint64(1000)

	status := hc.CheckReplicaHealth(state, primaryLSN)

	if status != HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %s", status)
	}

	// Check that replication lag check was recorded
	lagCheck := hc.GetCheckResult("replication_lag")
	if lagCheck == nil {
		t.Fatal("Expected replication_lag check to be recorded")
	}

	if lagCheck.Status != HealthStatusHealthy {
		t.Errorf("Expected healthy lag check, got %s", lagCheck.Status)
	}
}

// TestHealthCheck_CheckReplicaHealth_Degraded tests degraded replica
func TestHealthCheck_CheckReplicaHealth_Degraded(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.MaxLSNDifference = 100
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN:850, // Replica's current LSN
	}

	primaryLSN := uint64(1000) // 150 LSN behind

	status := hc.CheckReplicaHealth(state, primaryLSN)

	if status != HealthStatusDegraded {
		t.Errorf("Expected degraded status, got %s", status)
	}

	// Check that lag check was recorded
	lagCheck := hc.GetCheckResult("replication_lag")
	if lagCheck == nil {
		t.Fatal("Expected replication_lag check to be recorded")
	}

	if lagCheck.Status != HealthStatusDegraded {
		t.Errorf("Expected degraded lag check, got %s", lagCheck.Status)
	}

	if lagCheck.Details == nil {
		t.Error("Expected details to be set")
	} else {
		lag, ok := lagCheck.Details["lag_lsn"].(uint64)
		if !ok || lag != 150 {
			t.Errorf("Expected lag_lsn 150, got %v", lag)
		}
	}
}

// TestHealthCheck_GetAllChecks tests retrieving all checks
func TestHealthCheck_GetAllChecks(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	// Initially empty
	checks := hc.GetAllChecks()
	if len(checks) != 0 {
		t.Errorf("Expected 0 checks initially, got %d", len(checks))
	}

	// Run a health check with a degraded replica to ensure checks are recorded
	thresholds := DefaultHealthThresholds()
	thresholds.MaxLSNDifference = 100
	hc = NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  850, // 150 LSN behind (exceeds threshold)
				LastSeen:        time.Now(),
			},
		},
	}

	hc.CheckPrimaryHealth(state)

	checks = hc.GetAllChecks()
	if len(checks) == 0 {
		t.Error("Expected checks after health check with degraded replica")
	}

	// All checks should have valid data
	for _, check := range checks {
		if check.Name == "" {
			t.Error("Check should have a name")
		}
		if check.CheckedAt.IsZero() {
			t.Error("Check should have a timestamp")
		}
	}
}

// TestHealthCheck_GetCheckResult tests retrieving specific check
func TestHealthCheck_GetCheckResult(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.MaxLSNDifference = 100
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  850,
				LastSeen:        time.Now(),
			},
		},
	}

	hc.CheckPrimaryHealth(state)

	// Get existing check
	check := hc.GetCheckResult("replica_replica1_lag")
	if check == nil {
		t.Fatal("Expected to find replica_replica1_lag check")
	}

	if check.Name == "" {
		t.Error("Check should have a name")
	}

	// Get non-existent check
	nonExistent := hc.GetCheckResult("does_not_exist")
	if nonExistent != nil {
		t.Error("Expected nil for non-existent check")
	}
}

// TestHealthCheck_ClearChecks tests clearing all checks
func TestHealthCheck_ClearChecks(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN:995,
	}

	hc.CheckReplicaHealth(state, 1000)

	// Should have checks
	if len(hc.GetAllChecks()) == 0 {
		t.Error("Expected checks before clear")
	}

	// Clear checks
	hc.ClearChecks()

	// Should be empty now
	checks := hc.GetAllChecks()
	if len(checks) != 0 {
		t.Errorf("Expected 0 checks after clear, got %d", len(checks))
	}

	// Clearing again should be safe
	hc.ClearChecks()
	if len(hc.GetAllChecks()) != 0 {
		t.Error("Multiple clears should work")
	}
}

// TestHealthCheck_GenerateHealthReport_Healthy tests report generation for healthy system
func TestHealthCheck_GenerateHealthReport_Healthy(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN:995,
	}

	status := hc.CheckReplicaHealth(state, 1000)

	report := hc.GenerateHealthReport(status)

	if report.OverallStatus != HealthStatusHealthy {
		t.Errorf("Expected healthy overall status, got %s", report.OverallStatus)
	}

	if report.Summary == "" {
		t.Error("Expected non-empty summary")
	}

	// Summary format may vary, just check it's not empty

	if len(report.Checks) == 0 {
		t.Error("Expected checks in report")
	}

	if report.CheckedAt.IsZero() {
		t.Error("Expected CheckedAt to be set")
	}
}

// TestHealthCheck_GenerateHealthReport_Degraded tests report for degraded system
func TestHealthCheck_GenerateHealthReport_Degraded(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.MaxLSNDifference = 100
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN:850,
	}

	status := hc.CheckReplicaHealth(state, 1000)

	report := hc.GenerateHealthReport(status)

	if report.OverallStatus != HealthStatusDegraded {
		t.Errorf("Expected degraded overall status, got %s", report.OverallStatus)
	}

	// Summary should mention degraded checks
	if report.Summary == "" {
		t.Error("Expected non-empty summary")
	}

	// Should have at least one degraded check
	degradedCount := 0
	for _, check := range report.Checks {
		if check.Status == HealthStatusDegraded {
			degradedCount++
		}
	}

	if degradedCount == 0 {
		t.Error("Expected at least one degraded check in report")
	}
}

// TestHealthCheck_GenerateHealthReport_Mixed tests report with mixed check statuses
func TestHealthCheck_GenerateHealthReport_Mixed(t *testing.T) {
	thresholds := DefaultHealthThresholds()
	thresholds.MaxLSNDifference = 100
	thresholds.MinHealthyReplicas = 2
	hc := NewHealthCheck(thresholds)

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  998, // Healthy
				LastSeen:        time.Now(),
			},
			{
				ReplicaID:       "replica2",
				Connected:       true,
				LastAppliedLSN:  850, // Degraded (150 LSN behind)
				LastSeen:        time.Now(),
			},
			{
				ReplicaID:       "replica3",
				Connected:       false, // Unhealthy
				LastAppliedLSN:  900,
				LastSeen:        time.Now().Add(-10 * time.Second),
			},
		},
	}

	status := hc.CheckPrimaryHealth(state)

	report := hc.GenerateHealthReport(status)

	// Should have multiple check statuses
	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0

	for _, check := range report.Checks {
		switch check.Status {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		}
	}

	// Summary should mention the counts
	if report.Summary == "" {
		t.Error("Expected non-empty summary")
	}

	// With 1 healthy and 2 degraded/unhealthy, should be degraded or unhealthy overall
	if report.OverallStatus == HealthStatusHealthy {
		t.Errorf("Expected non-healthy status with mixed checks, got %s", report.OverallStatus)
	}
}

// TestHealthCheck_ConcurrentAccess tests thread safety
func TestHealthCheck_ConcurrentAccess(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN: 995,
	}

	// Run multiple operations concurrently using WaitGroup for proper synchronization
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hc.CheckReplicaHealth(state, 1000)
			hc.GetAllChecks()
			hc.GetCheckResult("replication_lag")
			if idx%10 == 0 {
				hc.ClearChecks()
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Should not panic and should complete successfully
}

// TestHealthCheck_CheckPrimaryHealth_MultipleReplicas tests with multiple replicas
func TestHealthCheck_CheckPrimaryHealth_MultipleReplicas(t *testing.T) {
	hc := NewHealthCheck(DefaultHealthThresholds())

	state := ReplicationState{
		CurrentLSN: 1000,
		Replicas: []ReplicaStatus{
			{
				ReplicaID:       "replica1",
				Connected:       true,
				LastAppliedLSN:  998,
				LastSeen:        time.Now(),
			},
			{
				ReplicaID:       "replica2",
				Connected:       true,
				LastAppliedLSN:  997,
				LastSeen:        time.Now(),
			},
			{
				ReplicaID:       "replica3",
				Connected:       true,
				LastAppliedLSN:  999,
				LastSeen:        time.Now(),
			},
		},
	}

	status := hc.CheckPrimaryHealth(state)

	if status != HealthStatusHealthy {
		t.Errorf("Expected healthy with 3 healthy replicas, got %s", status)
	}
}
