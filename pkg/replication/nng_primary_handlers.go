//go:build nng
// +build nng

package replication

import (
	"encoding/json"
	"log"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	"go.nanomsg.org/mangos/v3"
)

// convertProperties converts map[string]interface{} to map[string]storage.Value
func convertProperties(props map[string]interface{}) map[string]storage.Value {
	if props == nil {
		return nil
	}
	result := make(map[string]storage.Value, len(props))
	for k, v := range props {
		result[k] = interfaceToValue(v)
	}
	return result
}

// interfaceToValue converts an interface{} to storage.Value
func interfaceToValue(v interface{}) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case int:
		return storage.IntValue(int64(val))
	case int64:
		return storage.IntValue(val)
	case float64:
		return storage.FloatValue(val)
	case bool:
		return storage.BoolValue(val)
	case []byte:
		return storage.BytesValue(val)
	default:
		// For complex types, serialize to JSON bytes
		data, _ := json.Marshal(v)
		return storage.BytesValue(data)
	}
}

// publishWALEntries publishes WAL entries to all subscribers
func (nm *NNGReplicationManager) publishWALEntries() {
	defer nm.wg.Done()
	for {
		select {
		case <-nm.stopCh:
			return
		case entry := <-nm.walStream:
			// Serialize WAL entry
			data, err := json.Marshal(entry)
			if err != nil {
				log.Printf("Failed to marshal WAL entry: %v", err)
				continue
			}

			// Prepend topic for filtering (NNG doesn't have native topics like ZMQ)
			// We use a simple prefix: "WAL:" + data
			msg := append([]byte("WAL:"), data...)

			// Publish to local replicas
			if err := nm.walPublisher.Send(msg); err != nil {
				log.Printf("Failed to publish WAL entry: %v", err)
			}

			// Also publish to remote datacenters
			nm.publishToDatacenters(entry, msg)
		}
	}
}

// publishToDatacenters publishes to remote datacenters
func (nm *NNGReplicationManager) publishToDatacenters(entry *wal.Entry, data []byte) {
	nm.datacentersMu.RLock()
	defer nm.datacentersMu.RUnlock()

	for _, dc := range nm.datacenters {
		if dc.Connected && dc.Publisher != nil {
			if err := dc.Publisher.Send(data); err != nil {
				log.Printf("Failed to publish to datacenter %s: %v", dc.DatacenterID, err)
			}
		}
	}
}

// handleHealthChecks sends periodic surveys to all replicas and collects responses
func (nm *NNGReplicationManager) handleHealthChecks() {
	defer nm.wg.Done()

	// Set survey time (how long to wait for responses)
	nm.healthSurveyor.SetOption(mangos.OptionSurveyTime, 2*time.Second)

	ticker := time.NewTicker(5 * time.Second) // Survey every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-nm.stopCh:
			return
		case <-ticker.C:
			nm.conductHealthSurvey()
		}
	}
}

// conductHealthSurvey sends a survey to all replicas and collects their responses
func (nm *NNGReplicationManager) conductHealthSurvey() {
	// Build survey request (primary's current state)
	stats := nm.storage.GetStatistics()
	survey := HeartbeatMessage{
		From:       nm.primaryID,
		CurrentLSN: nm.storage.GetCurrentLSN(),
		NodeCount:  stats.NodeCount,
		EdgeCount:  stats.EdgeCount,
	}

	surveyData, err := json.Marshal(survey)
	if err != nil {
		log.Printf("Failed to marshal health survey: %v", err)
		return
	}

	// Send survey to all connected respondents
	if err := nm.healthSurveyor.Send(surveyData); err != nil {
		log.Printf("Failed to send health survey: %v", err)
		return
	}

	// Collect responses until timeout (OptionSurveyTime)
	respondedReplicas := make(map[string]bool)
	for {
		msg, err := nm.healthSurveyor.Recv()
		if err != nil {
			// Timeout or no more responses - survey complete
			break
		}

		// Parse replica response
		var hc HeartbeatMessage
		if err := json.Unmarshal(msg, &hc); err != nil {
			log.Printf("Failed to parse health response: %v", err)
			continue
		}

		// Update replica info
		nm.replicasMu.Lock()
		nm.replicas[hc.From] = &NNGReplicaInfo{
			ReplicaID:      hc.From,
			LastSeen:       time.Now(),
			LastAppliedLSN: hc.CurrentLSN,
			Healthy:        true,
		}
		nm.replicasMu.Unlock()

		respondedReplicas[hc.From] = true
	}

	// Mark replicas that didn't respond as unhealthy
	nm.replicasMu.Lock()
	for id, replica := range nm.replicas {
		if !respondedReplicas[id] {
			// Replica didn't respond to this survey
			if time.Since(replica.LastSeen) > 30*time.Second {
				replica.Healthy = false
			}
		}
	}
	nm.replicasMu.Unlock()

	if len(respondedReplicas) > 0 {
		log.Printf("Health survey complete: %d replicas responded", len(respondedReplicas))
	}
}

// handleBufferedWrites handles writes from PUSH sockets
func (nm *NNGReplicationManager) handleBufferedWrites() {
	defer nm.wg.Done()

	// Set receive timeout
	nm.writeReceiver.SetOption(mangos.OptionRecvDeadline, 1*time.Second)

	for {
		select {
		case <-nm.stopCh:
			return
		default:
		}

		// Receive write operation
		msg, err := nm.writeReceiver.Recv()
		if err != nil {
			continue
		}

		// Parse write operation
		var writeOp WriteOperation
		if err := json.Unmarshal(msg, &writeOp); err != nil {
			log.Printf("Failed to parse write operation: %v", err)
			continue
		}

		// Execute write operation
		nm.executeWriteOperation(&writeOp)
	}
}

// executeWriteOperation executes a buffered write operation
func (nm *NNGReplicationManager) executeWriteOperation(op *WriteOperation) {
	props := convertProperties(op.Properties)
	switch op.Type {
	case "create_node":
		if _, err := nm.storage.CreateNode(op.Labels, props); err != nil {
			log.Printf("Failed to create node: %v", err)
		}
	case "create_edge":
		if _, err := nm.storage.CreateEdge(op.FromNodeID, op.ToNodeID, op.EdgeType, props, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
		}
	}
}
