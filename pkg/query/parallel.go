package query

import (
	"context"
	"log"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// WorkerPool manages a pool of worker goroutines for parallel query execution
type WorkerPool struct {
	workers   int
	taskQueue chan Task
	results   chan TaskResult
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// Statistics
	tasksProcessed int64
	tasksActive    int64
}

// Task represents a unit of work
type Task interface {
	Execute(graph *storage.GraphStorage) (interface{}, error)
	ID() string
}

// TaskResult contains the result of a task execution
type TaskResult struct {
	TaskID string
	Result interface{}
	Error  error
}

// NewWorkerPool creates a worker pool with specified number of workers
func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		workers:   workers,
		taskQueue: make(chan Task, workers*10), // Buffered queue
		results:   make(chan TaskResult, workers*10),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start starts the worker pool
func (wp *WorkerPool) Start(graph *storage.GraphStorage) {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker(i, graph)
	}
}

// worker processes tasks from the queue
func (wp *WorkerPool) worker(id int, graph *storage.GraphStorage) {
	defer wp.wg.Done()

	for {
		select {
		case task, ok := <-wp.taskQueue:
			if !ok {
				return
			}

			atomic.AddInt64(&wp.tasksActive, 1)
			result, err := task.Execute(graph)
			atomic.AddInt64(&wp.tasksActive, -1)
			atomic.AddInt64(&wp.tasksProcessed, 1)

			select {
			case wp.results <- TaskResult{
				TaskID: task.ID(),
				Result: result,
				Error:  err,
			}:
			case <-wp.ctx.Done():
				return
			}

		case <-wp.ctx.Done():
			return
		}
	}
}

// Submit submits a task for execution
func (wp *WorkerPool) Submit(task Task) error {
	select {
	case wp.taskQueue <- task:
		return nil
	case <-wp.ctx.Done():
		return wp.ctx.Err()
	}
}

// Results returns the results channel
func (wp *WorkerPool) Results() <-chan TaskResult {
	return wp.results
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop() {
	close(wp.taskQueue)
	wp.wg.Wait()
	close(wp.results)
	wp.cancel()
}

// Stats returns pool statistics
func (wp *WorkerPool) Stats() (processed, active int64) {
	return atomic.LoadInt64(&wp.tasksProcessed), atomic.LoadInt64(&wp.tasksActive)
}

// ParallelTraversal performs parallel BFS traversal
type ParallelTraversal struct {
	graph     *storage.GraphStorage
	startIDs  []uint64
	maxDepth  int
	batchSize int
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
func (pt *ParallelTraversal) Execute() ([]*storage.Node, error) {
	numWorkers := runtime.NumCPU()
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
				if err := pt.traverseFrom(nodeID, 0, &visited, resultChan); err != nil {
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
func (pt *ParallelTraversal) traverseFrom(
	nodeID uint64,
	depth int,
	visited *sync.Map,
	results chan<- *storage.Node,
) error {
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

	// Send result
	select {
	case results <- node:
	default:
		// Channel full, apply backpressure
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
			wg.Add(1)
			semaphore <- struct{}{}

			go func(targetID uint64) {
				defer wg.Done()
				defer func() { <-semaphore }()

				if err := pt.traverseFrom(targetID, depth+1, visited, results); err != nil {
					// Log error but continue traversal
					// In parallel traversal, one branch failure shouldn't stop others
					log.Printf("Warning: parallel traversal error at depth %d: %v", depth+1, err)
				}
			}(edge.ToNodeID)
		}

		wg.Wait()
	}

	return nil
}

// ParallelAggregation performs parallel aggregation operations
type ParallelAggregation struct {
	graph *storage.GraphStorage
}

// NewParallelAggregation creates a parallel aggregation engine
func NewParallelAggregation(graph *storage.GraphStorage) *ParallelAggregation {
	return &ParallelAggregation{graph: graph}
}

// CountNodesByLabel counts nodes with a label in parallel
func (pa *ParallelAggregation) CountNodesByLabel(label string) (int, error) {
	stats := pa.graph.GetStatistics()
	numWorkers := runtime.NumCPU()

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

			resultChan <- count
		}(start, end)
	}

	// Wait and collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	total := 0
	for count := range resultChan {
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
func (pa *ParallelAggregation) AggregateProperty(
	propertyKey string,
	aggregateFunc func(values []interface{}) interface{},
) (interface{}, error) {
	stats := pa.graph.GetStatistics()
	numWorkers := runtime.NumCPU()

	// Divide work among workers (overflow-safe - use uint64 directly)
	var chunkSize uint64
	if numWorkers > 0 {
		chunkSize = (stats.NodeCount + uint64(numWorkers) - 1) / uint64(numWorkers)
	} else {
		chunkSize = stats.NodeCount
	}

	resultChan := make(chan []interface{}, numWorkers)

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

			values := make([]interface{}, 0)
			for nodeID := startID; nodeID <= endID; nodeID++ {
				node, err := pa.graph.GetNode(nodeID)
				if err != nil {
					continue
				}

				if value, exists := node.Properties[propertyKey]; exists {
					values = append(values, value.Data)
				}
			}

			resultChan <- values
		}(start, end)
	}

	// Wait and collect results
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	allValues := make([]interface{}, 0)
	for values := range resultChan {
		allValues = append(allValues, values...)
	}

	return aggregateFunc(allValues), nil
}

// ParallelPathFinder finds paths in parallel
type ParallelPathFinder struct {
	graph *storage.GraphStorage
}

// NewParallelPathFinder creates a parallel path finder
func NewParallelPathFinder(graph *storage.GraphStorage) *ParallelPathFinder {
	return &ParallelPathFinder{graph: graph}
}

// FindAllPaths finds all paths between multiple pairs of nodes in parallel
func (ppf *ParallelPathFinder) FindAllPaths(
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
		wg.Add(1)
		semaphore <- struct{}{}

		go func(start, end uint64) {
			defer wg.Done()
			defer func() { <-semaphore }()

			path := ppf.findPath(start, end, maxDepth)
			if path != nil {
				resultChan <- path
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
