// Package storage's observation.go defines the NodeObserver interface and
// the storage-side notify* dispatch helpers introduced by R2.1 (S11 spike,
// docs/internals/design/S11_AUTO_EMBEDDER_REDESIGN.md).
//
// The observer infrastructure is the foundation that R2.5 wires the
// AutoEmbedObserver into. R2.1 lands the interface + AddObserver method +
// post-mutation dispatch in 5 sites; the worker pool (R2.2), the Embedder
// interface (R2.3), the LSAEmbedder adapter (R2.4), and the
// AutoEmbedObserver itself (R2.5) come in subsequent PRs.
//
// Lock discipline (spike §7.4) — load-bearing for correctness:
//
//  1. Each public mutation method (CreateNodeWithTenant, UpdateNode, etc.)
//     completes its mutation under gs.mu.Lock + per-shard locks AND THEN
//     releases all of them.
//  2. After all locks release, the public method calls notifyNodeCreated /
//     notifyNodeUpdated / notifyNodeDeleted.
//  3. The notify* helpers themselves take gs.mu.RLock for the duration of
//     the observer-slice snapshot copy, then release it, and only THEN
//     invoke the observer's lifecycle method.
//
// This means observer code never runs under any storage lock. A slow
// observer cannot block concurrent storage operations beyond the brief
// RLock for the slice copy. An observer that itself calls into the
// storage layer (e.g., to write back an embedding) will not deadlock on
// the lock that triggered the dispatch.
//
// AddObserver is a concrete method on *GraphStorage; it is deliberately
// NOT added to the Storage interface yet — that gap closes in R3 alongside
// the F4 *VectorIndexForTenant methods. External wiring (server_init.go in
// R2.5) calls AddObserver via the concrete type at startup before serving
// requests.

package storage

import "context"

// NodeObserver receives node-lifecycle notifications. Implementations must
// be safe for concurrent calls from multiple goroutines: the same observer
// may be invoked from many storage operations in parallel.
//
// All three methods are passed context.Background() in the current
// implementation — the storage API does not carry context yet. Migration to
// *Ctx-passing variants is its own track and does not block R2.x.
//
// None of the lifecycle methods return an error. Errors encountered by the
// observer must be handled internally (log, meter, dead-letter queue). A
// panicking observer will crash the server; implementations must recover
// from panics or be verified panic-free.
type NodeObserver interface {
	// OnNodeCreated fires after a successful CreateNode* call, after all
	// storage locks have been released. node is a freshly-cloned snapshot
	// — safe to read without locking and safe to retain past return.
	OnNodeCreated(ctx context.Context, node *Node)

	// OnNodeUpdated fires after a successful UpdateNode* or
	// RemoveNodeProperties* call. node is the post-mutation snapshot;
	// oldNode is the pre-mutation snapshot. Both are clones; safe to read
	// and retain.
	//
	// Implementations that themselves write back to the same node (e.g.,
	// an auto-embedder that writes an embedding property) must guard
	// against re-triggering notification — the canonical pattern is a
	// sentinel property key or context value set before the internal
	// write. R2.5's AutoEmbedObserver implements this guard.
	OnNodeUpdated(ctx context.Context, node *Node, oldNode *Node)

	// OnNodeDeleted fires after a successful DeleteNode* call. The node's
	// data is no longer accessible by the time dispatch runs; only
	// nodeID and tenantID are provided. tenantID is the deleted node's
	// TenantID captured before deletion.
	OnNodeDeleted(ctx context.Context, nodeID uint64, tenantID string)
}

// AddObserver registers obs to receive node-lifecycle notifications.
//
// Observers are called in registration order. AddObserver is safe to call
// concurrently with storage operations, but the typical usage is at startup
// before serving requests (see R2.5's server_init.go wiring). Observers
// cannot be removed once registered — for tests, construct a fresh
// GraphStorage rather than detaching observers.
func (gs *GraphStorage) AddObserver(obs NodeObserver) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.observers = append(gs.observers, obs)
}

// snapshotObserversLocked returns a snapshot copy of the registered
// observer slice. Caller must NOT hold gs.mu.Lock when calling — the
// helper acquires the read lock itself. The snapshot is decoupled from
// the live slice so callers can iterate without holding any storage lock,
// which is the load-bearing invariant for spike §7.4.
func (gs *GraphStorage) snapshotObservers() []NodeObserver {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	if len(gs.observers) == 0 {
		return nil
	}
	snap := make([]NodeObserver, len(gs.observers))
	copy(snap, gs.observers)
	return snap
}

// notifyNodeCreated dispatches OnNodeCreated to every registered observer.
// Must be called AFTER all storage locks have been released by the public
// mutation method — see lock-discipline comment at the top of this file.
//
// node should be a clone (snapshot) of the freshly-created node, not the
// live shard pointer. The callers in node_operations.go capture this
// snapshot before unlocking.
func (gs *GraphStorage) notifyNodeCreated(ctx context.Context, node *Node) {
	observers := gs.snapshotObservers()
	for _, obs := range observers {
		obs.OnNodeCreated(ctx, node)
	}
}

// notifyNodeUpdated dispatches OnNodeUpdated to every registered observer.
// Both node and oldNode should be clones captured BEFORE the public
// mutation method's unlock.
func (gs *GraphStorage) notifyNodeUpdated(ctx context.Context, node *Node, oldNode *Node) {
	observers := gs.snapshotObservers()
	for _, obs := range observers {
		obs.OnNodeUpdated(ctx, node, oldNode)
	}
}

// notifyNodeDeleted dispatches OnNodeDeleted to every registered observer.
// nodeID and tenantID are captured from the looked-up node BEFORE the
// deletion runs.
func (gs *GraphStorage) notifyNodeDeleted(ctx context.Context, nodeID uint64, tenantID string) {
	observers := gs.snapshotObservers()
	for _, obs := range observers {
		obs.OnNodeDeleted(ctx, nodeID, tenantID)
	}
}
