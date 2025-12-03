package replication

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// receiveFromPrimary receives and applies WAL entries from primary
func (rn *ReplicaNode) receiveFromPrimary() {
	// Get decoder under lock once at start
	rn.connectedMu.RLock()
	decoder := rn.decoder
	rn.connectedMu.RUnlock()

	if decoder == nil {
		return
	}

	for {
		select {
		case <-rn.stopCh:
			return
		default:
		}

		var msg Message
		if err := decoder.Decode(&msg); err != nil {
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
		return rn.handleHeartbeatMessage(msg)

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
// This is a critical path - panic recovery ensures replica stability
func (rn *ReplicaNode) applyWALEntry(entry *wal.Entry) (err error) {
	// Panic recovery for critical replication path
	// A panic here could corrupt replica state or cause data loss
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in applyWALEntry (LSN=%d): %v", entry.LSN, r)
			err = fmt.Errorf("panic applying WAL entry LSN=%d: %v", entry.LSN, r)
		}
	}()

	// Apply the entry to storage
	if err := rn.applyWALOperation(entry); err != nil {
		return err
	}

	// Update last applied LSN and send acknowledgment
	return rn.acknowledgeWALEntry(entry.LSN)
}

// applyWALOperation applies a specific WAL operation to storage
func (rn *ReplicaNode) applyWALOperation(entry *wal.Entry) error {
	switch entry.OpType {
	case wal.OpCreateNode:
		return rn.applyCreateNode(entry.Data)

	case wal.OpCreateEdge:
		return rn.applyCreateEdge(entry.Data)

	default:
		log.Printf("Unknown WAL op type: %d", entry.OpType)
		return nil
	}
}

// applyCreateNode applies a node creation from WAL
func (rn *ReplicaNode) applyCreateNode(data []byte) error {
	var node storage.Node
	if err := json.Unmarshal(data, &node); err != nil {
		return fmt.Errorf("failed to unmarshal node: %w", err)
	}

	// Create node in storage (without WAL logging to avoid infinite loop)
	_, err := rn.storage.CreateNode(node.Labels, node.Properties)
	if err != nil {
		return fmt.Errorf("failed to create node: %w", err)
	}
	return nil
}

// applyCreateEdge applies an edge creation from WAL
func (rn *ReplicaNode) applyCreateEdge(data []byte) error {
	var edge storage.Edge
	if err := json.Unmarshal(data, &edge); err != nil {
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
	return nil
}

// acknowledgeWALEntry updates LSN and sends acknowledgment to primary
func (rn *ReplicaNode) acknowledgeWALEntry(lsn uint64) error {
	// Update last applied LSN under lock
	rn.connectedMu.Lock()
	rn.lastAppliedLSN = lsn
	lastAppliedLSN := rn.lastAppliedLSN
	rn.connectedMu.Unlock()

	// Send acknowledgment with current heartbeat sequence
	rn.heartbeatSeqMu.Lock()
	currentSeq := rn.lastReceivedHeartbeatSeq
	rn.heartbeatSeqMu.Unlock()

	ack := AckMessage{
		LastAppliedLSN:    lastAppliedLSN,
		ReplicaID:         rn.replicaID,
		HeartbeatSequence: currentSeq, // ACK the latest heartbeat we've seen
	}

	ackMsg, err := NewMessage(MsgAck, ack)
	if err != nil {
		return fmt.Errorf("failed to create ACK message: %w", err)
	}

	// Get encoder under lock to avoid race with disconnect
	rn.connectedMu.RLock()
	encoder := rn.encoder
	rn.connectedMu.RUnlock()

	if encoder == nil {
		return nil
	}

	return encoder.Encode(ackMsg)
}
