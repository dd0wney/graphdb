package storage

// AddObserver registers a new node observer for GraphStorage.
func (gs *GraphStorage) AddObserver(observer NodeObserver) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.observers = append(gs.observers, observer)
}

func (gs *GraphStorage) notifyNodeCreated(node *Node) {
	gs.mu.RLock()
	observers := gs.observers
	gs.mu.RUnlock()
	for _, o := range observers {
		o.OnNodeCreated(node)
	}
}

func (gs *GraphStorage) notifyNodeUpdated(node *Node, oldNode *Node) {
	gs.mu.RLock()
	observers := gs.observers
	gs.mu.RUnlock()
	for _, o := range observers {
		o.OnNodeUpdated(node, oldNode)
	}
}

func (gs *GraphStorage) notifyNodeDeleted(nodeID uint64, tenantID string) {
	gs.mu.RLock()
	observers := gs.observers
	gs.mu.RUnlock()
	for _, o := range observers {
		o.OnNodeDeleted(nodeID, tenantID)
	}
}

// AddObserver registers a new node observer for BTreeGraphStorage.
func (gs *BTreeGraphStorage) AddObserver(observer NodeObserver) {
	gs.vectorIndexesMu.Lock() // Reusing an existing mutex for the spike
	defer gs.vectorIndexesMu.Unlock()
	gs.observers = append(gs.observers, observer)
}

func (gs *BTreeGraphStorage) notifyNodeCreated(node *Node) {
	gs.vectorIndexesMu.RLock()
	observers := gs.observers
	gs.vectorIndexesMu.RUnlock()
	for _, o := range observers {
		o.OnNodeCreated(node)
	}
}

func (gs *BTreeGraphStorage) notifyNodeUpdated(node *Node, oldNode *Node) {
	gs.vectorIndexesMu.RLock()
	observers := gs.observers
	gs.vectorIndexesMu.RUnlock()
	for _, o := range observers {
		o.OnNodeUpdated(node, oldNode)
	}
}

func (gs *BTreeGraphStorage) notifyNodeDeleted(nodeID uint64, tenantID string) {
	gs.vectorIndexesMu.RLock()
	observers := gs.observers
	gs.vectorIndexesMu.RUnlock()
	for _, o := range observers {
		o.OnNodeDeleted(nodeID, tenantID)
	}
}
