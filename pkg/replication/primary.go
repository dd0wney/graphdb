package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// ReplicationManager manages replication on the primary node
type ReplicationManager struct {
	config              ReplicationConfig
	primaryID           string
	storage             *storage.GraphStorage
	replicas            map[string]*ReplicaConnection
	replicasMu          sync.RWMutex
	listener            net.Listener
	walStream           chan *wal.Entry
	stopCh              chan struct{}
	running             bool
	runningMu           sync.Mutex
	heartbeatSeqCounter uint64 // Monotonically increasing heartbeat sequence
	heartbeatSeqMu      sync.Mutex
}

// ReplicaConnection represents a connection to a replica
type ReplicaConnection struct {
	replicaID                string
	conn                     net.Conn
	lastResponseTime         time.Time // Primary's local time when last response received
	lastResponseHeartbeatSeq uint64    // Last heartbeat seq we received ACK for
	lastAppliedLSN           uint64
	sendCh                   chan *Message
	stopCh                   chan struct{}
}

// NewReplicationManager creates a new replication manager for primary
func NewReplicationManager(config ReplicationConfig, storage *storage.GraphStorage) *ReplicationManager {
	return &ReplicationManager{
		config:    config,
		primaryID: generateID(),
		storage:   storage,
		replicas:  make(map[string]*ReplicaConnection),
		walStream: make(chan *wal.Entry, config.WALBufferSize),
		stopCh:    make(chan struct{}),
	}
}

// Start starts the replication manager
func (rm *ReplicationManager) Start() error {
	rm.runningMu.Lock()
	defer rm.runningMu.Unlock()

	if rm.running {
		return fmt.Errorf("replication manager already running")
	}

	// Listen for replica connections
	listener, err := net.Listen("tcp", rm.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}
	rm.listener = listener
	rm.running = true

	// Start accepting connections
	go rm.acceptConnections()

	// Start heartbeat sender
	go rm.sendHeartbeats()

	log.Printf("Replication manager started on %s (primary_id=%s)", rm.config.ListenAddr, rm.primaryID)

	return nil
}

// Stop stops the replication manager
func (rm *ReplicationManager) Stop() error {
	rm.runningMu.Lock()
	defer rm.runningMu.Unlock()

	if !rm.running {
		return nil
	}

	close(rm.stopCh)
	rm.running = false

	// Close listener
	if rm.listener != nil {
		rm.listener.Close()
	}

	// Close all replica connections
	rm.replicasMu.Lock()
	for _, replica := range rm.replicas {
		close(replica.stopCh)
		replica.conn.Close()
	}
	rm.replicas = make(map[string]*ReplicaConnection)
	rm.replicasMu.Unlock()

	log.Printf("Replication manager stopped")

	return nil
}

// StreamWALEntry sends a WAL entry to all replicas
func (rm *ReplicationManager) StreamWALEntry(entry *wal.Entry) {
	// Check running state under lock to avoid race with Stop()
	rm.runningMu.Lock()
	if !rm.running {
		rm.runningMu.Unlock()
		return
	}
	rm.runningMu.Unlock()

	// Non-blocking send
	select {
	case rm.walStream <- entry:
	default:
		log.Printf("Warning: WAL stream buffer full, dropping entry")
	}
}

// acceptConnections accepts incoming replica connections
func (rm *ReplicationManager) acceptConnections() {
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

		go rm.handleReplicaConnection(conn)
	}
}

// handleReplicaConnection handles a connection from a replica
func (rm *ReplicationManager) handleReplicaConnection(conn net.Conn) {
	defer conn.Close()

	// Read handshake
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var handshakeMsg Message
	if err := decoder.Decode(&handshakeMsg); err != nil {
		log.Printf("Failed to read handshake: %v", err)
		return
	}

	var handshake HandshakeRequest
	if err := handshakeMsg.Decode(&handshake); err != nil {
		log.Printf("Failed to decode handshake: %v", err)
		return
	}

	log.Printf("Replica %s connected (last_lsn=%d)", handshake.ReplicaID, handshake.LastLSN)

	// Check if we can accept this replica
	rm.replicasMu.RLock()
	replicaCount := len(rm.replicas)
	rm.replicasMu.RUnlock()

	if replicaCount >= rm.config.MaxReplicas {
		response, _ := NewMessage(MsgHandshake, HandshakeResponse{
			PrimaryID:    rm.primaryID,
			Accepted:     false,
			ErrorMessage: "max replicas reached",
		})
		encoder.Encode(response)
		return
	}

	// Send handshake response
	response, _ := NewMessage(MsgHandshake, HandshakeResponse{
		PrimaryID:  rm.primaryID,
		CurrentLSN: 0, // TODO: Get from storage
		Version:    "1.0",
		Accepted:   true,
	})

	if err := encoder.Encode(response); err != nil {
		log.Printf("Failed to send handshake response: %v", err)
		return
	}

	// Create replica connection
	replica := &ReplicaConnection{
		replicaID:                handshake.ReplicaID,
		conn:                     conn,
		lastResponseTime:         time.Now(), // Use primary's local monotonic time
		lastResponseHeartbeatSeq: 0,
		lastAppliedLSN:           handshake.LastLSN,
		sendCh:                   make(chan *Message, 100),
		stopCh:                   make(chan struct{}),
	}

	rm.replicasMu.Lock()
	rm.replicas[handshake.ReplicaID] = replica
	rm.replicasMu.Unlock()

	// Start sender and receiver goroutines
	go rm.sendToReplica(replica, encoder)
	rm.receiveFromReplica(replica, decoder)

	// Clean up when done
	rm.replicasMu.Lock()
	delete(rm.replicas, handshake.ReplicaID)
	rm.replicasMu.Unlock()

	log.Printf("Replica %s disconnected", handshake.ReplicaID)
}

// sendToReplica sends messages to a replica
func (rm *ReplicationManager) sendToReplica(replica *ReplicaConnection, encoder *json.Encoder) {
	for {
		select {
		case <-replica.stopCh:
			return
		case msg := <-replica.sendCh:
			if err := encoder.Encode(msg); err != nil {
				log.Printf("Failed to send to replica %s: %v", replica.replicaID, err)
				close(replica.stopCh)
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
			close(replica.stopCh)
			return
		}

		rm.handleReplicaMessage(replica, &msg)
	}
}

// handleReplicaMessage handles a message from a replica
func (rm *ReplicationManager) handleReplicaMessage(replica *ReplicaConnection, msg *Message) {
	// Always update response time when we receive any message (uses primary's local clock)
	replica.lastResponseTime = time.Now()

	switch msg.Type {
	case MsgHeartbeat:
		var hb HeartbeatMessage
		if err := msg.Decode(&hb); err == nil {
			// Replica echoed our heartbeat back - update sequence
			replica.lastResponseHeartbeatSeq = hb.Sequence
		}

	case MsgAck:
		var ack AckMessage
		if err := msg.Decode(&ack); err == nil {
			replica.lastAppliedLSN = ack.LastAppliedLSN
			// Track heartbeat sequence from ACK
			if ack.HeartbeatSequence > replica.lastResponseHeartbeatSeq {
				replica.lastResponseHeartbeatSeq = ack.HeartbeatSequence
			}
		}

	case MsgError:
		var errMsg ErrorMessage
		if err := msg.Decode(&errMsg); err == nil {
			log.Printf("Replica %s error: %s", replica.replicaID, errMsg.Message)
			if errMsg.Fatal {
				close(replica.stopCh)
			}
		}
	}
}

// sendHeartbeats sends periodic heartbeats to all replicas
func (rm *ReplicationManager) sendHeartbeats() {
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

	// Increment heartbeat sequence (monotonic, never resets)
	rm.heartbeatSeqMu.Lock()
	rm.heartbeatSeqCounter++
	currentSeq := rm.heartbeatSeqCounter
	rm.heartbeatSeqMu.Unlock()

	hb := HeartbeatMessage{
		From:       rm.primaryID,
		Sequence:   currentSeq, // Logical time - monotonically increasing
		CurrentLSN: 0,          // TODO: Get from WAL
		NodeCount:  stats.NodeCount,
		EdgeCount:  stats.EdgeCount,
	}

	msg, _ := NewMessage(MsgHeartbeat, hb)

	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	for _, replica := range rm.replicas {
		select {
		case replica.sendCh <- msg:
		default:
			log.Printf("Warning: Replica %s send buffer full", replica.replicaID)
		}
	}
}

// GetReplicationState returns current replication state
func (rm *ReplicationManager) GetReplicationState() ReplicationState {
	rm.replicasMu.RLock()
	defer rm.replicasMu.RUnlock()

	rm.heartbeatSeqMu.Lock()
	currentSeq := rm.heartbeatSeqCounter
	rm.heartbeatSeqMu.Unlock()

	state := ReplicationState{
		IsPrimary:    true,
		PrimaryID:    rm.primaryID,
		ReplicaCount: len(rm.replicas),
		Replicas:     make([]ReplicaStatus, 0, len(rm.replicas)),
	}

	now := time.Now()
	deadThreshold := rm.config.ReplicaDeadThreshold()

	for _, replica := range rm.replicas {
		// Calculate lag using primary's local monotonic time
		lagDuration := now.Sub(replica.lastResponseTime)

		// Calculate heartbeat lag (how many heartbeats behind)
		heartbeatLag := currentSeq - replica.lastResponseHeartbeatSeq

		// Determine if replica is healthy based on heartbeat lag
		// Following ZeroMQ Paranoid Pirate pattern
		connected := heartbeatLag < uint64(deadThreshold)

		state.Replicas = append(state.Replicas, ReplicaStatus{
			ReplicaID:      replica.replicaID,
			Connected:      connected,                // Based on heartbeat lag, not TCP connection
			LastSeen:       replica.lastResponseTime, // Use primary's local time for display
			LastAppliedLSN: replica.lastAppliedLSN,
			LagMs:          lagDuration.Milliseconds(), // Time since last response
			HeartbeatLag:   heartbeatLag,               // Logical heartbeat lag
			LagDuration:    lagDuration,
		})
	}

	return state
}

// generateID generates a unique ID using UUID v4
// This ensures node identity survives network changes and prevents collisions
func generateID() string {
	return uuid.New().String()
}
