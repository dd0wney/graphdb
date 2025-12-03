//go:build zmq
// +build zmq

package replication

import (
	"encoding/json"
	"log"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/wal"
)

// publishWALEntries publishes WAL entries to all subscribers
func (zm *ZMQReplicationManager) publishWALEntries() {
	defer zm.wg.Done()
	for {
		select {
		case <-zm.stopCh:
			return
		case entry := <-zm.walStream:
			// Serialize WAL entry
			data, err := json.Marshal(entry)
			if err != nil {
				log.Printf("Failed to marshal WAL entry: %v", err)
				continue
			}

			// Publish to local replicas (PUB/SUB)
			// Topic: "WAL" for filtering
			if _, err := zm.walPublisher.SendMessage("WAL", data); err != nil {
				log.Printf("Failed to publish WAL entry: %v", err)
			}

			// Also publish to remote datacenters
			zm.publishToDatacenters(entry, data)
		}
	}
}

// publishToDatacenters publishes to remote datacenters
func (zm *ZMQReplicationManager) publishToDatacenters(entry *wal.Entry, data []byte) {
	zm.datacentersMu.RLock()
	defer zm.datacentersMu.RUnlock()

	for _, dc := range zm.datacenters {
		if dc.Connected && dc.Publisher != nil {
			if _, err := dc.Publisher.SendMessage("WAL", data); err != nil {
				log.Printf("Failed to publish to datacenter %s: %v", dc.DatacenterID, err)
			}
		}
	}
}

// handleHealthChecks handles health check requests from replicas
func (zm *ZMQReplicationManager) handleHealthChecks() {
	defer zm.wg.Done()
	for {
		select {
		case <-zm.stopCh:
			return
		default:
		}

		// Receive health check with timeout
		zm.healthRouter.SetRcvtimeo(1 * time.Second)
		msg, err := zm.healthRouter.RecvMessage(0)
		if err != nil {
			// Timeout or error, continue
			continue
		}

		if len(msg) < 2 {
			continue
		}

		// ROUTER gives us: [identity, empty, payload]
		identity := msg[0]
		payload := msg[len(msg)-1]

		// Parse health check message
		var hc HeartbeatMessage
		if err := json.Unmarshal([]byte(payload), &hc); err != nil {
			log.Printf("Failed to parse health check: %v", err)
			continue
		}

		// Update replica info
		zm.replicasMu.Lock()
		zm.replicas[hc.From] = &ZMQReplicaInfo{
			ReplicaID:      hc.From,
			LastSeen:       time.Now(),
			LastAppliedLSN: hc.CurrentLSN,
			Healthy:        true,
		}
		zm.replicasMu.Unlock()

		// Send health check response
		stats := zm.storage.GetStatistics()
		response := HeartbeatMessage{
			From:       zm.primaryID,
			CurrentLSN: zm.storage.GetCurrentLSN(),
			NodeCount:  stats.NodeCount,
			EdgeCount:  stats.EdgeCount,
		}

		responseData, err := json.Marshal(response)
		if err != nil {
			log.Printf("Failed to marshal health check response: %v", err)
			continue
		}
		if _, err := zm.healthRouter.SendMessage(identity, "", responseData); err != nil {
			log.Printf("Failed to send health check response: %v", err)
		}
	}
}

// handleBufferedWrites handles writes from PUSH sockets
func (zm *ZMQReplicationManager) handleBufferedWrites() {
	defer zm.wg.Done()
	for {
		select {
		case <-zm.stopCh:
			return
		default:
		}

		// Receive write operation with timeout
		zm.writeReceiver.SetRcvtimeo(1 * time.Second)
		msg, err := zm.writeReceiver.RecvMessage(0)
		if err != nil {
			continue
		}

		if len(msg) == 0 {
			continue
		}

		// Parse write operation
		var writeOp WriteOperation
		if err := json.Unmarshal([]byte(msg[0]), &writeOp); err != nil {
			log.Printf("Failed to parse write operation: %v", err)
			continue
		}

		// Execute write operation
		zm.executeWriteOperation(&writeOp)
	}
}

// executeWriteOperation executes a buffered write operation
func (zm *ZMQReplicationManager) executeWriteOperation(op *WriteOperation) {
	switch op.Type {
	case "create_node":
		if _, err := zm.storage.CreateNode(op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
		}
	case "create_edge":
		if _, err := zm.storage.CreateEdge(op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
		}
	}
}
