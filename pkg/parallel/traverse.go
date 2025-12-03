package parallel

import (
	"runtime"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ParallelTraverser performs parallel graph traversals
type ParallelTraverser struct {
	graph      *storage.GraphStorage
	workerPool *WorkerPool
	numWorkers int
}

// NewParallelTraverser creates a new parallel traverser
func NewParallelTraverser(graph *storage.GraphStorage, numWorkers int) (*ParallelTraverser, error) {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	pool, err := NewWorkerPool(numWorkers)
	if err != nil {
		return nil, err
	}

	return &ParallelTraverser{
		graph:      graph,
		workerPool: pool,
		numWorkers: numWorkers,
	}, nil
}

// Close closes the worker pool
func (pt *ParallelTraverser) Close() {
	pt.workerPool.Close()
}
