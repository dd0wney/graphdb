package replication

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// WriteExecutor executes write operations.
// Uses interface{} for properties to avoid tight coupling to storage.Value.
type WriteExecutor interface {
	CreateNode(labels []string, properties map[string]interface{}) (uint64, error)
	CreateEdge(from, to uint64, edgeType string, properties map[string]interface{}, weight float64) (uint64, error)
}

// WriteReceiver receives and executes buffered write operations.
// Single responsibility: receive writes from replicas and execute them.
type WriteReceiver struct {
	socket      ListenSocket
	addr        string
	executor    WriteExecutor
	recvTimeout time.Duration
	stopCh      chan struct{}
	wg          sync.WaitGroup
	running     bool
	runningMu   sync.Mutex
}

// WriteReceiverConfig configures the write receiver.
type WriteReceiverConfig struct {
	Address     string
	RecvTimeout time.Duration
}

// NewWriteReceiver creates a new write receiver.
func NewWriteReceiver(
	factory SocketFactory,
	config WriteReceiverConfig,
	executor WriteExecutor,
) (*WriteReceiver, error) {
	socket, err := factory.NewPullSocket()
	if err != nil {
		return nil, err
	}

	timeout := config.RecvTimeout
	if timeout <= 0 {
		timeout = 1 * time.Second
	}

	return &WriteReceiver{
		socket:      socket,
		addr:        config.Address,
		executor:    executor,
		recvTimeout: timeout,
		stopCh:      make(chan struct{}),
	}, nil
}

// Start begins receiving writes.
func (r *WriteReceiver) Start() error {
	r.runningMu.Lock()
	defer r.runningMu.Unlock()

	if r.running {
		return nil
	}

	if err := r.socket.Listen(r.addr); err != nil {
		return err
	}

	if err := r.socket.SetRecvDeadline(r.recvTimeout); err != nil {
		r.socket.Close()
		return err
	}

	r.running = true
	r.wg.Add(1)
	go r.receiveLoop()

	log.Printf("Write receiver started on %s", r.addr)
	return nil
}

// Stop stops the receiver.
func (r *WriteReceiver) Stop() error {
	r.runningMu.Lock()
	defer r.runningMu.Unlock()

	if !r.running {
		return nil
	}

	close(r.stopCh)
	r.running = false
	r.wg.Wait()
	r.socket.Close()

	log.Printf("Write receiver stopped")
	return nil
}

func (r *WriteReceiver) receiveLoop() {
	defer r.wg.Done()

	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		msg, err := r.socket.Recv()
		if err != nil {
			continue // Timeout
		}

		var op WriteOperation
		if err := json.Unmarshal(msg, &op); err != nil {
			log.Printf("Failed to parse write operation: %v", err)
			continue
		}

		r.executeWrite(&op)
	}
}

func (r *WriteReceiver) executeWrite(op *WriteOperation) {
	switch op.Type {
	case "create_node":
		if _, err := r.executor.CreateNode(op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
		}
	case "create_edge":
		if _, err := r.executor.CreateEdge(op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
		}
	default:
		log.Printf("Unknown write operation type: %s", op.Type)
	}
}
