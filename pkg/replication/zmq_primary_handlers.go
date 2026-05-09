//go:build zmq
// +build zmq

package replication

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
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

// executeWriteOperation executes a buffered write operation.
//
// Audit A8 (2026-05-09): fails closed when op.TenantID is empty.
// Mirror of WriteReceiver.executeWrite in write_receiver.go — see
// that comment for the full rationale and the
// REPLICATION_ALLOW_EMPTY_TENANT escape hatch.
func (zm *ZMQReplicationManager) executeWriteOperation(op *WriteOperation) {
	if op.TenantID == "" {
		if os.Getenv(replicationAllowEmptyTenantEnv) != "1" {
			log.Printf("replication (zmq): refusing %q with empty tenant_id; "+
				"set %s=1 to opt into legacy default-tenant behavior",
				op.Type, replicationAllowEmptyTenantEnv)
			return
		}
		op.TenantID = tenant.DefaultTenantID
	}

	switch op.Type {
	case "create_node":
		if _, err := zm.storage.CreateNodeWithTenant(op.TenantID, op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
		}
	case "create_edge":
		if _, err := zm.storage.CreateEdgeWithTenant(op.TenantID, op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
		}
	}
}
