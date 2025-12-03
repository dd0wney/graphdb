package parallel

import "sync"

// TraverseDFS performs parallel depth-first traversal
// Note: DFS is inherently less parallelizable than BFS
func (pt *ParallelTraverser) TraverseDFS(startNode uint64, maxDepth int) []uint64 {
	visited := &sync.Map{}
	results := make(chan uint64, 1000)
	var wg sync.WaitGroup

	wg.Add(1)
	go pt.dfsWorker(startNode, 0, maxDepth, visited, results, &wg)

	// Wait for completion
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	collected := make([]uint64, 0)
	for nodeID := range results {
		collected = append(collected, nodeID)
	}

	return collected
}

// dfsWorker performs DFS recursively with parallelization at each level
func (pt *ParallelTraverser) dfsWorker(nodeID uint64, currentDepth, maxDepth int, visited *sync.Map, results chan<- uint64, wg *sync.WaitGroup) {
	defer wg.Done()

	// Check depth limit
	if currentDepth >= maxDepth {
		return
	}

	// Mark as visited
	if _, alreadyVisited := visited.LoadOrStore(nodeID, true); alreadyVisited {
		return
	}

	// Add to results
	results <- nodeID

	// Get outgoing edges
	edges, err := pt.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return
	}

	// Process edges in parallel if there are many
	if len(edges) > 10 {
		// Parallel processing for high-degree nodes
		for _, edge := range edges {
			wg.Add(1)
			edgeCopy := edge
			go pt.dfsWorker(edgeCopy.ToNodeID, currentDepth+1, maxDepth, visited, results, wg)
		}
	} else {
		// Sequential processing for low-degree nodes (less overhead)
		for _, edge := range edges {
			wg.Add(1)
			pt.dfsWorker(edge.ToNodeID, currentDepth+1, maxDepth, visited, results, wg)
		}
	}
}
