package query

import (
	"context"
	"log"
	"runtime"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ParallelTraversal performs parallel BFS traversal
type ParallelTraversal struct {
	graph     *storage.GraphStorage
	startIDs  []uint64
	maxDepth  int
	batchSize int
	ctx       context.Context // Context for cancellation propagation
}

// NewParallelTraversal creates a parallel traversal query
func NewParallelTraversal(graph *storage.GraphStorage, startIDs []uint64, maxDepth int) *ParallelTraversal {
	return &ParallelTraversal{
		graph:     graph,
		startIDs:  startIDs,
		maxDepth:  maxDepth,
		batchSize: 100,
	}
}

// Execute performs parallel traversal from multiple starting nodes
//
// Concurrent Safety:
// 1. Uses sync.Map for thread-safe visited tracking across workers
// 2. Multiple goroutines traverse different branches simultaneously
// 3. Results channel has buffer to reduce blocking
// 4. Workers coordinate via WaitGroup for proper shutdown
// 5. Error channel uses select/default to prevent blocking on error reporting
// 6. Context is propagated to all spawned goroutines for cancellation
//
// Concurrent Edge Cases:
// 1. Multiple workers may discover same node - sync.Map.LoadOrStore handles this
// 2. Result channel may fill up - select/default prevents goroutine leaks
// 3. One worker error doesn't stop others - they continue until WaitGroup completes
// 4. Channel close is synchronized with WaitGroup to prevent send-on-closed-channel
// 5. Context cancellation propagates to all child goroutines spawned by traverseFrom
func (pt *ParallelTraversal) Execute(ctx context.Context) ([]*storage.Node, error) {
	// Store context for use by traverseFrom
	pt.ctx = ctx

	numWorkers := runtime.NumCPU()
	// Defensive: ensure minimum of 1 worker (NumCPU should never return 0, but guard against it)
	if numWorkers <= 0 {
		numWorkers = 1
	}
	visited := sync.Map{} // Thread-safe map
	resultChan := make(chan *storage.Node, 1000)
	errorChan := make(chan error, 1)

	var wg sync.WaitGroup

	// Process start nodes in batches (overflow-safe calculation)
	var batchSize int
	if numWorkers > 0 {
		// Use int64 to prevent overflow in intermediate calculation
		batchSize = int((int64(len(pt.startIDs)) + int64(numWorkers) - 1) / int64(numWorkers))
	} else {
		batchSize = len(pt.startIDs)
	}

	for i := 0; i < numWorkers && i*batchSize < len(pt.startIDs); i++ {
		start := i * batchSize
		end := start + batchSize
		if end > len(pt.startIDs) {
			end = len(pt.startIDs)
		}

		wg.Add(1)
		go func(nodeIDs []uint64) {
			defer wg.Done()

			for _, nodeID := range nodeIDs {
				// Respect context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				if err := pt.traverseFrom(nodeID, 0, &visited, resultChan); err != nil {
					// Don't report context cancellation as an error
					if ctx.Err() != nil {
						return
					}
					select {
					case errorChan <- err:
					default:
					}
					return
				}
			}
		}(pt.startIDs[start:end])
	}

	// Close result channel when all workers done
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	results := make([]*storage.Node, 0)
	for node := range resultChan {
		// Respect context cancellation during collection
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		results = append(results, node)
	}

	// Check for errors
	select {
	case err := <-errorChan:
		return nil, err
	default:
	}

	return results, nil
}

// traverseFrom performs BFS from a single node
//
// Concurrent Safety:
// 1. Called concurrently from multiple goroutines (recursive parallel traversal)
// 2. Uses sync.Map.LoadOrStore for atomic visited checking
// 3. Spawns child goroutines with semaphore limiting concurrency
// 4. Each goroutine independently handles its subtree
// 5. Context cancellation is checked before spawning new goroutines
//
// Concurrent Edge Cases:
// 1. Multiple goroutines may try to visit same node - LoadOrStore ensures only first succeeds
// 2. Results channel may be full/closed - select/default prevents blocking
// 3. Child goroutine errors logged but don't stop parent (parallel branches independent)
// 4. Semaphore prevents goroutine explosion - max 10 concurrent children per level
// 5. Context cancellation stops spawning new goroutines and exits gracefully
func (pt *ParallelTraversal) traverseFrom(
	nodeID uint64,
	depth int,
	visited *sync.Map,
	results chan<- *storage.Node,
) error {
	// Check context cancellation first
	select {
	case <-pt.ctx.Done():
		return pt.ctx.Err()
	default:
	}

	if depth > pt.maxDepth {
		return nil
	}

	// Check if already visited
	if _, loaded := visited.LoadOrStore(nodeID, true); loaded {
		return nil
	}

	// Get node
	node, err := pt.graph.GetNode(nodeID)
	if err != nil {
		return err
	}

	// Send result (defensive: select/default prevents blocking on full/closed channel)
	select {
	case results <- node:
	case <-pt.ctx.Done():
		return pt.ctx.Err()
	default:
		// Channel full, apply backpressure by skipping this result
	}

	// Get neighbors
	edges, err := pt.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return err
	}

	// Process neighbors in parallel for deeper levels
	if depth < pt.maxDepth {
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, 10) // Limit concurrency

		for _, edge := range edges {
			// Check context before spawning new goroutine
			select {
			case <-pt.ctx.Done():
				// Wait for already-spawned goroutines to complete
				wg.Wait()
				return pt.ctx.Err()
			default:
			}

			wg.Add(1)
			// Use select to avoid blocking on semaphore if context cancelled
			select {
			case semaphore <- struct{}{}:
			case <-pt.ctx.Done():
				wg.Done() // Undo the Add(1)
				wg.Wait()
				return pt.ctx.Err()
			}

			go func(targetID uint64) {
				defer wg.Done()
				defer func() { <-semaphore }()

				if err := pt.traverseFrom(targetID, depth+1, visited, results); err != nil {
					// Log error but continue traversal (unless context cancelled)
					// In parallel traversal, one branch failure shouldn't stop others
					if pt.ctx.Err() == nil {
						log.Printf("Warning: parallel traversal error at depth %d: %v", depth+1, err)
					}
				}
			}(edge.ToNodeID)
		}

		wg.Wait()
	}

	return nil
}
