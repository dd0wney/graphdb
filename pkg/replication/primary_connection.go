package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

// acceptConnections accepts incoming replica connections.
// Uses a semaphore to limit concurrent connection handlers and prevent resource exhaustion.
func (rm *ReplicationManager) acceptConnections() {
	defer rm.wg.Done()

	// Semaphore to limit concurrent connection handlers
	// Allow MaxReplicas + 5 to handle connection handshakes in progress
	maxHandlers := rm.config.MaxReplicas + 5
	sem := make(chan struct{}, maxHandlers)

	for {
		select {
		case <-rm.stopCh:
			return
		default:
		}

		conn, err := rm.listener.Accept()
		if err != nil {
			select {
			case <-rm.stopCh:
				return
			default:
				log.Printf("Error accepting connection: %v", err)
				continue
			}
		}

		// Try to acquire semaphore slot (non-blocking)
		select {
		case sem <- struct{}{}:
			rm.wg.Add(1)
			go func() {
				defer func() { <-sem }() // Release semaphore when done
				rm.handleReplicaConnection(conn)
			}()
		default:
			// At capacity - reject connection immediately
			log.Printf("Connection rejected: at handler capacity (%d)", maxHandlers)
			conn.Close()
		}
	}
}

// handleReplicaConnection handles a connection from a replica
func (rm *ReplicationManager) handleReplicaConnection(conn net.Conn) {
	defer rm.wg.Done()
	defer conn.Close()

	// Set handshake timeout to prevent slow/hanging connections
	handshakeTimeout := rm.config.GetHandshakeTimeout()
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		log.Printf("Failed to set handshake deadline: %v", err)
		return
	}

	// Read handshake
	// Note: json.NewDecoder and json.NewEncoder do not return errors - they always succeed
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	handshake, err := rm.receiveHandshake(decoder, handshakeTimeout)
	if err != nil {
		log.Printf("Handshake failed: %v", err)
		return
	}

	log.Printf("Replica %s connected (last_lsn=%d, epoch=%d, term=%d)",
		handshake.ReplicaID, handshake.LastLSN, handshake.Epoch, handshake.Term)

	// Validate and respond to handshake
	if err := rm.validateAndRespondHandshake(handshake, encoder); err != nil {
		return
	}

	// Clear handshake deadline after successful handshake
	if err := conn.SetDeadline(time.Time{}); err != nil {
		log.Printf("Failed to clear handshake deadline: %v", err)
		return
	}

	// Create and register replica connection
	replica := rm.createReplicaConnection(handshake, conn)

	rm.replicasMu.Lock()
	rm.replicas[handshake.ReplicaID] = replica
	rm.replicasMu.Unlock()

	// Start sender and receiver goroutines with read/write timeouts
	rm.wg.Add(1)
	go rm.sendToReplica(replica, encoder)
	rm.receiveFromReplica(replica, decoder)

	// Clean up when done
	rm.replicasMu.Lock()
	delete(rm.replicas, handshake.ReplicaID)
	rm.replicasMu.Unlock()

	log.Printf("Replica %s disconnected", handshake.ReplicaID)
}

// receiveHandshake receives and decodes the handshake request
func (rm *ReplicationManager) receiveHandshake(decoder *json.Decoder, timeout time.Duration) (*HandshakeRequest, error) {
	var handshakeMsg Message
	if err := decoder.Decode(&handshakeMsg); err != nil {
		return nil, fmt.Errorf("failed to read handshake (timeout=%v): %w", timeout, err)
	}

	var handshake HandshakeRequest
	if err := handshakeMsg.Decode(&handshake); err != nil {
		return nil, fmt.Errorf("failed to decode handshake: %w", err)
	}

	return &handshake, nil
}

// validateAndRespondHandshake validates handshake and sends response
func (rm *ReplicationManager) validateAndRespondHandshake(handshake *HandshakeRequest, encoder *json.Encoder) error {
	// EPOCH FENCING: Check if replica has higher epoch (we are stale)
	if rm.clusterEnabled && handshake.Epoch > rm.membership.GetEpoch() {
		return rm.rejectHandshakeEpochFencing(handshake, encoder)
	}

	// Check if we can accept this replica
	rm.replicasMu.RLock()
	replicaCount := len(rm.replicas)
	rm.replicasMu.RUnlock()

	if replicaCount >= rm.config.MaxReplicas {
		return rm.rejectHandshakeMaxReplicas(encoder)
	}

	// Send acceptance response
	return rm.acceptHandshake(encoder)
}

// rejectHandshakeEpochFencing rejects handshake due to epoch fencing
func (rm *ReplicationManager) rejectHandshakeEpochFencing(handshake *HandshakeRequest, encoder *json.Encoder) error {
	log.Printf("⚠️  FENCING: Replica has higher epoch %d > %d - stepping down",
		handshake.Epoch, rm.membership.GetEpoch())

	response, err := NewMessage(MsgHandshake, HandshakeResponse{
		PrimaryID:    rm.primaryID,
		Accepted:     false,
		ErrorMessage: fmt.Sprintf("stale epoch: replica epoch %d > primary epoch %d", handshake.Epoch, rm.membership.GetEpoch()),
		Epoch:        handshake.Epoch,
		Term:         handshake.Term,
	})
	if err != nil {
		return fmt.Errorf("failed to create handshake error response: %w", err)
	}
	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("failed to send handshake error response: %w", err)
	}

	// Gracefully step down
	go rm.onBecomeFollower()
	return fmt.Errorf("epoch fencing triggered")
}

// rejectHandshakeMaxReplicas rejects handshake due to max replicas
func (rm *ReplicationManager) rejectHandshakeMaxReplicas(encoder *json.Encoder) error {
	response, err := NewMessage(MsgHandshake, HandshakeResponse{
		PrimaryID:    rm.primaryID,
		Accepted:     false,
		ErrorMessage: "max replicas reached",
	})
	if err != nil {
		return fmt.Errorf("failed to create max replicas error message: %w", err)
	}
	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("failed to send max replicas error: %w", err)
	}
	return fmt.Errorf("max replicas reached")
}

// acceptHandshake sends acceptance response
func (rm *ReplicationManager) acceptHandshake(encoder *json.Encoder) error {
	// Get current epoch/term if cluster enabled
	var epoch, term uint64
	if rm.clusterEnabled {
		epoch = rm.membership.GetEpoch()
		term = rm.electionMgr.GetCurrentTerm()
	}

	response, err := NewMessage(MsgHandshake, HandshakeResponse{
		PrimaryID:  rm.primaryID,
		CurrentLSN: rm.storage.GetCurrentLSN(),
		Version:    "1.0",
		Accepted:   true,
		Epoch:      epoch,
		Term:       term,
	})
	if err != nil {
		return fmt.Errorf("failed to create handshake response: %w", err)
	}

	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("failed to send handshake response: %w", err)
	}

	return nil
}

// createReplicaConnection creates a new replica connection
func (rm *ReplicationManager) createReplicaConnection(handshake *HandshakeRequest, conn net.Conn) *ReplicaConnection {
	return &ReplicaConnection{
		replicaID:                handshake.ReplicaID,
		conn:                     conn,
		lastResponseTime:         time.Now(), // Use primary's local monotonic time
		lastResponseHeartbeatSeq: 0,
		lastAppliedLSN:           handshake.LastLSN,
		sendCh:                   make(chan *Message, rm.config.GetSendBufferSize()),
		stopCh:                   make(chan struct{}),
	}
}

// sendToReplica sends messages to a replica
func (rm *ReplicationManager) sendToReplica(replica *ReplicaConnection, encoder *json.Encoder) {
	defer rm.wg.Done()
	for {
		select {
		case <-replica.stopCh:
			return
		case msg := <-replica.sendCh:
			if err := encoder.Encode(msg); err != nil {
				log.Printf("Failed to send to replica %s: %v", replica.replicaID, err)
				replica.stopOnce.Do(func() {
					close(replica.stopCh)
				})
				return
			}
		}
	}
}

// receiveFromReplica receives messages from a replica
func (rm *ReplicationManager) receiveFromReplica(replica *ReplicaConnection, decoder *json.Decoder) {
	for {
		select {
		case <-replica.stopCh:
			return
		default:
		}

		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			log.Printf("Failed to receive from replica %s: %v", replica.replicaID, err)
			replica.stopOnce.Do(func() {
				close(replica.stopCh)
			})
			return
		}

		rm.handleReplicaMessage(replica, &msg)
	}
}
