//go:build zmq
// +build zmq

package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/wal"
	zmq "github.com/pebbe/zmq4"
)

// ZMQReplicaNode represents a ZeroMQ-based replica instance
type ZMQReplicaNode struct {
	config    ReplicationConfig
	replicaID string
	storage   *storage.GraphStorage

	// ZeroMQ sockets
	walSubscriber *zmq.Socket // SUB socket for WAL streaming
	healthDealer  *zmq.Socket // DEALER socket for health checks
	writePusher   *zmq.Socket // PUSH socket for write forwarding

	// State
	lastAppliedLSN uint64
	primaryID      string
	connected      bool
	connectedMu    sync.RWMutex

	// Channels
	stopCh    chan struct{}
	running   bool
	runningMu sync.Mutex
}

// NewZMQReplicaNode creates a new ZeroMQ-based replica node
func NewZMQReplicaNode(config ReplicationConfig, storage *storage.GraphStorage) (*ZMQReplicaNode, error) {
	if config.ReplicaID == "" {
		config.ReplicaID = generateID()
	}

	return &ZMQReplicaNode{
		config:    config,
		replicaID: config.ReplicaID,
		storage:   storage,
		stopCh:    make(chan struct{}),
	}, nil
}

// Start starts the ZeroMQ replica node
func (zr *ZMQReplicaNode) Start() error {
	zr.runningMu.Lock()
	defer zr.runningMu.Unlock()

	if zr.running {
		return fmt.Errorf("replica already running")
	}

	// Create SUB socket for WAL streaming
	sub, err := zmq.NewSocket(zmq.SUB)
	if err != nil {
		return fmt.Errorf("failed to create SUB socket: %w", err)
	}

	// Connect to primary's PUB socket
	walAddr := fmt.Sprintf("tcp://%s", zr.config.PrimaryAddr)
	if err := sub.Connect(walAddr); err != nil {
		sub.Close()
		return fmt.Errorf("failed to connect to primary WAL: %w", err)
	}

	// Subscribe to WAL topic
	if err := sub.SetSubscribe("WAL"); err != nil {
		sub.Close()
		return fmt.Errorf("failed to subscribe to WAL: %w", err)
	}
	zr.walSubscriber = sub
	log.Printf("Subscribed to WAL stream at %s", walAddr)

	// Create DEALER socket for health checks
	dealer, err := zmq.NewSocket(zmq.DEALER)
	if err != nil {
		zr.walSubscriber.Close()
		return fmt.Errorf("failed to create DEALER socket: %w", err)
	}

	// Set identity
	dealer.SetIdentity(zr.replicaID)

	// Connect to primary's ROUTER socket
	healthAddr := fmt.Sprintf("tcp://%s:9091", extractHost(zr.config.PrimaryAddr))
	if err := dealer.Connect(healthAddr); err != nil {
		zr.walSubscriber.Close()
		dealer.Close()
		return fmt.Errorf("failed to connect to health router: %w", err)
	}
	zr.healthDealer = dealer
	log.Printf("Connected to health router at %s", healthAddr)

	// Create PUSH socket for write forwarding (optional)
	pusher, err := zmq.NewSocket(zmq.PUSH)
	if err != nil {
		zr.walSubscriber.Close()
		zr.healthDealer.Close()
		return fmt.Errorf("failed to create PUSH socket: %w", err)
	}

	writeAddr := fmt.Sprintf("tcp://%s:9092", extractHost(zr.config.PrimaryAddr))
	if err := pusher.Connect(writeAddr); err != nil {
		zr.walSubscriber.Close()
		zr.healthDealer.Close()
		pusher.Close()
		return fmt.Errorf("failed to connect to write buffer: %w", err)
	}
	zr.writePusher = pusher
	log.Printf("Connected to write buffer at %s", writeAddr)

	zr.running = true
	zr.setConnected(true)

	// Start goroutines
	go zr.receiveWALEntries()
	go zr.sendHealthChecks()

	log.Printf("ZeroMQ replica started (replica_id=%s)", zr.replicaID)

	return nil
}

// Stop stops the ZeroMQ replica node
func (zr *ZMQReplicaNode) Stop() error {
	zr.runningMu.Lock()
	defer zr.runningMu.Unlock()

	if !zr.running {
		return nil
	}

	close(zr.stopCh)
	zr.running = false
	zr.setConnected(false)

	// Close sockets
	if zr.walSubscriber != nil {
		zr.walSubscriber.Close()
	}
	if zr.healthDealer != nil {
		zr.healthDealer.Close()
	}
	if zr.writePusher != nil {
		zr.writePusher.Close()
	}

	log.Printf("ZeroMQ replica stopped")

	return nil
}

// receiveWALEntries receives and applies WAL entries from primary
func (zr *ZMQReplicaNode) receiveWALEntries() {
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

			data, _ := json.Marshal(hc)

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

// GetReplicationState returns current replication state
func (zr *ZMQReplicaNode) GetReplicationState() ReplicationState {
	zr.connectedMu.RLock()
	defer zr.connectedMu.RUnlock()

	return ReplicationState{
		IsPrimary:  false,
		PrimaryID:  zr.primaryID,
		CurrentLSN: zr.lastAppliedLSN,
	}
}

// isConnected returns connection status
func (zr *ZMQReplicaNode) isConnected() bool {
	zr.connectedMu.RLock()
	defer zr.connectedMu.RUnlock()
	return zr.connected
}

// setConnected sets connection status
func (zr *ZMQReplicaNode) setConnected(connected bool) {
	zr.connectedMu.Lock()
	defer zr.connectedMu.Unlock()
	zr.connected = connected
}

// extractHost extracts host from address (e.g., "localhost:9090" -> "localhost")
func extractHost(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}
