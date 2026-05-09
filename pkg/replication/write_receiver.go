package replication

import (
	"encoding/json"
	"fmt"
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

// ApplyWriteOperation applies a single WriteOperation against executor,
// with the same fail-closed gate as the in-package receive-loop
// dispatcher (WriteReceiver.executeWrite). Exposed so the audit
// regression suite can drive the apply path end-to-end without
// standing up a full WriteReceiver + socket — see the
// A8/replication-write-preserves-tenant row in
// pkg/api/audit_regression_test.go.
//
// Audit A8 (2026-05-09): empty op.TenantID is refused by default —
// silent default-tenant routing is the exact pattern this audit
// closes (in-house precedent: the JWT_SECRET fail-closed fix in
// pkg/api/server_init.go:74-77).
//
// The REPLICATION_ALLOW_EMPTY_TENANT=1 env var opts back into the
// legacy behavior (default empty to "default") for one-shot migration
// scenarios — e.g., draining writes from an unmigrated replica. Off
// by default; document and remove once all senders populate TenantID.
//
// op is taken by value so the escape-hatch's TenantID rewrite never
// mutates caller state. Callers can reuse a single op struct across
// multiple apply calls without surprise.
//
// Returns nil on successful dispatch AND on fail-closed refusal —
// refusal is the documented success path of the gate, signaled by
// the log.Printf above. A non-nil return means the executor itself
// rejected the op (e.g., storage error); callers driving the apply
// path from tests should treat a non-nil return as a fatal signal
// rather than relying on observable-state assertions alone.
func ApplyWriteOperation(executor WriteExecutor, op WriteOperation) error {
	if op.TenantID == "" {
		if os.Getenv(replicationAllowEmptyTenantEnv) != "1" {
			log.Printf("replication: refusing %q with empty tenant_id; "+
				"set %s=1 to opt into legacy default-tenant behavior",
				op.Type, replicationAllowEmptyTenantEnv)
			return nil
		}
		op.TenantID = tenant.DefaultTenantID
	}

	switch op.Type {
	case "create_node":
		if _, err := executor.CreateNodeWithTenant(op.TenantID, op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
			return fmt.Errorf("create_node: %w", err)
		}
	case "create_edge":
		if _, err := executor.CreateEdgeWithTenant(op.TenantID, op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
			return fmt.Errorf("create_edge: %w", err)
		}
	default:
		log.Printf("Unknown write operation type: %s", op.Type)
	}
	return nil
}

// executeWrite is the in-package receive-loop dispatcher. Thin
// wrapper around ApplyWriteOperation; the (*WriteOperation)
// signature mirrors the receive-loop's already-allocated unmarshal
// target. Errors are intentionally discarded — the loop has no
// upstream caller to surface them to.
func (r *WriteReceiver) executeWrite(op *WriteOperation) {
	_ = ApplyWriteOperation(r.executor, *op)
}
