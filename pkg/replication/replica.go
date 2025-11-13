package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// ReplicaNode represents a replica instance
type ReplicaNode struct {
	config                   ReplicationConfig
	replicaID                string
	storage                  *storage.GraphStorage
	conn                     net.Conn
	encoder                  *json.Encoder
	decoder                  *json.Decoder
	lastAppliedLSN           uint64
	primaryID                string
	connected                bool
	connectedMu              sync.RWMutex
	stopCh                   chan struct{}
	running                  bool
	runningMu                sync.Mutex
	lastReceivedHeartbeatSeq uint64 // Last heartbeat sequence received from primary
	heartbeatSeqMu           sync.Mutex
}

// ReplicaStatusInfo contains detailed status information about this replica
type ReplicaStatusInfo struct {
	ReplicaID        string    `json:"replica_id"`
	PrimaryID        string    `json:"primary_id"`
	Connected        bool      `json:"connected"`
	LastAppliedLSN   uint64    `json:"last_applied_lsn"`
	LastHeartbeatSeq uint64    `json:"last_heartbeat_seq"`
	Timestamp        time.Time `json:"timestamp"`
}

// NewReplicaNode creates a new replica node
func NewReplicaNode(config ReplicationConfig, storage *storage.GraphStorage) *ReplicaNode {
	if config.ReplicaID == "" {
		config.ReplicaID = generateID()
	}

	return &ReplicaNode{
		config:    config,
		replicaID: config.ReplicaID,
		storage:   storage,
		stopCh:    make(chan struct{}),
	}
}

// Start starts the replica node
func (rn *ReplicaNode) Start() error {
	rn.runningMu.Lock()
	defer rn.runningMu.Unlock()

	if rn.running {
		return fmt.Errorf("replica already running")
	}

	rn.running = true

	// Start connection manager
	go rn.connectionManager()

	log.Printf("Replica node started (replica_id=%s)", rn.replicaID)

	return nil
}

// Stop stops the replica node
func (rn *ReplicaNode) Stop() error {
	rn.runningMu.Lock()
	defer rn.runningMu.Unlock()

	if !rn.running {
		return nil
	}

	close(rn.stopCh)
	rn.running = false

	rn.disconnect()

	log.Printf("Replica node stopped")

	return nil
}

// connectionManager manages connection to primary
func (rn *ReplicaNode) connectionManager() {
	for {
		select {
		case <-rn.stopCh:
			return
		default:
		}

		// Connect to primary
		if err := rn.connectToPrimary(); err != nil {
			log.Printf("Failed to connect to primary: %v", err)

			select {
			case <-rn.stopCh:
				return
			case <-time.After(rn.config.ReconnectDelay):
				continue
			}
		}

		// Receive and apply WAL entries
		rn.receiveFromPrimary()

		// Disconnected, wait before reconnecting
		rn.disconnect()

		select {
		case <-rn.stopCh:
			return
		case <-time.After(rn.config.ReconnectDelay):
		}
	}
}

// connectToPrimary establishes connection to primary
func (rn *ReplicaNode) connectToPrimary() error {
	log.Printf("Connecting to primary at %s...", rn.config.PrimaryAddr)

	conn, err := net.Dial("tcp", rn.config.PrimaryAddr)
	if err != nil {
		return fmt.Errorf("failed to dial primary: %w", err)
	}

	rn.conn = conn
	rn.encoder = json.NewEncoder(conn)
	rn.decoder = json.NewDecoder(conn)

	// Send handshake
	handshake := HandshakeRequest{
		ReplicaID:    rn.replicaID,
		LastLSN:      rn.lastAppliedLSN,
		Version:      "1.0",
		Capabilities: []string{"wal-streaming"},
	}

	msg, err := NewMessage(MsgHandshake, handshake)
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to create handshake: %w", err)
	}

	if err := rn.encoder.Encode(msg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	// Receive handshake response
	var responseMsg Message
	if err := rn.decoder.Decode(&responseMsg); err != nil {
		conn.Close()
		return fmt.Errorf("failed to receive handshake response: %w", err)
	}

	var response HandshakeResponse
	if err := responseMsg.Decode(&response); err != nil {
		conn.Close()
		return fmt.Errorf("failed to decode handshake response: %w", err)
	}

	if !response.Accepted {
		conn.Close()
		return fmt.Errorf("handshake rejected: %s", response.ErrorMessage)
	}

	rn.primaryID = response.PrimaryID
	rn.setConnected(true)

	log.Printf("Connected to primary %s (current_lsn=%d)", rn.primaryID, response.CurrentLSN)

	// Start heartbeat sender
	go rn.sendHeartbeats()

	return nil
}

// disconnect closes connection to primary
func (rn *ReplicaNode) disconnect() {
	rn.setConnected(false)

	if rn.conn != nil {
		rn.conn.Close()
		rn.conn = nil
	}
}

// receiveFromPrimary receives and applies WAL entries from primary
func (rn *ReplicaNode) receiveFromPrimary() {
	for {
		select {
		case <-rn.stopCh:
			return
		default:
		}

		var msg Message
		if err := rn.decoder.Decode(&msg); err != nil {
			log.Printf("Error receiving from primary: %v", err)
			return
		}

		if err := rn.handleMessage(&msg); err != nil {
			log.Printf("Error handling message: %v", err)
			// Continue processing, don't disconnect on application errors
		}
	}
}

// handleMessage handles a message from primary
func (rn *ReplicaNode) handleMessage(msg *Message) error {
	switch msg.Type {
	case MsgHeartbeat:
		var hb HeartbeatMessage
		if err := msg.Decode(&hb); err != nil {
			return err
		}
		// Track the heartbeat sequence number
		rn.heartbeatSeqMu.Lock()
		rn.lastReceivedHeartbeatSeq = hb.Sequence
		rn.heartbeatSeqMu.Unlock()

		// Echo back the heartbeat with the same sequence
		return rn.sendHeartbeatResponse(hb.Sequence)

	case MsgWALEntry:
		var walMsg WALEntryMessage
		if err := msg.Decode(&walMsg); err != nil {
			return err
		}
		return rn.applyWALEntry(walMsg.Entry)

	case MsgSnapshot:
		var snapMsg SnapshotMessage
		if err := msg.Decode(&snapMsg); err != nil {
			return err
		}
		log.Printf("Snapshot message received: %s", snapMsg.SnapshotID)
		// TODO: Handle snapshot transfer
		return nil

	case MsgError:
		var errMsg ErrorMessage
		if err := msg.Decode(&errMsg); err != nil {
			return err
		}
		log.Printf("Error from primary: %s", errMsg.Message)
		return fmt.Errorf("primary error: %s", errMsg.Message)

	default:
		log.Printf("Unknown message type: %d", msg.Type)
		return nil
	}
}

// applyWALEntry applies a WAL entry to local storage
func (rn *ReplicaNode) applyWALEntry(entry *wal.Entry) error {
	// Apply the entry to storage
	switch entry.OpType {
	case wal.OpCreateNode:
		var node storage.Node
		if err := json.Unmarshal(entry.Data, &node); err != nil {
			return fmt.Errorf("failed to unmarshal node: %w", err)
		}

		// Create node in storage (without WAL logging to avoid infinite loop)
		_, err := rn.storage.CreateNode(node.Labels, node.Properties)
		if err != nil {
			return fmt.Errorf("failed to create node: %w", err)
		}

	case wal.OpCreateEdge:
		var edge storage.Edge
		if err := json.Unmarshal(entry.Data, &edge); err != nil {
			return fmt.Errorf("failed to unmarshal edge: %w", err)
		}

		// Create edge in storage
		_, err := rn.storage.CreateEdge(
			edge.FromNodeID,
			edge.ToNodeID,
			edge.Type,
			edge.Properties,
			edge.Weight,
		)
		if err != nil {
			return fmt.Errorf("failed to create edge: %w", err)
		}

	default:
		log.Printf("Unknown WAL op type: %d", entry.OpType)
	}

	// Update last applied LSN
	rn.lastAppliedLSN = entry.LSN

	// Send acknowledgment with current heartbeat sequence
	rn.heartbeatSeqMu.Lock()
	currentSeq := rn.lastReceivedHeartbeatSeq
	rn.heartbeatSeqMu.Unlock()

	ack := AckMessage{
		LastAppliedLSN:    rn.lastAppliedLSN,
		ReplicaID:         rn.replicaID,
		HeartbeatSequence: currentSeq, // ACK the latest heartbeat we've seen
	}

	ackMsg, _ := NewMessage(MsgAck, ack)
	return rn.encoder.Encode(ackMsg)
}

// sendHeartbeatResponse echoes a heartbeat back to the primary
func (rn *ReplicaNode) sendHeartbeatResponse(sequence uint64) error {
	stats := rn.storage.GetStatistics()
	hb := HeartbeatMessage{
		From:       rn.replicaID,
		Sequence:   sequence, // Echo back the same sequence
		CurrentLSN: rn.lastAppliedLSN,
		NodeCount:  stats.NodeCount,
		EdgeCount:  stats.EdgeCount,
	}

	msg, err := NewMessage(MsgHeartbeat, hb)
	if err != nil {
		return err
	}

	return rn.encoder.Encode(msg)
}

// sendHeartbeats sends periodic heartbeats to primary
// Note: Replicas don't initiate sequence numbers, they only echo them back
// when responding to primary heartbeats
func (rn *ReplicaNode) sendHeartbeats() {
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

			// Get current heartbeat sequence to include in periodic heartbeat
			rn.heartbeatSeqMu.Lock()
			currentSeq := rn.lastReceivedHeartbeatSeq
			rn.heartbeatSeqMu.Unlock()

			stats := rn.storage.GetStatistics()
			hb := HeartbeatMessage{
				From:       rn.replicaID,
				Sequence:   currentSeq, // Echo back latest received sequence
				CurrentLSN: rn.lastAppliedLSN,
				NodeCount:  stats.NodeCount,
				EdgeCount:  stats.EdgeCount,
			}

			msg, _ := NewMessage(MsgHeartbeat, hb)
			if err := rn.encoder.Encode(msg); err != nil {
				log.Printf("Failed to send heartbeat: %v", err)
				return
			}
		}
	}
}

// GetReplicationState returns current replication state
func (rn *ReplicaNode) GetReplicationState() ReplicationState {
	rn.connectedMu.RLock()
	defer rn.connectedMu.RUnlock()

	return ReplicationState{
		IsPrimary:  false,
		PrimaryID:  rn.primaryID,
		CurrentLSN: rn.lastAppliedLSN,
	}
}

// isConnected returns connection status
func (rn *ReplicaNode) isConnected() bool {
	rn.connectedMu.RLock()
	defer rn.connectedMu.RUnlock()
	return rn.connected
}

// setConnected sets connection status
func (rn *ReplicaNode) setConnected(connected bool) {
	rn.connectedMu.Lock()
	defer rn.connectedMu.Unlock()
	rn.connected = connected
}

// GetReplicaStatus returns detailed status information about this replica
func (rn *ReplicaNode) GetReplicaStatus() ReplicaStatusInfo {
	rn.connectedMu.RLock()
	connected := rn.connected
	primaryID := rn.primaryID
	lastAppliedLSN := rn.lastAppliedLSN
	rn.connectedMu.RUnlock()

	rn.heartbeatSeqMu.Lock()
	lastHeartbeatSeq := rn.lastReceivedHeartbeatSeq
	rn.heartbeatSeqMu.Unlock()

	return ReplicaStatusInfo{
		ReplicaID:        rn.replicaID,
		PrimaryID:        primaryID,
		Connected:        connected,
		LastAppliedLSN:   lastAppliedLSN,
		LastHeartbeatSeq: lastHeartbeatSeq,
		Timestamp:        time.Now(),
	}
}

// CalculateLagLSN calculates the LSN lag between this replica and the primary
func (rn *ReplicaNode) CalculateLagLSN(primaryCurrentLSN uint64) uint64 {
	if primaryCurrentLSN <= rn.lastAppliedLSN {
		return 0
	}
	return primaryCurrentLSN - rn.lastAppliedLSN
}
