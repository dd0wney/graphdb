package parallel

import "sync"

// TraverseBFS performs parallel breadth-first traversal
func (pt *ParallelTraverser) TraverseBFS(startNodes []uint64, maxDepth int) []uint64 {
	if len(startNodes) == 0 {
		return nil
	}

	visited := &sync.Map{} // Thread-safe visited set
	allResults := make([]uint64, 0)

	// Track current and next level
	currentLevel := startNodes
	depth := 0

	// Mark start nodes as visited
	for _, nodeID := range startNodes {
		visited.Store(nodeID, true)
	}

	// Process levels
	for depth < maxDepth && len(currentLevel) > 0 {
		nextLevel := &sync.Map{} // Thread-safe next level set
		levelWg := sync.WaitGroup{}

		// Divide current level among workers (overflow-safe)
		chunkSize := 1
		if len(currentLevel) > 0 && pt.numWorkers > 0 {
			// Use int64 to prevent overflow in intermediate calculation
			chunkSize = int((int64(len(currentLevel)) + int64(pt.numWorkers) - 1) / int64(pt.numWorkers))
			if chunkSize < 1 {
				chunkSize = 1
			}
		}

		for i := 0; i < len(currentLevel); i += chunkSize {
			end := i + chunkSize
			if end > len(currentLevel) {
				end = len(currentLevel)
			}

			chunk := currentLevel[i:end]
			levelWg.Add(1)

			pt.workerPool.Submit(func() {
				defer levelWg.Done()
				pt.processChunk(chunk, visited, nextLevel)
			})
		}

		// Wait for level to complete
		levelWg.Wait()

		// Convert nextLevel to slice and add to results
		currentLevel = make([]uint64, 0)
		nextLevel.Range(func(key, value any) bool {
			if nodeID, ok := key.(uint64); ok {
				currentLevel = append(currentLevel, nodeID)
				allResults = append(allResults, nodeID)
			}
			return true
		})

		depth++
	}

	return allResults
}

// processChunk processes a chunk of nodes in parallel
func (pt *ParallelTraverser) processChunk(nodes []uint64, visited, nextLevel *sync.Map) {
	for _, nodeID := range nodes {
		// Get outgoing edges
		edges, err := pt.graph.GetOutgoingEdges(nodeID)
		if err != nil {
			continue
		}

		// Process each edge
		for _, edge := range edges {
			// Check if already visited
			if _, alreadyVisited := visited.LoadOrStore(edge.ToNodeID, true); !alreadyVisited {
				nextLevel.Store(edge.ToNodeID, true)
			}
		}
	}
}
