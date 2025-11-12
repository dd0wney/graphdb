package storage

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Batch represents a batch of write operations
type Batch struct {
	graph *GraphStorage
	ops   []batchOp
	mu    sync.Mutex
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

// AddNode queues a node creation in the batch
func (b *Batch) AddNode(labels []string, properties map[string]Value) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Allocate ID using thread-safe method
	nodeID, err := b.graph.allocateNodeID()
	if err != nil {
		return 0, err
	}

	b.ops = append(b.ops, batchOp{
		opType:     opCreateNode,
		nodeID:     nodeID,
		labels:     labels,
		properties: properties,
	})

	return nodeID, nil
}

// AddEdge queues an edge creation in the batch
func (b *Batch) AddEdge(fromNodeID, toNodeID uint64, edgeType string, properties map[string]Value, weight float64) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Allocate ID using thread-safe method
	edgeID, err := b.graph.allocateEdgeID()
	if err != nil {
		return 0, err
	}

	b.ops = append(b.ops, batchOp{
		opType:     opCreateEdge,
		edgeID:     edgeID,
		fromNodeID: fromNodeID,
		toNodeID:   toNodeID,
		edgeType:   edgeType,
		properties: properties,
		weight:     weight,
	})

	return edgeID, nil
}

// UpdateNode queues a node update in the batch
func (b *Batch) UpdateNode(nodeID uint64, properties map[string]Value) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ops = append(b.ops, batchOp{
		opType:     opUpdateNode,
		nodeID:     nodeID,
		properties: properties,
	})
}

// DeleteNode queues a node deletion in the batch
func (b *Batch) DeleteNode(nodeID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ops = append(b.ops, batchOp{
		opType: opDeleteNode,
		nodeID: nodeID,
	})
}

// DeleteEdge queues an edge deletion in the batch
func (b *Batch) DeleteEdge(edgeID uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ops = append(b.ops, batchOp{
		opType: opDeleteEdge,
		edgeID: edgeID,
	})
}

// Commit executes all batched operations atomically
func (b *Batch) Commit() error {
	b.graph.mu.Lock()
	defer b.graph.mu.Unlock()

	// Execute all operations
	for _, op := range b.ops {
		switch op.opType {
		case opCreateNode:
			node := &Node{
				ID:         op.nodeID,
				Labels:     op.labels,
				Properties: op.properties,
			}

			b.graph.nodes[node.ID] = node
			atomic.AddUint64(&b.graph.stats.NodeCount, 1)

			// Update label indexes
			for _, label := range node.Labels {
				b.graph.nodesByLabel[label] = append(b.graph.nodesByLabel[label], node.ID)
			}

			// Update property indexes
			for key, value := range node.Properties {
				if idx, exists := b.graph.propertyIndexes[key]; exists {
					idx.Insert(node.ID, value)
				}
			}

		case opCreateEdge:
			edge := &Edge{
				ID:         op.edgeID,
				FromNodeID: op.fromNodeID,
				ToNodeID:   op.toNodeID,
				Type:       op.edgeType,
				Properties: op.properties,
				Weight:     op.weight,
			}

			b.graph.edges[edge.ID] = edge
			atomic.AddUint64(&b.graph.stats.EdgeCount, 1)

			// Update edge type index
			b.graph.edgesByType[edge.Type] = append(b.graph.edgesByType[edge.Type], edge.ID)

			// Update adjacency lists
			b.graph.outgoingEdges[edge.FromNodeID] = append(b.graph.outgoingEdges[edge.FromNodeID], edge.ID)
			b.graph.incomingEdges[edge.ToNodeID] = append(b.graph.incomingEdges[edge.ToNodeID], edge.ID)

		case opUpdateNode:
			node, exists := b.graph.nodes[op.nodeID]
			if !exists {
				return fmt.Errorf("node %d not found", op.nodeID)
			}

			// Update property indexes (remove old, add new)
			for key, oldValue := range node.Properties {
				if idx, exists := b.graph.propertyIndexes[key]; exists {
					idx.Remove(node.ID, oldValue)
				}
			}

			// Update properties
			for key, value := range op.properties {
				node.Properties[key] = value
			}

			// Re-index
			for key, value := range node.Properties {
				if idx, exists := b.graph.propertyIndexes[key]; exists {
					idx.Insert(node.ID, value)
				}
			}

		case opDeleteNode:
			node, exists := b.graph.nodes[op.nodeID]
			if !exists {
				continue
			}

			// Remove from label indexes
			for _, label := range node.Labels {
				ids := b.graph.nodesByLabel[label]
				for i, id := range ids {
					if id == op.nodeID {
						b.graph.nodesByLabel[label] = append(ids[:i], ids[i+1:]...)
						break
					}
				}
			}

			// Remove from property indexes
			for key, value := range node.Properties {
				if idx, exists := b.graph.propertyIndexes[key]; exists {
					idx.Remove(op.nodeID, value)
				}
			}

			// Delete edges
			outgoing := b.graph.outgoingEdges[op.nodeID]
			for _, edgeID := range outgoing {
				delete(b.graph.edges, edgeID)
				// Atomic decrement with underflow protection
				for {
					current := atomic.LoadUint64(&b.graph.stats.EdgeCount)
					if current == 0 {
						break
					}
					if atomic.CompareAndSwapUint64(&b.graph.stats.EdgeCount, current, current-1) {
						break
					}
				}
			}

			incoming := b.graph.incomingEdges[op.nodeID]
			for _, edgeID := range incoming {
				delete(b.graph.edges, edgeID)
				// Atomic decrement with underflow protection
				for {
					current := atomic.LoadUint64(&b.graph.stats.EdgeCount)
					if current == 0 {
						break
					}
					if atomic.CompareAndSwapUint64(&b.graph.stats.EdgeCount, current, current-1) {
						break
					}
				}
			}

			delete(b.graph.outgoingEdges, op.nodeID)
			delete(b.graph.incomingEdges, op.nodeID)

			// Delete node with atomic decrement
			delete(b.graph.nodes, op.nodeID)
			for {
				current := atomic.LoadUint64(&b.graph.stats.NodeCount)
				if current == 0 {
					break
				}
				if atomic.CompareAndSwapUint64(&b.graph.stats.NodeCount, current, current-1) {
					break
				}
			}

		case opDeleteEdge:
			edge, exists := b.graph.edges[op.edgeID]
			if !exists {
				continue
			}

			// Remove from type index
			edgeIDs := b.graph.edgesByType[edge.Type]
			for i, id := range edgeIDs {
				if id == op.edgeID {
					b.graph.edgesByType[edge.Type] = append(edgeIDs[:i], edgeIDs[i+1:]...)
					break
				}
			}

			// Remove from adjacency lists
			outgoing := b.graph.outgoingEdges[edge.FromNodeID]
			for i, id := range outgoing {
				if id == op.edgeID {
					b.graph.outgoingEdges[edge.FromNodeID] = append(outgoing[:i], outgoing[i+1:]...)
					break
				}
			}

			incoming := b.graph.incomingEdges[edge.ToNodeID]
			for i, id := range incoming {
				if id == op.edgeID {
					b.graph.incomingEdges[edge.ToNodeID] = append(incoming[:i], incoming[i+1:]...)
					break
				}
			}

			// Delete edge with atomic decrement
			delete(b.graph.edges, op.edgeID)
			for {
				current := atomic.LoadUint64(&b.graph.stats.EdgeCount)
				if current == 0 {
					break
				}
				if atomic.CompareAndSwapUint64(&b.graph.stats.EdgeCount, current, current-1) {
					break
				}
			}
		}
	}

	return nil
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
