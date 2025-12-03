package parallel

import (
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
		nextLevel.Range(func(key, value any) bool {
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
