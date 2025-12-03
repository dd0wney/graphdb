package storage

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
