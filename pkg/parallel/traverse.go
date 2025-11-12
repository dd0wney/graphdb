package parallel

import (
	"runtime"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ParallelTraverser performs parallel graph traversals
type ParallelTraverser struct {
	graph      *storage.GraphStorage
	workerPool *WorkerPool
	numWorkers int
}

// NewParallelTraverser creates a new parallel traverser
func NewParallelTraverser(graph *storage.GraphStorage, numWorkers int) *ParallelTraverser {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	return &ParallelTraverser{
		graph:      graph,
		workerPool: NewWorkerPool(numWorkers),
		numWorkers: numWorkers,
	}
}

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
		nextLevel.Range(func(key, value interface{}) bool {
			nodeID := key.(uint64)
			currentLevel = append(currentLevel, nodeID)
			allResults = append(allResults, nodeID)
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

// ParallelShortestPath finds shortest paths using parallel BFS
func (pt *ParallelTraverser) ParallelShortestPath(startNode, endNode uint64, maxDepth int) ([]uint64, error) {
	if startNode == endNode {
		return []uint64{startNode}, nil
	}

	visited := &sync.Map{}
	parent := &sync.Map{}
	found := make(chan bool, 1)

	visited.Store(startNode, true)
	currentLevel := []uint64{startNode}
	depth := 0

	// BFS with parallel level processing
	for depth < maxDepth && len(currentLevel) > 0 {
		nextLevel := &sync.Map{}
		levelWg := sync.WaitGroup{}

		// Divide work among workers (overflow-safe)
		chunkSize := 1
		if len(currentLevel) > 0 && pt.numWorkers > 0 {
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

				for _, nodeID := range chunk {
					edges, err := pt.graph.GetOutgoingEdges(nodeID)
					if err != nil {
						continue
					}

					for _, edge := range edges {
						if _, alreadyVisited := visited.LoadOrStore(edge.ToNodeID, true); !alreadyVisited {
							parent.Store(edge.ToNodeID, nodeID)
							nextLevel.Store(edge.ToNodeID, true)

							// Check if we found the target
							if edge.ToNodeID == endNode {
								select {
								case found <- true:
								default:
								}
								return
							}
						}
					}
				}
			})
		}

		levelWg.Wait()

		// Check if path was found
		select {
		case <-found:
			// Reconstruct path
			return pt.reconstructPath(parent, startNode, endNode), nil
		default:
		}

		// Convert nextLevel to slice with safe type assertion
		currentLevel = make([]uint64, 0)
		nextLevel.Range(func(key, value interface{}) bool {
			if nodeID, ok := key.(uint64); ok {
				currentLevel = append(currentLevel, nodeID)
			}
			return true
		})

		depth++
	}

	return nil, storage.ErrNodeNotFound
}

// reconstructPath reconstructs the path from parent map
func (pt *ParallelTraverser) reconstructPath(parent *sync.Map, start, end uint64) []uint64 {
	path := make([]uint64, 0)
	current := end

	for current != start {
		path = append(path, current)
		parentVal, ok := parent.Load(current)
		if !ok {
			return nil
		}
		// Safe type assertion
		parentID, ok := parentVal.(uint64)
		if !ok {
			return nil // Invalid data in parent map
		}
		current = parentID
	}
	path = append(path, start)

	// Reverse path
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	return path
}

// Close closes the worker pool
func (pt *ParallelTraverser) Close() {
	pt.workerPool.Close()
}
