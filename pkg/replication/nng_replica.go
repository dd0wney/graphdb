//go:build nng
// +build nng

package replication

import (
	"fmt"
	"log"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"go.nanomsg.org/mangos/v3"
	"go.nanomsg.org/mangos/v3/protocol/push"
	"go.nanomsg.org/mangos/v3/protocol/respondent"
	"go.nanomsg.org/mangos/v3/protocol/sub"

	// Register transports
	_ "go.nanomsg.org/mangos/v3/transport/all"
)

// NNGReplicaNode represents an NNG-based replica instance
type NNGReplicaNode struct {
	config    ReplicationConfig
	replicaID string
	storage   *storage.GraphStorage

	// NNG sockets
	walSubscriber     mangos.Socket // SUB socket for WAL streaming
	healthRespondent  mangos.Socket // RESPONDENT socket for health surveys
	writePusher       mangos.Socket // PUSH socket for write forwarding

	// State
	lastAppliedLSN uint64
	primaryID      string
	connected      bool
	connectedMu    sync.RWMutex

	// Channels
	stopCh    chan struct{}
	wg        sync.WaitGroup
	running   bool
	runningMu sync.Mutex
}

// NewNNGReplicaNode creates a new NNG-based replica node
func NewNNGReplicaNode(config ReplicationConfig, storage *storage.GraphStorage) (*NNGReplicaNode, error) {
	if config.ReplicaID == "" {
		config.ReplicaID = generateID()
	}

	return &NNGReplicaNode{
		config:    config,
		replicaID: config.ReplicaID,
		storage:   storage,
		stopCh:    make(chan struct{}),
	}, nil
}

// Start starts the NNG replica node
func (nr *NNGReplicaNode) Start() error {
	nr.runningMu.Lock()
	defer nr.runningMu.Unlock()

	if nr.running {
		return fmt.Errorf("replica already running")
	}

	// Use cleanup helper to ensure all resources are closed on error
	cleanup := NewResourceCleanup()
	defer cleanup.Cleanup()

	// Create SUB socket for WAL streaming
	subSock, err := sub.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create SUB socket: %w", err)
	}
	cleanup.Add(subSock, "WAL subscriber")

	// Connect to primary's PUB socket
	walAddr := fmt.Sprintf("tcp://%s", nr.config.PrimaryAddr)
	if err := subSock.Dial(walAddr); err != nil {
		return fmt.Errorf("failed to connect to primary WAL: %w", err)
	}

	// Subscribe to WAL topic (empty string = all messages, or prefix filter)
	// NNG SUB uses prefix matching on the message bytes
	if err := subSock.SetOption(mangos.OptionSubscribe, []byte("WAL:")); err != nil {
		return fmt.Errorf("failed to subscribe to WAL: %w", err)
	}
	nr.walSubscriber = subSock
	log.Printf("Subscribed to WAL stream at %s", walAddr)

	// Create RESPONDENT socket for health surveys
	respSock, err := respondent.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create RESPONDENT socket: %w", err)
	}
	cleanup.Add(respSock, "health respondent")

	// Connect to primary's SURVEYOR socket
	healthAddr := fmt.Sprintf("tcp://%s:9091", extractHost(nr.config.PrimaryAddr))
	if err := respSock.Dial(healthAddr); err != nil {
		return fmt.Errorf("failed to connect to health surveyor: %w", err)
	}
	nr.healthRespondent = respSock
	log.Printf("Connected to health surveyor at %s", healthAddr)

	// Create PUSH socket for write forwarding
	pushSock, err := push.NewSocket()
	if err != nil {
		return fmt.Errorf("failed to create PUSH socket: %w", err)
	}
	cleanup.Add(pushSock, "write pusher")

	writeAddr := fmt.Sprintf("tcp://%s:9092", extractHost(nr.config.PrimaryAddr))
	if err := pushSock.Dial(writeAddr); err != nil {
		return fmt.Errorf("failed to connect to write buffer: %w", err)
	}
	nr.writePusher = pushSock
	log.Printf("Connected to write buffer at %s", writeAddr)

	nr.running = true
	nr.setConnected(true)

	// Start goroutines
	nr.wg.Add(2)
	go nr.receiveWALEntries()
	go nr.respondToHealthSurveys()

	log.Printf("NNG replica started (replica_id=%s)", nr.replicaID)

	// Success - prevent cleanup from closing resources
	cleanup.Clear()

	return nil
}

// Stop stops the NNG replica node
func (nr *NNGReplicaNode) Stop() error {
	nr.runningMu.Lock()
	defer nr.runningMu.Unlock()

	if !nr.running {
		return nil
	}

	close(nr.stopCh)
	nr.running = false
	nr.setConnected(false)

	// Close sockets
	if nr.walSubscriber != nil {
		if err := nr.walSubscriber.Close(); err != nil {
			log.Printf("Warning: Failed to close WAL subscriber: %v", err)
		}
	}
	if nr.healthRespondent != nil {
		if err := nr.healthRespondent.Close(); err != nil {
			log.Printf("Warning: Failed to close health respondent: %v", err)
		}
	}
	if nr.writePusher != nil {
		if err := nr.writePusher.Close(); err != nil {
			log.Printf("Warning: Failed to close write pusher: %v", err)
		}
	}

	// Wait for all goroutines to complete
	nr.wg.Wait()

	log.Printf("NNG replica stopped")

	return nil
}

// GetReplicationState returns current replication state
func (nr *NNGReplicaNode) GetReplicationState() ReplicationState {
	nr.connectedMu.RLock()
	defer nr.connectedMu.RUnlock()

	return ReplicationState{
		IsPrimary:  false,
		PrimaryID:  nr.primaryID,
		CurrentLSN: nr.lastAppliedLSN,
	}
}

// isConnected returns connection status
func (nr *NNGReplicaNode) isConnected() bool {
	nr.connectedMu.RLock()
	defer nr.connectedMu.RUnlock()
	return nr.connected
}

// setConnected sets connection status
func (nr *NNGReplicaNode) setConnected(connected bool) {
	nr.connectedMu.Lock()
	defer nr.connectedMu.Unlock()
	nr.connected = connected
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
