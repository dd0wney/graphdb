package replication

import (
	"log"
	"runtime/debug"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// sendHeartbeats sends periodic heartbeats to all replicas
func (rm *ReplicationManager) sendHeartbeats() {
	defer rm.wg.Done()
	ticker := time.NewTicker(rm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rm.stopCh:
			return
		case <-ticker.C:
			rm.broadcastHeartbeat()
		}
	}
}

// broadcastHeartbeat sends heartbeat to all replicas
func (rm *ReplicationManager) broadcastHeartbeat() {
	stats := rm.storage.GetStatistics()

	// Increment heartbeat sequence atomically (monotonic, never resets, lock-free)
	currentSeq := rm.heartbeatSeqCounter.Add(1)

	// Get epoch/term if cluster enabled
	var epoch, term uint64
	if rm.clusterEnabled {
		epoch = rm.membership.GetEpoch()
		term = rm.electionMgr.GetCurrentTerm()
	}

	hb := HeartbeatMessage{
		From:       rm.primaryID,
		Sequence:   currentSeq, // Logical time - monotonically increasing
		CurrentLSN: rm.storage.GetCurrentLSN(),
		NodeCount:  stats.NodeCount,
		EdgeCount:  stats.EdgeCount,
		Epoch:      epoch, // For fencing stale primaries
		Term:       term,  // Current election term
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		log.Printf("Failed to create heartbeat message: %v", err)
		return
	}

	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	// Track connected replicas metric
	if rm.metricsRegistry != nil {
		rm.metricsRegistry.ReplicationConnectedReplicas.Set(float64(len(rm.replicas)))
	}

	for _, replica := range rm.replicas {
		select {
		case replica.sendCh <- msg:
			// Track heartbeat sent
			if rm.metricsRegistry != nil {
				rm.metricsRegistry.ReplicationHeartbeatsTotal.WithLabelValues("sent").Inc()
			}
		default:
			log.Printf("Warning: Replica %s send buffer full", replica.replicaID)
		}
	}
}

// broadcastWALEntries broadcasts WAL entries to all connected replicas
func (rm *ReplicationManager) broadcastWALEntries() {
	defer rm.wg.Done()
	for {
		select {
		case <-rm.stopCh:
			return
		case entry := <-rm.walStream:
			rm.broadcastWALEntry(entry)
		}
	}
}

// broadcastWALEntry broadcasts a single WAL entry to all replicas
func (rm *ReplicationManager) broadcastWALEntry(entry *wal.Entry) {
	// Create WAL message with proper wrapper
	walMsg := WALEntryMessage{Entry: entry}
	msg, err := NewMessage(MsgWALEntry, walMsg)
	if err != nil {
		log.Printf("Failed to create WAL message: %v", err)
		return
	}

	// Calculate entry size for throughput tracking
	entrySize := len(entry.Data)

	// Broadcast to all replicas
	rm.replicasMu.RLock()
	for _, replica := range rm.replicas {
		select {
		case replica.sendCh <- msg:
			// Track WAL entry sent and throughput
			if rm.metricsRegistry != nil {
				rm.metricsRegistry.ReplicationWALEntriesTotal.WithLabelValues("sent").Inc()
				rm.metricsRegistry.ReplicationThroughputBytes.WithLabelValues("sent").Add(float64(entrySize))
			}
		default:
			log.Printf("Warning: Replica %s send buffer full", replica.replicaID)
		}
	}
	rm.replicasMu.RUnlock()
}

// handleReplicaMessage handles a message from a replica.
// Includes panic recovery to prevent server crashes from malformed messages.
func (rm *ReplicationManager) handleReplicaMessage(replica *ReplicaConnection, msg *Message) {
	// Panic recovery - prevent crashes from malformed replica messages
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Printf("PANIC in handleReplicaMessage for replica %s: %v\n%s",
				replica.replicaID, r, stack)
			// Don't close the connection - let it recover and continue
		}
	}()

	switch msg.Type {
	case MsgHeartbeat:
		rm.handleHeartbeatResponse(replica, msg)

	case MsgAck:
		rm.handleAckMessage(replica, msg)

	case MsgError:
		rm.handleErrorMessage(replica, msg)
	}
}

// handleHeartbeatResponse handles heartbeat echo from replica
func (rm *ReplicationManager) handleHeartbeatResponse(replica *ReplicaConnection, msg *Message) {
	var hb HeartbeatMessage
	if err := msg.Decode(&hb); err == nil {
		// Replica echoed our heartbeat back - update sequence
		replica.mu.Lock()
		replica.lastResponseTime = time.Now()
		replica.lastResponseHeartbeatSeq = hb.Sequence
		replica.mu.Unlock()
	}
}

// handleAckMessage handles WAL acknowledgment from replica
func (rm *ReplicationManager) handleAckMessage(replica *ReplicaConnection, msg *Message) {
	var ack AckMessage
	if err := msg.Decode(&ack); err == nil {
		replica.mu.Lock()
		replica.lastResponseTime = time.Now()
		replica.lastAppliedLSN = ack.LastAppliedLSN
		// Track heartbeat sequence from ACK
		if ack.HeartbeatSequence > replica.lastResponseHeartbeatSeq {
			replica.lastResponseHeartbeatSeq = ack.HeartbeatSequence
		}
		replica.mu.Unlock()
	}
}

// handleErrorMessage handles error message from replica
func (rm *ReplicationManager) handleErrorMessage(replica *ReplicaConnection, msg *Message) {
	var errMsg ErrorMessage
	if err := msg.Decode(&errMsg); err == nil {
		log.Printf("Replica %s error: %s", replica.replicaID, errMsg.Message)
		if errMsg.Fatal {
			replica.stopOnce.Do(func() {
				close(replica.stopCh)
			})
		}
	}
}
