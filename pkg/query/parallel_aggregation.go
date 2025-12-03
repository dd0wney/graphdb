package query

import (
	"context"
	"runtime"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ParallelAggregation performs parallel aggregation operations
type ParallelAggregation struct {
	graph *storage.GraphStorage
}

// NewParallelAggregation creates a parallel aggregation engine
func NewParallelAggregation(graph *storage.GraphStorage) *ParallelAggregation {
	return &ParallelAggregation{graph: graph}
}

// CountNodesByLabel counts nodes with a label in parallel
//
// Concurrent Safety:
// 1. Divides node ID range among workers for parallel scanning
// 2. Each worker has independent count accumulator (no shared state)
// 3. Workers send results to buffered channel (size = numWorkers)
// 4. Main goroutine waits via WaitGroup before closing channels
//
// Concurrent Edge Cases:
// 1. Workers may encounter deleted nodes - silently skipped (continue on error)
// 2. No synchronization needed between workers - non-overlapping ID ranges
// 3. Channel buffer prevents workers from blocking on result send
func (pa *ParallelAggregation) CountNodesByLabel(ctx context.Context, label string) (int, error) {
	stats := pa.graph.GetStatistics()
	numWorkers := runtime.NumCPU()
	// Defensive: ensure minimum of 1 worker
	if numWorkers <= 0 {
		numWorkers = 1
	}

	// Divide work among workers (overflow-safe - use uint64 directly)
	var chunkSize uint64
	if numWorkers > 0 {
		chunkSize = (stats.NodeCount + uint64(numWorkers) - 1) / uint64(numWorkers)
	} else {
		chunkSize = stats.NodeCount
	}

	resultChan := make(chan int, numWorkers)
	errorChan := make(chan error, 1)

	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		start := uint64(i)*chunkSize + 1
		end := uint64(i+1) * chunkSize
		if end > stats.NodeCount {
			end = stats.NodeCount
		}

		wg.Add(1)
		go func(startID, endID uint64) {
			defer wg.Done()

			count := 0
			for nodeID := startID; nodeID <= endID; nodeID++ {
				// Respect context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				node, err := pa.graph.GetNode(nodeID)
				if err != nil {
					continue
				}

				// Check if node has label
				for _, nodeLabel := range node.Labels {
					if nodeLabel == label {
						count++
						break
					}
				}
			}

			select {
			case resultChan <- count:
			case <-ctx.Done():
				return
			}
		}(start, end)
	}

	// Wait and collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	total := 0
	for count := range resultChan {
		// Respect context cancellation during collection
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		total += count
	}

	// Check for errors
	select {
	case err := <-errorChan:
		return 0, err
	default:
	}

	return total, nil
}

// AggregateProperty performs parallel property aggregation
//
// Concurrent Safety:
// 1. Divides node ID range among workers for parallel scanning
// 2. Each worker builds independent value slice (no shared state)
// 3. Workers send value slices to buffered channel (size = numWorkers)
// 4. Main goroutine aggregates after all workers complete
//
// Concurrent Edge Cases:
// 1. Workers may encounter deleted nodes - silently skipped (continue on error)
// 2. Workers may encounter nodes without the property - only existing values collected
// 3. No synchronization needed between workers - non-overlapping ID ranges
// 4. Final aggregation happens sequentially after parallel collection
func (pa *ParallelAggregation) AggregateProperty(
	ctx context.Context,
	propertyKey string,
	aggregateFunc func(values []any) any,
) (any, error) {
	stats := pa.graph.GetStatistics()
	numWorkers := runtime.NumCPU()
	// Defensive: ensure minimum of 1 worker
	if numWorkers <= 0 {
		numWorkers = 1
	}

	// Divide work among workers (overflow-safe - use uint64 directly)
	var chunkSize uint64
	if numWorkers > 0 {
		chunkSize = (stats.NodeCount + uint64(numWorkers) - 1) / uint64(numWorkers)
	} else {
		chunkSize = stats.NodeCount
	}

	resultChan := make(chan []any, numWorkers)

	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		start := uint64(i)*chunkSize + 1
		end := uint64(i+1) * chunkSize
		if end > stats.NodeCount {
			end = stats.NodeCount
		}

		wg.Add(1)
		go func(startID, endID uint64) {
			defer wg.Done()

			values := make([]any, 0)
			for nodeID := startID; nodeID <= endID; nodeID++ {
				// Respect context cancellation
				select {
				case <-ctx.Done():
					return
				default:
				}

				node, err := pa.graph.GetNode(nodeID)
				if err != nil {
					continue
				}

				if value, exists := node.Properties[propertyKey]; exists {
					values = append(values, value.Data)
				}
			}

			select {
			case resultChan <- values:
			case <-ctx.Done():
				return
			}
		}(start, end)
	}

	// Wait and collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	allValues := make([]any, 0)
	for values := range resultChan {
		// Respect context cancellation during collection
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		allValues = append(allValues, values...)
	}

	return aggregateFunc(allValues), nil
}
