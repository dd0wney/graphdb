//go:build zmq
// +build zmq

package replication

import (
	"fmt"
	"log"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
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
	wg        sync.WaitGroup // Tracks all goroutines for clean shutdown
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

	// Use cleanup helper to ensure all resources are closed on error
	cleanup := NewResourceCleanup()
	defer cleanup.Cleanup()

	// Create SUB socket for WAL streaming
	sub, err := zmq.NewSocket(zmq.SUB)
	if err != nil {
		return fmt.Errorf("failed to create SUB socket: %w", err)
	}
	cleanup.Add(sub, "WAL subscriber")

	// Connect to primary's PUB socket
	walAddr := fmt.Sprintf("tcp://%s", zr.config.PrimaryAddr)
	if err := sub.Connect(walAddr); err != nil {
		return fmt.Errorf("failed to connect to primary WAL: %w", err)
	}

	// Subscribe to WAL topic
	if err := sub.SetSubscribe("WAL"); err != nil {
		return fmt.Errorf("failed to subscribe to WAL: %w", err)
	}
	zr.walSubscriber = sub
	log.Printf("Subscribed to WAL stream at %s", walAddr)

	// Create DEALER socket for health checks
	dealer, err := zmq.NewSocket(zmq.DEALER)
	if err != nil {
		return fmt.Errorf("failed to create DEALER socket: %w", err)
	}
	cleanup.Add(dealer, "health dealer")

	// Set identity
	if err := dealer.SetIdentity(zr.replicaID); err != nil {
		return fmt.Errorf("failed to set dealer identity: %w", err)
	}

	// Connect to primary's ROUTER socket
	healthAddr := fmt.Sprintf("tcp://%s:9091", extractHost(zr.config.PrimaryAddr))
	if err := dealer.Connect(healthAddr); err != nil {
		return fmt.Errorf("failed to connect to health router: %w", err)
	}
	zr.healthDealer = dealer
	log.Printf("Connected to health router at %s", healthAddr)

	// Create PUSH socket for write forwarding (optional)
	pusher, err := zmq.NewSocket(zmq.PUSH)
	if err != nil {
		return fmt.Errorf("failed to create PUSH socket: %w", err)
	}
	cleanup.Add(pusher, "write pusher")

	writeAddr := fmt.Sprintf("tcp://%s:9092", extractHost(zr.config.PrimaryAddr))
	if err := pusher.Connect(writeAddr); err != nil {
		return fmt.Errorf("failed to connect to write buffer: %w", err)
	}
	zr.writePusher = pusher
	log.Printf("Connected to write buffer at %s", writeAddr)

	zr.running = true
	zr.setConnected(true)

	// Start goroutines
	zr.wg.Add(1)
	go zr.receiveWALEntries()
	zr.wg.Add(1)
	go zr.sendHealthChecks()

	log.Printf("ZeroMQ replica started (replica_id=%s)", zr.replicaID)

	// Success - prevent cleanup from closing resources
	cleanup.Clear()

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
		if err := zr.walSubscriber.Close(); err != nil {
			log.Printf("Warning: Failed to close WAL subscriber: %v", err)
		}
	}
	if zr.healthDealer != nil {
		if err := zr.healthDealer.Close(); err != nil {
			log.Printf("Warning: Failed to close health dealer: %v", err)
		}
	}
	if zr.writePusher != nil {
		if err := zr.writePusher.Close(); err != nil {
			log.Printf("Warning: Failed to close write pusher: %v", err)
		}
	}

	// Wait for all goroutines to complete
	zr.wg.Wait()

	log.Printf("ZeroMQ replica stopped")

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
