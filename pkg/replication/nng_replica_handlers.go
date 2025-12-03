//go:build nng
// +build nng

package replication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	"go.nanomsg.org/mangos/v3"
)

// receiveWALEntries receives and applies WAL entries from primary
func (nr *NNGReplicaNode) receiveWALEntries() {
	defer nr.wg.Done()

	// Set receive timeout
	nr.walSubscriber.SetOption(mangos.OptionRecvDeadline, 1*time.Second)

	for {
		select {
		case <-nr.stopCh:
			return
		default:
		}

		// Receive message
		msg, err := nr.walSubscriber.Recv()
		if err != nil {
			// Timeout or error, continue
			continue
		}

		// Strip the "WAL:" prefix
		const walPrefix = "WAL:"
		if !bytes.HasPrefix(msg, []byte(walPrefix)) {
			log.Printf("Received message without WAL prefix, ignoring")
			continue
		}
		data := msg[len(walPrefix):]

		// Parse WAL entry
		var entry wal.Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			log.Printf("Failed to unmarshal WAL entry: %v", err)
			continue
		}

		// Apply entry
		if err := nr.applyWALEntry(&entry); err != nil {
			log.Printf("Failed to apply WAL entry: %v", err)
		}
	}
}

// respondToHealthSurveys responds to health surveys from the primary
func (nr *NNGReplicaNode) respondToHealthSurveys() {
	defer nr.wg.Done()

	// Set receive timeout
	nr.healthRespondent.SetOption(mangos.OptionRecvDeadline, 1*time.Second)

	for {
		select {
		case <-nr.stopCh:
			return
		default:
		}

		// Wait for survey from primary
		msg, err := nr.healthRespondent.Recv()
		if err != nil {
			// Timeout or error, continue
			continue
		}

		// Parse survey (primary's state)
		var primaryState HeartbeatMessage
		if err := json.Unmarshal(msg, &primaryState); err != nil {
			log.Printf("Failed to parse health survey: %v", err)
			continue
		}

		// Update our knowledge of primary
		nr.primaryID = primaryState.From

		// Build and send our response
		stats := nr.storage.GetStatistics()
		response := HeartbeatMessage{
			From:       nr.replicaID,
			CurrentLSN: nr.lastAppliedLSN,
			NodeCount:  stats.NodeCount,
			EdgeCount:  stats.EdgeCount,
		}

		responseData, err := json.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshal health response: %v", err)
			continue
		}

		if err := nr.healthRespondent.Send(responseData); err != nil {
			log.Printf("Failed to send health response: %v", err)
		}
	}
}

// applyWALEntry applies a WAL entry to local storage
func (nr *NNGReplicaNode) applyWALEntry(entry *wal.Entry) error {
	switch entry.OpType {
	case wal.OpCreateNode:
		var node storage.Node
		if err := json.Unmarshal(entry.Data, &node); err != nil {
			return fmt.Errorf("failed to unmarshal node: %w", err)
		}

		if _, err := nr.storage.CreateNode(node.Labels, node.Properties); err != nil {
			return fmt.Errorf("failed to create node: %w", err)
		}

	case wal.OpCreateEdge:
		var edge storage.Edge
		if err := json.Unmarshal(entry.Data, &edge); err != nil {
			return fmt.Errorf("failed to unmarshal edge: %w", err)
		}

		if _, err := nr.storage.CreateEdge(
			edge.FromNodeID,
			edge.ToNodeID,
			edge.Type,
			edge.Properties,
			edge.Weight,
		); err != nil {
			return fmt.Errorf("failed to create edge: %w", err)
		}
	}

	nr.lastAppliedLSN = entry.LSN

	return nil
}

// ForwardWrite forwards a write operation to primary (PUSH/PULL pattern)
func (nr *NNGReplicaNode) ForwardWrite(op *WriteOperation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal write operation: %w", err)
	}

	if err := nr.writePusher.Send(data); err != nil {
		return fmt.Errorf("failed to forward write: %w", err)
	}

	return nil
}
