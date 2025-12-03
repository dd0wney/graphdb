package replication

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

// WriteForwarder forwards write operations to the primary.
// Single responsibility: forward writes from replica to primary.
type WriteForwarder struct {
	socket      DialSocket
	primaryAddr string
	running     bool
	runningMu   sync.Mutex
}

// WriteForwarderConfig configures the write forwarder.
type WriteForwarderConfig struct {
	PrimaryAddr string
}

// NewWriteForwarder creates a new write forwarder.
func NewWriteForwarder(
	factory SocketFactory,
	config WriteForwarderConfig,
) (*WriteForwarder, error) {
	socket, err := factory.NewPushSocket()
	if err != nil {
		return nil, err
	}

	return &WriteForwarder{
		socket:      socket,
		primaryAddr: config.PrimaryAddr,
	}, nil
}

// Start connects to the primary.
func (f *WriteForwarder) Start() error {
	f.runningMu.Lock()
	defer f.runningMu.Unlock()

	if f.running {
		return nil
	}

	if err := f.socket.Dial(f.primaryAddr); err != nil {
		return err
	}

	f.running = true
	log.Printf("Write forwarder connected to %s", f.primaryAddr)
	return nil
}

// Stop disconnects from the primary.
func (f *WriteForwarder) Stop() error {
	f.runningMu.Lock()
	defer f.runningMu.Unlock()

	if !f.running {
		return nil
	}

	f.running = false
	f.socket.Close()
	log.Printf("Write forwarder stopped")
	return nil
}

// Forward sends a write operation to the primary.
func (f *WriteForwarder) Forward(op *WriteOperation) error {
	if !f.running {
		return fmt.Errorf("write forwarder not running")
	}

	data, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("failed to marshal write operation: %w", err)
	}

	return f.socket.Send(data)
}
