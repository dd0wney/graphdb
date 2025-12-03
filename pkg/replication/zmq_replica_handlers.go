//go:build zmq
// +build zmq

package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// receiveWALEntries receives and applies WAL entries from primary
func (zr *ZMQReplicaNode) receiveWALEntries() {
	defer zr.wg.Done()
	for {
		select {
		case <-zr.stopCh:
			return
		default:
		}

		// Receive message: [topic, data]
		msg, err := zr.walSubscriber.RecvMessage(0)
		if err != nil {
			log.Printf("Error receiving WAL entry: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if len(msg) < 2 {
			continue
		}

		// Parse WAL entry (second part is the data)
		var entry wal.Entry
		if err := json.Unmarshal([]byte(msg[1]), &entry); err != nil {
			log.Printf("Failed to unmarshal WAL entry: %v", err)
			continue
		}

		// Apply entry
		if err := zr.applyWALEntry(&entry); err != nil {
			log.Printf("Failed to apply WAL entry: %v", err)
		}
	}
}

// sendHealthChecks sends periodic health checks to primary
func (zr *ZMQReplicaNode) sendHealthChecks() {
	defer zr.wg.Done()
	ticker := time.NewTicker(zr.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-zr.stopCh:
			return
		case <-ticker.C:
			if !zr.isConnected() {
				continue
			}

			stats := zr.storage.GetStatistics()
			hc := HeartbeatMessage{
				From:       zr.replicaID,
				CurrentLSN: zr.lastAppliedLSN,
				NodeCount:  stats.NodeCount,
				EdgeCount:  stats.EdgeCount,
			}

			data, err := json.Marshal(hc)
			if err != nil {
				log.Printf("Failed to marshal health check: %v", err)
				continue
			}

			// Send to ROUTER via DEALER (no identity needed, DEALER adds it)
			if _, err := zr.healthDealer.SendMessage("", data); err != nil {
				log.Printf("Failed to send health check: %v", err)
				continue
			}

			// Receive response (optional)
			zr.healthDealer.SetRcvtimeo(2 * time.Second)
			_, err := zr.healthDealer.RecvMessage(0)
			if err != nil {
				log.Printf("No health check response: %v", err)
			}
		}
	}
}

// applyWALEntry applies a WAL entry to local storage
func (zr *ZMQReplicaNode) applyWALEntry(entry *wal.Entry) error {
	switch entry.OpType {
	case wal.OpCreateNode:
		var node storage.Node
		if err := json.Unmarshal(entry.Data, &node); err != nil {
			return fmt.Errorf("failed to unmarshal node: %w", err)
		}

		if _, err := zr.storage.CreateNode(node.Labels, node.Properties); err != nil {
			return fmt.Errorf("failed to create node: %w", err)
		}

	case wal.OpCreateEdge:
		var edge storage.Edge
		if err := json.Unmarshal(entry.Data, &edge); err != nil {
			return fmt.Errorf("failed to unmarshal edge: %w", err)
		}

		if _, err := zr.storage.CreateEdge(
			edge.FromNodeID,
			edge.ToNodeID,
			edge.Type,
			edge.Properties,
			edge.Weight,
		); err != nil {
			return fmt.Errorf("failed to create edge: %w", err)
		}
	}

	zr.lastAppliedLSN = entry.LSN

	return nil
}

// ForwardWrite forwards a write operation to primary (PUSH/PULL pattern)
func (zr *ZMQReplicaNode) ForwardWrite(op *WriteOperation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal write operation: %w", err)
	}

	if _, err := zr.writePusher.SendMessage(data); err != nil {
		return fmt.Errorf("failed to forward write: %w", err)
	}

	return nil
}
