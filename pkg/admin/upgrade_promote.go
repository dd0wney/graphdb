package admin

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
)

// PromoteToPrimary promotes this replica to primary
func (um *UpgradeManager) PromoteToPrimary(ctx context.Context, waitForSync bool, timeout time.Duration) (*PromoteResponse, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	startTime := time.Now()

	response := &PromoteResponse{
		PreviousRole: um.getCurrentRole(),
		PromotedAt:   time.Now(),
	}

	// Verify this is a replica
	if um.isPrimary {
		response.Success = false
		response.Message = "Already a primary node"
		return response, fmt.Errorf("already a primary node")
	}

	if um.replica == nil {
		response.Success = false
		response.Message = "Not configured as replica"
		return response, fmt.Errorf("not configured as replica")
	}

	// Wait for replication to sync if requested
	if waitForSync {
		syncCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		if err := um.WaitForReplicationSync(syncCtx, 100, 2); err != nil {
			response.Success = false
			response.Message = fmt.Sprintf("Failed to sync before promotion: %v", err)
			return response, err
		}
	}

	// If cluster mode is enabled, trigger an election
	if um.clusterEnabled && um.electionMgr != nil {
		log.Printf("Cluster mode enabled - triggering election for manual promotion...")

		// Trigger an election
		if err := um.electionMgr.StartElection(); err != nil {
			response.Success = false
			response.Message = fmt.Sprintf("Failed to start election: %v", err)
			return response, err
		}

		// Wait for this node to become leader (with timeout)
		electionTimeout := time.After(10 * time.Second)
		checkTicker := time.NewTicker(500 * time.Millisecond)
		defer checkTicker.Stop()

		electionStarted := time.Now()

	electionLoop:
		for {
			select {
			case <-electionTimeout:
				response.Success = false
				response.Message = "Election timeout - failed to become leader"
				return response, fmt.Errorf("election timeout after 10s")

			case <-checkTicker.C:
				if um.electionMgr.IsLeader() {
					electionDuration := time.Since(electionStarted)
					log.Printf("✅ Won election after %v - proceeding with promotion", electionDuration)
					break electionLoop
				}
				log.Printf("Waiting to become leader (election in progress)...")
			}
		}
	} else {
		log.Printf("Cluster mode disabled - performing direct promotion without election")
	}

	// Stop replica mode
	if err := um.replica.Stop(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to stop replica: %v", err)
		return response, err
	}

	// Promote to primary
	um.isPrimary = true

	// Initialize replication manager as primary
	listenAddr := fmt.Sprintf(":%d", um.config.ReplicationPort)
	config := replication.ReplicationConfig{
		ListenAddr:        listenAddr,
		HeartbeatInterval: 1 * time.Second,
		MaxReplicas:       10,
		WALBufferSize:     1000,
	}

	um.replication = replication.NewReplicationManager(config, um.storage)
	if err := um.replication.Start(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to start as primary: %v", err)
		return response, err
	}

	response.Success = true
	response.NewRole = "primary"
	response.Message = "Successfully promoted to primary"
	response.WaitedSeconds = time.Since(startTime).Seconds()

	log.Printf("Node promoted to primary (waited %.2fs)", response.WaitedSeconds)

	return response, nil
}

// StepDownToPrimary demotes primary to replica
func (um *UpgradeManager) StepDownToReplica(ctx context.Context, newPrimaryAddr string) (*StepDownResponse, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	response := &StepDownResponse{
		PreviousRole:  um.getCurrentRole(),
		SteppedDownAt: time.Now(),
	}

	// Verify this is a primary
	if !um.isPrimary {
		response.Success = false
		response.Message = "Not a primary node"
		return response, fmt.Errorf("not a primary node")
	}

	// Stop primary mode
	if err := um.replication.Stop(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to stop primary: %v", err)
		return response, err
	}

	// Demote to replica
	um.isPrimary = false

	// Initialize as replica
	config := replication.ReplicationConfig{
		PrimaryAddr:       newPrimaryAddr,
		HeartbeatInterval: 1 * time.Second,
	}

	um.replica = replication.NewReplicaNode(config, um.storage)
	if err := um.replica.Start(); err != nil {
		response.Success = false
		response.Message = fmt.Sprintf("Failed to start as replica: %v", err)
		return response, err
	}

	response.Success = true
	response.NewRole = "replica"
	response.Message = fmt.Sprintf("Successfully demoted to replica, following %s", newPrimaryAddr)

	log.Printf("Node demoted to replica, following %s", newPrimaryAddr)

	return response, nil
}
