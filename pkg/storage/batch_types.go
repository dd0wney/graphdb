package storage

import "sync"

// Batch represents a batch of write operations
type Batch struct {
	graph *GraphStorage
	ops   []batchOp
	mu    sync.Mutex

	// Off-lock work collected by the execute* methods during Commit's locked
	// phase and drained after gs.mu is released — HNSW vector inserts and
	// observer dispatch (Track P H2 plan-under-lock / apply-off-lock), mirroring
	// Transaction.Commit. haveObservers is computed once under the lock at the
	// start of Commit so the execute* methods only clone for notify when needed.
	haveObservers    bool
	vectorPlans      []vectorInsertPlan
	createdForNotify []*Node
	updatedForNotify []batchUpdateNotify
	deletedForNotify []batchDeleteNotify
}

type batchUpdateNotify struct{ oldNode, newNode *Node }
type batchDeleteNotify struct {
	id       uint64
	tenantID string
}

type batchOpType int

const (
	opCreateNode batchOpType = iota
	opCreateEdge
	opUpdateNode
	opDeleteNode
	opDeleteEdge
)

type batchOp struct {
	opType     batchOpType
	nodeID     uint64
	edgeID     uint64
	labels     []string
	properties map[string]Value
	fromNodeID uint64
	toNodeID   uint64
	edgeType   string
	weight     float64
}

// BeginBatch starts a new batch operation
func (g *GraphStorage) BeginBatch() *Batch {
	return &Batch{
		graph: g,
		ops:   make([]batchOp, 0, 1000),
	}
}

// Size returns the number of operations in the batch
func (b *Batch) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.ops)
}

// Clear removes all operations from the batch
func (b *Batch) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops = b.ops[:0]
}
