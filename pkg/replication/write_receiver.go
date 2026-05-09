package replication

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// WriteExecutor executes write operations.
// Uses interface{} for properties to avoid tight coupling to storage.Value.
//
// Audit A8 (2026-05-09): methods carry an explicit tenantID. The
// caller (executeWrite) refuses empty tenantID by default — see the
// fail-closed gate there. Concrete implementers can assume tenantID
// is non-empty unless the REPLICATION_ALLOW_EMPTY_TENANT escape hatch
// is set (in which case the apply path defaults it to "default" before
// calling these methods).
type WriteExecutor interface {
	CreateNodeWithTenant(tenantID string, labels []string, properties map[string]interface{}) (uint64, error)
	CreateEdgeWithTenant(tenantID string, from, to uint64, edgeType string, properties map[string]interface{}, weight float64) (uint64, error)
}

// replicationAllowEmptyTenantEnv is the env var that opts a deployment
// out of the default fail-closed behavior on empty WriteOperation.TenantID.
// See doc on executeWrite — same shape as the JWT_SECRET fail-closed
// pattern in pkg/api/server_init.go.
const replicationAllowEmptyTenantEnv = "REPLICATION_ALLOW_EMPTY_TENANT"

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

// executeWrite applies a forwarded WriteOperation against the
// underlying storage.
//
// Audit A8 (2026-05-09): fails closed when op.TenantID is empty —
// silent default-to-default-tenant is the exact pattern this audit
// closes (in-house precedent: the JWT_SECRET fail-closed fix in
// pkg/api/server_init.go:74-77).
//
// The REPLICATION_ALLOW_EMPTY_TENANT=1 env var opts back into the
// legacy behavior (default empty to "default") for one-shot migration
// scenarios — e.g., draining writes from an unmigrated replica. Off
// by default; document and remove once all senders populate TenantID.
func (r *WriteReceiver) executeWrite(op *WriteOperation) {
	if op.TenantID == "" {
		if os.Getenv(replicationAllowEmptyTenantEnv) != "1" {
			log.Printf("replication: refusing %q with empty tenant_id; "+
				"set %s=1 to opt into legacy default-tenant behavior",
				op.Type, replicationAllowEmptyTenantEnv)
			return
		}
		op.TenantID = tenant.DefaultTenantID
	}

	switch op.Type {
	case "create_node":
		if _, err := r.executor.CreateNodeWithTenant(op.TenantID, op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
		}
	case "create_edge":
		if _, err := r.executor.CreateEdgeWithTenant(op.TenantID, op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
		}
	default:
		log.Printf("Unknown write operation type: %s", op.Type)
	}
}
