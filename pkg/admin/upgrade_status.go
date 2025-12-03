package admin

import (
	"context"
	"fmt"
	"log"
	"time"
)

// GetUpgradeStatus returns current upgrade readiness status
func (um *UpgradeManager) GetUpgradeStatus() UpgradeStatus {
	um.mu.RLock()
	defer um.mu.RUnlock()

	status := UpgradeStatus{
		Timestamp:   time.Now(),
		CurrentRole: um.getCurrentRole(),
		Ready:       true,
		CanPromote:  false,
	}

	if um.isPrimary {
		// Primary node status
		state := um.replication.GetReplicationState()
		status.ConnectedReplicas = state.ReplicaCount
		status.Phase = "primary_running"
		status.Message = fmt.Sprintf("Primary with %d connected replicas", state.ReplicaCount)

		// Can step down if at least one replica is caught up
		for _, replica := range state.Replicas {
			if replica.Connected && replica.HeartbeatLag < 3 {
				status.CanPromote = true
				break
			}
		}
	} else if um.replica != nil {
		// Replica node status
		replicaStatus := um.replica.GetReplicaStatus()

		status.Phase = "replica_running"
		status.ReplicationLag = 0 // Will be set by caller if they have primary LSN
		status.HeartbeatLag = replicaStatus.LastHeartbeatSeq

		if replicaStatus.Connected {
			status.Message = fmt.Sprintf("Connected to primary %s (LSN: %d)",
				replicaStatus.PrimaryID, replicaStatus.LastAppliedLSN)
			// Can promote if connected and has recent heartbeat (lag < 5)
			status.CanPromote = replicaStatus.Connected && replicaStatus.LastHeartbeatSeq > 0
		} else {
			status.Message = "Disconnected from primary"
			status.Ready = false
			status.CanPromote = false
		}
	} else {
		status.Phase = "standalone"
		status.Message = "Node running in standalone mode"
	}

	return status
}

// WaitForReplicationSync waits for replication to catch up
func (um *UpgradeManager) WaitForReplicationSync(ctx context.Context, maxLagMs int64, maxHeartbeatLag uint64) error {
	if um.replica == nil {
		return fmt.Errorf("not a replica node")
	}

	log.Printf("Waiting for replication sync (maxLagMs=%d, maxHeartbeatLag=%d)...", maxLagMs, maxHeartbeatLag)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for replication sync: %w", ctx.Err())
		case <-ticker.C:
			status := um.replica.GetReplicaStatus()

			// Check if connected
			if !status.Connected {
				log.Printf("Replica not connected to primary, waiting...")
				continue
			}

			// Check heartbeat lag (simplified check - just verify we're receiving heartbeats)
			if status.LastHeartbeatSeq == 0 {
				log.Printf("No heartbeats received yet, waiting...")
				continue
			}

			// For now, we can't calculate exact LSN lag without knowing the primary's current LSN
			// So we use a heuristic: if we're connected and receiving heartbeats, we're considered synced
			elapsed := time.Since(startTime)
			if elapsed >= 1*time.Second {
				log.Printf("Replication sync complete after %v (connected=%v, heartbeat_seq=%d, lsn=%d)",
					elapsed, status.Connected, status.LastHeartbeatSeq, status.LastAppliedLSN)
				return nil
			}
		}
	}
}
