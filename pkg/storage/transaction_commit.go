package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dd0wney/graphdb/pkg/wal"
)

// sortedTxIDs returns m's keys in ascending order. Node/edge IDs come from the
// monotonic atomic counter, so ascending ID == creation order. Commit iterates
// its buffers through this so the apply order (persist, WAL, and especially HNSW
// vector inserts) is deterministic — the direct and batch paths already apply in
// creation order, and a map's random iteration order made the transaction path's
// vector index occasionally disconnect on a tiny graph (the flaky
// TestMetamorphic_NoDelete: transaction top-k lost neighbours live/batch kept).
func sortedTxIDs[V any](m map[uint64]V) []uint64 {
	ids := make([]uint64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Commit applies all buffered changes atomically and durably.
//
//  1. Validate the whole buffer (edge endpoints + update targets resolve to
//     this tenant) BEFORE mutating anything — a bad reference aborts with
//     nothing applied (all-or-none for reference errors).
//  2. Under gs.mu, persist created nodes → created edges → property updates via
//     the SHARED persist*Locked helpers (the same shard/global/tenant/vector/
//     property indexes + stats the direct write paths maintain), collecting the
//     WAL entries and vector plans.
//  3. Release gs.mu, then make the whole batch durable with a SINGLE fsync
//     (all-or-none) via appendWALBatch. Durability is always atomic: appendWAL-
//     Batch is the last step, so any earlier error writes no WAL and nothing
//     becomes durable.
//  4. Off-lock: apply the HNSW vector inserts, then dispatch observer
//     notifications (so auto-embed observers see committed nodes).
//
// Commit serializes on gs.mu (last-writer-wins; no conflict detection between
// concurrent transactions). Limitation: a malformed-vector error (wrong
// dimension) during step 2 aborts the commit and writes no WAL — so nothing is
// durable — but may leave a transient, non-durable in-memory partial (the same
// behaviour the direct create/update paths have on a mid-apply error). It is
// cleaned up by the absence of a WAL record on the next restart.
func (tx *Transaction) Commit() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return ErrTransactionNotActive
	}
	if tx.committed || tx.rolledBack {
		return ErrTransactionAlreadyEnded
	}

	c, err := tx.applyLocked()
	if err != nil {
		return err
	}

	// (3) Atomic durability — one fsync for the whole batch. Propagate the
	// error: a commit that did not become durable must fail loudly.
	if err := tx.appendWALBarriered(c.walEntries); err != nil {
		return fmt.Errorf("commit: WAL durability: %w", err)
	}

	// (4) Off-lock: HNSW vector inserts, then observer dispatch.
	tx.gs.applyNodeVectorInserts(c.vectorPlans)
	if c.haveObservers {
		ctx := context.Background()
		for _, n := range c.createdForNotify {
			tx.gs.notifyNodeCreated(ctx, n)
		}
		for _, u := range c.updatesForNotify {
			tx.gs.notifyNodeUpdated(ctx, u.newNode, u.oldNode)
		}
	}

	return nil
}

// txCommit is what the locked half of Commit hands to the off-lock half.
type txCommit struct {
	walEntries       []wal.BatchEntry
	vectorPlans      []vectorInsertPlan
	haveObservers    bool
	createdForNotify []*Node
	updatesForNotify []txUpdateNotify
}

type txUpdateNotify struct{ oldNode, newNode *Node }

// applyLocked is steps 1 and 2 of Commit, under gs.mu. The deferred unlock
// matters: the Cypher executor and net/http recover panics, so a panic here
// that left gs.mu held would hang every later write, Close included.
//
// On success it returns with txWALBarrier read-held, and the caller must
// release it through appendWALBarriered. The barrier is taken before gs.mu is
// released: a CompactWAL boundary captured in between would see this commit's
// state in the snapshot while its entries are still unappended (LSN >
// boundary), and the surviving WAL would re-apply them over the snapshot on
// recovery (M-1). The order cannot be reversed either, because
// walLSNBarrieredLocked takes the barrier while it holds gs.mu.
func (tx *Transaction) applyLocked() (txCommit, error) {
	tx.gs.mu.Lock()
	defer tx.gs.mu.Unlock()
	panicPoint("Transaction.Commit")

	// (1) Validate references before any mutation (all-or-none).
	if err := tx.validateLocked(); err != nil {
		return txCommit{}, err
	}

	c := txCommit{
		walEntries:    make([]wal.BatchEntry, 0, len(tx.createdNodes)+len(tx.createdEdges)+len(tx.updatedNodes)),
		haveObservers: len(tx.gs.observers) > 0,
	}

	// (2a) Created nodes — through the shared persist helper (indexes, stats,
	// vector plan), then a WAL entry. Iterate in creation (ascending-ID) order so
	// the HNSW vector-insert order is deterministic (see sortedTxIDs).
	for _, nodeID := range sortedTxIDs(tx.createdNodes) {
		node := tx.createdNodes[nodeID]
		plans, err := tx.gs.persistNodeLocked(node)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: persist node %d: %w", node.ID, err)
		}
		c.vectorPlans = append(c.vectorPlans, plans...)
		data, err := json.Marshal(node)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: marshal node %d: %w", node.ID, err)
		}
		c.walEntries = append(c.walEntries, wal.BatchEntry{OpType: wal.OpCreateNode, Data: data})
		if c.haveObservers {
			c.createdForNotify = append(c.createdForNotify, node.Clone())
		}
	}

	// (2b) Created edges — endpoints are now persisted (or pre-existing).
	// Ascending-ID order for deterministic persist + WAL ordering.
	for _, edgeID := range sortedTxIDs(tx.createdEdges) {
		edge := tx.createdEdges[edgeID]
		if err := tx.gs.persistEdgeLocked(edge); err != nil {
			return txCommit{}, fmt.Errorf("commit: persist edge %d: %w", edge.ID, err)
		}
		data, err := json.Marshal(edge)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: marshal edge %d: %w", edge.ID, err)
		}
		c.walEntries = append(c.walEntries, wal.BatchEntry{OpType: wal.OpCreateEdge, Data: data})
	}

	// (2c) Property updates to existing nodes. (Updates to nodes created in this
	// same transaction were merged into the buffered node by tx.UpdateNode, so
	// they ride the OpCreateNode entry above — updatedNodes holds only existing-
	// node updates.)
	for _, nodeID := range sortedTxIDs(tx.updatedNodes) {
		props := tx.updatedNodes[nodeID]
		// mmap mode: promote a base-resident node into the overlay (CoW) before
		// the property-index update + in-place mutation below.
		node, err := tx.gs.materializeNode(nodeID)
		if err != nil {
			// validateLocked guaranteed existence + ownership; defensive only.
			return txCommit{}, fmt.Errorf("commit: update target %d vanished: %w", nodeID, err)
		}
		var oldNode *Node
		if c.haveObservers {
			oldNode = node.Clone()
		}
		// Maintain property indexes BEFORE mutating node.Properties — the helper
		// reads the old value off the live node to Remove it, then Inserts the
		// new (mirrors the direct UpdateNode path). Omitting this left the
		// property index stale after a transaction update of an existing node
		// (the per-tenant-index/#288 class, in the dormant transaction path).
		if err := tx.gs.updatePropertyIndexes(nodeID, node, props); err != nil {
			return txCommit{}, fmt.Errorf("commit: update property indexes for node %d: %w", nodeID, err)
		}
		tx.gs.writeTxNodeUpdate(node, props)

		// Re-index vectors for the updated node (parity with the direct
		// UpdateNode path).
		plans, err := tx.gs.planNodeVectorInserts(node)
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: plan vectors for updated node %d: %w", nodeID, err)
		}
		c.vectorPlans = append(c.vectorPlans, plans...)

		data, err := json.Marshal(nodeUpdateRecord{NodeID: nodeID, Properties: props})
		if err != nil {
			return txCommit{}, fmt.Errorf("commit: marshal update %d: %w", nodeID, err)
		}
		c.walEntries = append(c.walEntries, wal.BatchEntry{OpType: wal.OpUpdateNode, Data: data})
		if c.haveObservers {
			c.updatesForNotify = append(c.updatesForNotify, txUpdateNotify{oldNode: oldNode, newNode: node.Clone()})
		}
	}

	tx.committed = true
	tx.active = false
	tx.gs.txWALBarrier.RLock()
	return c, nil
}

// writeTxNodeUpdate merges props into node in place under the node's shard
// lock. Caller holds gs.mu.
func (gs *GraphStorage) writeTxNodeUpdate(node *Node, props map[string]Value) {
	gs.lockShard(node.ID)
	defer gs.unlockShard(node.ID)
	panicPoint("Transaction.Commit.shard")

	// A node created with nil properties has no map to merge into.
	if node.Properties == nil && len(props) > 0 {
		node.Properties = make(map[string]Value, len(props))
	}
	for k, v := range props {
		node.Properties[k] = v
	}
	node.UpdatedAt = time.Now().Unix()
}

// appendWALBarriered appends the commit's WAL batch and then releases the
// txWALBarrier read lock that applyLocked returned with. The release is
// deferred, so a panic in the WAL or in encryption cannot leave the barrier
// held: walLSNBarrieredLocked would then block on it while holding gs.mu, and
// Snapshot and Close would hang.
func (tx *Transaction) appendWALBarriered(entries []wal.BatchEntry) error {
	defer tx.gs.txWALBarrier.RUnlock()
	panicPoint("Transaction.Commit.wal")
	return tx.gs.noteWALWriteError(tx.gs.appendWALBatch(entries))
}

// validateLocked checks that every created edge's endpoints and every update
// target resolve to this transaction's tenant — either a node created in this
// same transaction or an existing node owned by the tenant. Caller holds gs.mu.
// Returning an error here aborts the commit before any mutation, giving
// all-or-none semantics for reference errors.
func (tx *Transaction) validateLocked() error {
	resolvable := func(id uint64) bool {
		if _, ok := tx.createdNodes[id]; ok {
			return true
		}
		_, err := tx.gs.getNodeRefForTenant(id, tx.tenantID)
		return err == nil
	}
	for _, edge := range tx.createdEdges {
		if !resolvable(edge.FromNodeID) {
			return fmt.Errorf("commit: edge %d from-node %d not found in tenant", edge.ID, edge.FromNodeID)
		}
		if !resolvable(edge.ToNodeID) {
			return fmt.Errorf("commit: edge %d to-node %d not found in tenant", edge.ID, edge.ToNodeID)
		}
	}
	for nodeID := range tx.updatedNodes {
		if !resolvable(nodeID) {
			return fmt.Errorf("commit: update target %d not found in tenant", nodeID)
		}
	}
	return tx.validateWidthsLocked()
}

// validateWidthsLocked refuses the commit before any mutation when a record
// it would write is too wide for the snapshot format. Checking in the apply
// phase instead would fail the commit with part of it already applied.
func (tx *Transaction) validateWidthsLocked() error {
	tenant := effectiveTenantID(tx.tenantID).String()
	for _, node := range tx.createdNodes {
		if err := checkNodeWidths(tenant, node.Labels, node.Properties); err != nil {
			return fmt.Errorf("commit: node %d: %w", node.ID, err)
		}
	}
	for _, edge := range tx.createdEdges {
		if err := checkEdgeWidths(tenant, edge.Type, edge.Properties); err != nil {
			return fmt.Errorf("commit: edge %d: %w", edge.ID, err)
		}
	}
	for nodeID, props := range tx.updatedNodes {
		if _, created := tx.createdNodes[nodeID]; created {
			continue // merged into the created node, checked above
		}
		existing, err := tx.gs.getNodeRefForTenant(nodeID, tx.tenantID)
		if err != nil {
			return fmt.Errorf("commit: update target %d: %w", nodeID, err)
		}
		if err := checkPatchWidths(existing.Properties, props, nil); err != nil {
			return fmt.Errorf("commit: update of node %d: %w", nodeID, err)
		}
	}
	return nil
}

// Rollback rolls back the transaction
func (tx *Transaction) Rollback() error {
	tx.mu.Lock()
	defer tx.mu.Unlock()

	if !tx.active {
		return nil // Rollback is idempotent
	}

	if tx.committed {
		return errors.New("cannot rollback a committed transaction")
	}

	// Since changes are buffered, rollback just means discarding the buffers
	// No need to undo anything as nothing was written to storage

	tx.rolledBack = true
	tx.active = false

	// Clear buffers
	tx.createdNodes = make(map[uint64]*Node)
	tx.createdEdges = make(map[uint64]*Edge)
	tx.updatedNodes = make(map[uint64]map[string]Value)

	return nil
}
