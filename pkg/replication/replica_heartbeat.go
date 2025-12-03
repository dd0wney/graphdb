package replication

import (
	"fmt"
	"log"
	"time"
)

// handleHeartbeatMessage processes a heartbeat message from primary
func (rn *ReplicaNode) handleHeartbeatMessage(msg *Message) error {
	var hb HeartbeatMessage
	if err := msg.Decode(&hb); err != nil {
		return err
	}

	// Track the heartbeat sequence number
	rn.heartbeatSeqMu.Lock()
	rn.lastReceivedHeartbeatSeq = hb.Sequence
	rn.heartbeatSeqMu.Unlock()

	// Update cluster state if enabled
	if rn.clusterEnabled {
		rn.updateClusterHeartbeatState(hb)
	}

	// Echo back the heartbeat with the same sequence
	return rn.sendHeartbeatResponse(hb.Sequence)
}

// updateClusterHeartbeatState updates cluster state based on received heartbeat
func (rn *ReplicaNode) updateClusterHeartbeatState(hb HeartbeatMessage) {
	// Update heartbeat time for health monitoring
	rn.heartbeatTimeMu.Lock()
	rn.lastHeartbeatTime = time.Now()
	rn.heartbeatTimeMu.Unlock()

	// Reset election timer - primary is alive
	rn.electionMgr.ResetElectionTimer()

	// Update primary node info in membership
	if rn.primaryID != "" {
		if err := rn.membership.UpdateNodeHeartbeat(rn.primaryID, hb.Sequence, hb.Epoch, hb.Term); err != nil {
			log.Printf("Warning: Failed to update node heartbeat in membership: %v", err)
		}
	}
}

// sendHeartbeatResponse echoes a heartbeat back to the primary
func (rn *ReplicaNode) sendHeartbeatResponse(sequence uint64) error {
	stats := rn.storage.GetStatistics()

	// Get last applied LSN under lock
	rn.connectedMu.RLock()
	lastAppliedLSN := rn.lastAppliedLSN
	rn.connectedMu.RUnlock()

	hb := HeartbeatMessage{
		From:       rn.replicaID,
		Sequence:   sequence, // Echo back the same sequence
		CurrentLSN: lastAppliedLSN,
		NodeCount:  stats.NodeCount,
		EdgeCount:  stats.EdgeCount,
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		return err
	}

	// Get encoder under lock to avoid race with disconnect
	rn.connectedMu.RLock()
	encoder := rn.encoder
	rn.connectedMu.RUnlock()

	if encoder == nil {
		return nil
	}

	return encoder.Encode(msg)
}

// sendHeartbeats sends periodic heartbeats to primary
// Note: Replicas don't initiate sequence numbers, they only echo them back
// when responding to primary heartbeats
func (rn *ReplicaNode) sendHeartbeats() {
	defer rn.wg.Done()
	ticker := time.NewTicker(rn.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rn.stopCh:
			return
		case <-ticker.C:
			if !rn.isConnected() {
				return
			}

			if err := rn.sendPeriodicHeartbeat(); err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
				return
			}
		}
	}
}

// sendPeriodicHeartbeat sends a single periodic heartbeat
func (rn *ReplicaNode) sendPeriodicHeartbeat() error {
	// Get current heartbeat sequence to include in periodic heartbeat
	rn.heartbeatSeqMu.Lock()
	currentSeq := rn.lastReceivedHeartbeatSeq
	rn.heartbeatSeqMu.Unlock()

	// Get last applied LSN under lock
	rn.connectedMu.RLock()
	lastAppliedLSN := rn.lastAppliedLSN
	rn.connectedMu.RUnlock()

	stats := rn.storage.GetStatistics()
	hb := HeartbeatMessage{
		From:       rn.replicaID,
		Sequence:   currentSeq, // Echo back latest received sequence
		CurrentLSN: lastAppliedLSN,
		NodeCount:  stats.NodeCount,
		EdgeCount:  stats.EdgeCount,
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		return fmt.Errorf("failed to create heartbeat message: %w", err)
	}

	// Get encoder under lock to avoid race with disconnect
	rn.connectedMu.RLock()
	encoder := rn.encoder
	rn.connectedMu.RUnlock()

	if encoder == nil {
		return nil
	}

	return encoder.Encode(msg)
}

// monitorPrimaryHealth monitors primary heartbeats and triggers elections on failure
//
// Concurrent Safety:
// 1. Runs in dedicated goroutine started by Start()
// 2. Uses heartbeatTimeMu for safe access to lastHeartbeatTime
// 3. Checks stopCh for clean shutdown
// 4. Election triggering is thread-safe (handled by ElectionManager)
//
// Election Triggering Logic:
// 1. Checks heartbeat timeout every second
// 2. If no heartbeat received within election timeout, assumes primary is dead
// 3. Triggers election via ElectionManager.StartElection()
// 4. ElectionManager handles the actual election process (voting, quorum, etc.)
func (rn *ReplicaNode) monitorPrimaryHealth() {
	defer rn.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	log.Printf("Primary health monitor started for replica %s", rn.replicaID)

	for {
		select {
		case <-rn.stopCh:
			log.Printf("Primary health monitor stopped")
			return

		case <-ticker.C:
			rn.checkPrimaryHealth()
		}
	}
}

// checkPrimaryHealth checks if primary is still alive
func (rn *ReplicaNode) checkPrimaryHealth() {
	// Check if we're connected to a primary
	if !rn.isConnected() {
		// Not connected, election manager will handle timeouts
		return
	}

	// Get time since last heartbeat
	rn.heartbeatTimeMu.Lock()
	lastHeartbeat := rn.lastHeartbeatTime
	rn.heartbeatTimeMu.Unlock()

	// If we haven't initialized lastHeartbeatTime yet, skip check
	if lastHeartbeat.IsZero() {
		return
	}

	timeSinceHeartbeat := time.Since(lastHeartbeat)

	// Get election timeout from config (3x heartbeat interval)
	electionTimeout := rn.config.HeartbeatInterval * 3

	// Check if primary appears to be dead
	if timeSinceHeartbeat > electionTimeout {
		log.Printf("⚠️  PRIMARY FAILURE DETECTED: No heartbeat for %v (timeout: %v)",
			timeSinceHeartbeat, electionTimeout)

		// Disconnect from dead primary
		rn.disconnect()

		// The election manager will detect the timeout via its own timer
		// and trigger an election automatically
		log.Printf("Waiting for election manager to trigger election...")
	}
}
