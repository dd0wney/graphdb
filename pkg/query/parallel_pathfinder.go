package query

import (
	"context"
	"runtime"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ParallelPathFinder finds paths in parallel
type ParallelPathFinder struct {
	graph *storage.GraphStorage
}

// NewParallelPathFinder creates a parallel path finder
func NewParallelPathFinder(graph *storage.GraphStorage) *ParallelPathFinder {
	return &ParallelPathFinder{graph: graph}
}

// FindAllPaths finds all paths between multiple pairs of nodes in parallel
//
// Concurrent Safety:
// 1. Each pair is processed by independent goroutine
// 2. Semaphore limits concurrent path searches to NumCPU()
// 3. Each path search maintains independent visited map (no shared state)
// 4. Results collected via buffered channel after all workers complete
//
// Concurrent Edge Cases:
// 1. Semaphore prevents goroutine explosion when processing many pairs
// 2. Path search may fail (no path found) - returns nil, continues processing others
// 3. Goroutines may complete in any order - results order not preserved
// 4. All goroutines complete before returning (WaitGroup ensures this)
func (ppf *ParallelPathFinder) FindAllPaths(
	ctx context.Context,
	pairs [][2]uint64,
	maxDepth int,
) ([][]uint64, error) {
	numWorkers := runtime.NumCPU()
	resultChan := make(chan []uint64, len(pairs))
	errorChan := make(chan error, 1)

	// Use semaphore to limit concurrent path searches
	semaphore := make(chan struct{}, numWorkers)
	var wg sync.WaitGroup

	for _, pair := range pairs {
		// Respect context cancellation before starting new goroutine
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		default:
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(start, end uint64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// Respect context cancellation
			select {
			case <-ctx.Done():
				return
			default:
			}

			path := ppf.findPath(start, end, maxDepth)
			if path != nil {
				select {
				case resultChan <- path:
				case <-ctx.Done():
					return
				}
			}
		}(pair[0], pair[1])
	}

	// Wait and close
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	paths := make([][]uint64, 0, len(pairs))
	for path := range resultChan {
		// Respect context cancellation during collection
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		paths = append(paths, path)
	}

	// Check for errors
	select {
	case err := <-errorChan:
		return nil, err
	default:
	}

	return paths, nil
}

// findPath performs simple BFS path finding
func (ppf *ParallelPathFinder) findPath(start, end uint64, maxDepth int) []uint64 {
	visited := make(map[uint64]uint64) // node -> parent
	queue := []uint64{start}
	visited[start] = start

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		nextQueue := make([]uint64, 0)

		for _, nodeID := range queue {
			if nodeID == end {
				// Reconstruct path
				path := make([]uint64, 0)
				current := end
				for current != start {
					path = append(path, current)
					current = visited[current]
				}
				path = append(path, start)

				// Reverse
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}

				return path
			}

			edges, err := ppf.graph.GetOutgoingEdges(nodeID)
			if err != nil {
				continue
			}

			for _, edge := range edges {
				if _, seen := visited[edge.ToNodeID]; !seen {
					visited[edge.ToNodeID] = nodeID
					nextQueue = append(nextQueue, edge.ToNodeID)
				}
			}
		}

		queue = nextQueue
	}

	return nil // No path found
}
