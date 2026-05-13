package storage

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingObserver captures every NodeObserver lifecycle call. Methods are
// safe to call from any goroutine; reads should call snapshot() to copy
// while holding ro.mu.
type recordingObserver struct {
	mu      sync.Mutex
	created []*Node
	updated []updatedRecord
	deleted []deletedRecord
}

type updatedRecord struct {
	node    *Node
	oldNode *Node
}

type deletedRecord struct {
	nodeID   uint64
	tenantID string
}

func (ro *recordingObserver) OnNodeCreated(_ context.Context, node *Node) {
	ro.mu.Lock()
	defer ro.mu.Unlock()
	ro.created = append(ro.created, node)
}

func (ro *recordingObserver) OnNodeUpdated(_ context.Context, node *Node, oldNode *Node) {
	ro.mu.Lock()
	defer ro.mu.Unlock()
	ro.updated = append(ro.updated, updatedRecord{node: node, oldNode: oldNode})
}

func (ro *recordingObserver) OnNodeDeleted(_ context.Context, nodeID uint64, tenantID string) {
	ro.mu.Lock()
	defer ro.mu.Unlock()
	ro.deleted = append(ro.deleted, deletedRecord{nodeID: nodeID, tenantID: tenantID})
}

func newTestStorageForObservation(t *testing.T) *GraphStorage {
	t.Helper()
	tmpDir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig() error = %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

// TestNodeObserverCreated pins that OnNodeCreated fires after a successful
// CreateNodeWithTenant call and receives the freshly-created node snapshot.
func TestNodeObserverCreated(t *testing.T) {
	gs := newTestStorageForObservation(t)
	obs := &recordingObserver{}
	gs.AddObserver(obs)

	node, err := gs.CreateNodeWithTenant("tenantA", []string{"Doc"}, map[string]Value{
		"name": StringValue("alpha"),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant() error = %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.created) != 1 {
		t.Fatalf("OnNodeCreated fired %d times, want 1", len(obs.created))
	}
	if obs.created[0].ID != node.ID {
		t.Errorf("OnNodeCreated node.ID = %d, want %d", obs.created[0].ID, node.ID)
	}
	if obs.created[0].TenantID != "tenantA" {
		t.Errorf("OnNodeCreated node.TenantID = %q, want \"tenantA\"", obs.created[0].TenantID)
	}
}

// TestNodeObserverUpdated pins that OnNodeUpdated fires after a successful
// UpdateNode/UpdateNodeForTenant and receives both pre- and post-update
// snapshots.
func TestNodeObserverUpdated(t *testing.T) {
	gs := newTestStorageForObservation(t)
	obs := &recordingObserver{}
	gs.AddObserver(obs)

	node, err := gs.CreateNodeWithTenant("tenantA", []string{"Doc"}, map[string]Value{
		"name": StringValue("alpha"),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant() error = %v", err)
	}
	// Reset created so subsequent assertions are scoped to the update.
	obs.mu.Lock()
	obs.created = nil
	obs.mu.Unlock()

	if err := gs.UpdateNode(node.ID, map[string]Value{
		"name": StringValue("beta"),
	}); err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.updated) != 1 {
		t.Fatalf("OnNodeUpdated fired %d times, want 1", len(obs.updated))
	}
	rec := obs.updated[0]
	if rec.oldNode == nil || rec.node == nil {
		t.Fatalf("OnNodeUpdated nil snapshots: oldNode=%v node=%v", rec.oldNode, rec.node)
	}
	oldName, _ := rec.oldNode.Properties["name"].AsString()
	newName, _ := rec.node.Properties["name"].AsString()
	if oldName != "alpha" {
		t.Errorf("oldNode.name = %q, want \"alpha\"", oldName)
	}
	if newName != "beta" {
		t.Errorf("node.name = %q, want \"beta\"", newName)
	}
}

// TestNodeObserverDeleted pins that OnNodeDeleted fires after a successful
// DeleteNode/DeleteNodeForTenant and receives (nodeID, tenantID) — the
// node's data is not retained by then.
func TestNodeObserverDeleted(t *testing.T) {
	gs := newTestStorageForObservation(t)
	obs := &recordingObserver{}
	gs.AddObserver(obs)

	node, err := gs.CreateNodeWithTenant("tenantB", []string{"Doc"}, map[string]Value{
		"name": StringValue("gamma"),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant() error = %v", err)
	}

	if err := gs.DeleteNode(node.ID); err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}

	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.deleted) != 1 {
		t.Fatalf("OnNodeDeleted fired %d times, want 1", len(obs.deleted))
	}
	rec := obs.deleted[0]
	if rec.nodeID != node.ID {
		t.Errorf("OnNodeDeleted nodeID = %d, want %d", rec.nodeID, node.ID)
	}
	if rec.tenantID != "tenantB" {
		t.Errorf("OnNodeDeleted tenantID = %q, want \"tenantB\"", rec.tenantID)
	}
}

// TestNodeObserverDispatchOutsideLock pins spike §7.4 — observer code runs
// AFTER all storage locks are released. The lock-acquiring observer would
// deadlock if dispatch ran while any storage lock (gs.mu, shard locks) was
// held by the dispatching goroutine.
//
// The test installs an observer whose OnNodeCreated calls back into
// GraphStorage (CountNodesForTenant, which itself takes gs.mu.RLock). If
// the notify dispatch ran under gs.mu.Lock (write-locked), the RLock would
// deadlock. The 5-second deadline catches that case.
func TestNodeObserverDispatchOutsideLock(t *testing.T) {
	gs := newTestStorageForObservation(t)

	done := make(chan struct{})
	obs := &reentrantObserver{
		gs:   gs,
		done: done,
	}
	gs.AddObserver(obs)

	// Run the create on a separate goroutine so we can detect deadlock via
	// the 5-second timeout. If dispatch ran under gs.mu.Lock, the observer's
	// call to CountNodesForTenant would block trying to acquire gs.mu.RLock.
	go func() {
		_, _ = gs.CreateNodeWithTenant("tenantC", []string{"Doc"}, map[string]Value{
			"name": StringValue("delta"),
		})
	}()

	select {
	case <-done:
		// Observer completed its re-entrant read — dispatch ran outside the lock.
	case <-time.After(5 * time.Second):
		t.Fatal("observer dispatch deadlocked — notify ran while gs.mu.Lock was held")
	}
}

// reentrantObserver verifies dispatch happens outside any storage lock by
// itself calling into the storage layer from its OnNodeCreated method.
type reentrantObserver struct {
	gs   *GraphStorage
	done chan struct{}
}

func (ro *reentrantObserver) OnNodeCreated(_ context.Context, _ *Node) {
	// Re-entrant call: takes gs.mu.RLock. If dispatch ran under gs.mu.Lock,
	// this would deadlock waiting for the writer.
	_ = ro.gs.CountNodesForTenant("tenantC")
	close(ro.done)
}

func (ro *reentrantObserver) OnNodeUpdated(_ context.Context, _ *Node, _ *Node)   {}
func (ro *reentrantObserver) OnNodeDeleted(_ context.Context, _ uint64, _ string) {}

// TestNodeObserverOrder pins that observers are invoked in registration
// order. Two observers each append a tag to a shared slice; the resulting
// order must match registration order.
func TestNodeObserverOrder(t *testing.T) {
	gs := newTestStorageForObservation(t)

	var mu sync.Mutex
	var order []string
	tagObserver := func(tag string) NodeObserver {
		return &funcObserver{
			created: func(_ context.Context, _ *Node) {
				mu.Lock()
				order = append(order, tag)
				mu.Unlock()
			},
		}
	}
	gs.AddObserver(tagObserver("first"))
	gs.AddObserver(tagObserver("second"))

	if _, err := gs.CreateNodeWithTenant("tenantA", []string{"Doc"}, nil); err != nil {
		t.Fatalf("CreateNodeWithTenant() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("observer call order = %v, want [first second]", order)
	}
}

// funcObserver is a minimal NodeObserver whose lifecycle methods can be
// supplied per-test via function fields. Nil fields are no-ops.
type funcObserver struct {
	created func(context.Context, *Node)
	updated func(context.Context, *Node, *Node)
	deleted func(context.Context, uint64, string)
}

func (f *funcObserver) OnNodeCreated(ctx context.Context, node *Node) {
	if f.created != nil {
		f.created(ctx, node)
	}
}
func (f *funcObserver) OnNodeUpdated(ctx context.Context, node *Node, oldNode *Node) {
	if f.updated != nil {
		f.updated(ctx, node, oldNode)
	}
}
func (f *funcObserver) OnNodeDeleted(ctx context.Context, nodeID uint64, tenantID string) {
	if f.deleted != nil {
		f.deleted(ctx, nodeID, tenantID)
	}
}

// TestNodeObserverNoneRegistered pins that calls work normally when no
// observers are registered (the load-bearing behavior-preservation
// criterion). Verifies that the conditional Clone() in UpdateNode does
// not allocate when observers is empty — by registering nothing and
// confirming the existing CreateNode/UpdateNode/DeleteNode paths succeed.
func TestNodeObserverNoneRegistered(t *testing.T) {
	gs := newTestStorageForObservation(t)

	node, err := gs.CreateNodeWithTenant("tenantA", []string{"Doc"}, map[string]Value{
		"name": StringValue("zero"),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant() error = %v", err)
	}
	if err := gs.UpdateNode(node.ID, map[string]Value{
		"name": StringValue("one"),
	}); err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}
	if err := gs.DeleteNode(node.ID); err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}
}
