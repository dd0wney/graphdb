package query

import (
	"context"
	"sync"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

// ResultStream provides streaming query results
type ResultStream struct {
	ch     chan *storage.Node
	errCh  chan error
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

// NewResultStream creates a new result stream
func NewResultStream(bufferSize int) *ResultStream {
	ctx, cancel := context.WithCancel(context.Background())

	return &ResultStream{
		ch:     make(chan *storage.Node, bufferSize),
		errCh:  make(chan error, 1),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Next returns the next result or error
func (rs *ResultStream) Next() (*storage.Node, error) {
	select {
	case node, ok := <-rs.ch:
		if !ok {
			// Check for error
			select {
			case err := <-rs.errCh:
				return nil, err
			default:
				return nil, nil // End of stream
			}
		}
		return node, nil

	case err := <-rs.errCh:
		return nil, err

	case <-rs.ctx.Done():
		return nil, rs.ctx.Err()
	}
}

// Send sends a result to the stream
func (rs *ResultStream) Send(node *storage.Node) bool {
	select {
	case rs.ch <- node:
		return true
	case <-rs.ctx.Done():
		return false
	}
}

// SendError sends an error and closes the stream
func (rs *ResultStream) SendError(err error) {
	select {
	case rs.errCh <- err:
	default:
	}
	rs.Close()
}

// Close closes the result stream
func (rs *ResultStream) Close() {
	rs.once.Do(func() {
		close(rs.ch)
		rs.cancel()
	})
}

// StreamingQuery executes queries with streaming results
type StreamingQuery struct {
	graph *storage.GraphStorage
}

// NewStreamingQuery creates a streaming query executor
func NewStreamingQuery(graph *storage.GraphStorage) *StreamingQuery {
	return &StreamingQuery{graph: graph}
}

// StreamNodes streams all nodes matching a filter
func (sq *StreamingQuery) StreamNodes(
	filter func(*storage.Node) bool,
) *ResultStream {
	stream := NewResultStream(100)

	go func() {
		defer stream.Close()

		stats := sq.graph.GetStatistics()
		nodeCount := int(stats.NodeCount)

		for nodeID := uint64(1); nodeID <= uint64(nodeCount); nodeID++ {
			node, err := sq.graph.GetNode(nodeID)
			if err != nil {
				continue
			}

			if filter == nil || filter(node) {
				if !stream.Send(node) {
					return // Stream cancelled
				}
			}
		}
	}()

	return stream
}

// StreamTraversal streams nodes discovered during traversal
func (sq *StreamingQuery) StreamTraversal(
	startID uint64,
	maxDepth int,
) *ResultStream {
	stream := NewResultStream(100)

	go func() {
		defer stream.Close()

		visited := make(map[uint64]bool)
		sq.streamTraverseFrom(startID, 0, maxDepth, visited, stream)
	}()

	return stream
}

// streamTraverseFrom performs streaming BFS
func (sq *StreamingQuery) streamTraverseFrom(
	nodeID uint64,
	depth int,
	maxDepth int,
	visited map[uint64]bool,
	stream *ResultStream,
) {
	if depth > maxDepth || visited[nodeID] {
		return
	}

	visited[nodeID] = true

	node, err := sq.graph.GetNode(nodeID)
	if err != nil {
		return
	}

	if !stream.Send(node) {
		return // Stream cancelled
	}

	edges, err := sq.graph.GetOutgoingEdges(nodeID)
	if err != nil {
		return
	}

	for _, edge := range edges {
		sq.streamTraverseFrom(edge.ToNodeID, depth+1, maxDepth, visited, stream)
	}
}

// BatchProcessor processes query results in batches
type BatchProcessor struct {
	batchSize int
	processor func([]*storage.Node) error
}

// NewBatchProcessor creates a batch processor
func NewBatchProcessor(batchSize int, processor func([]*storage.Node) error) *BatchProcessor {
	return &BatchProcessor{
		batchSize: batchSize,
		processor: processor,
	}
}

// Process processes a result stream in batches
func (bp *BatchProcessor) Process(stream *ResultStream) error {
	batch := make([]*storage.Node, 0, bp.batchSize)

	for {
		node, err := stream.Next()
		if err != nil {
			return err
		}

		if node == nil {
			// End of stream - process final batch
			if len(batch) > 0 {
				if err := bp.processor(batch); err != nil {
					return err
				}
			}
			break
		}

		batch = append(batch, node)

		if len(batch) >= bp.batchSize {
			if err := bp.processor(batch); err != nil {
				return err
			}
			batch = batch[:0] // Reset batch
		}
	}

	return nil
}

// PipelineStage represents a stage in a query pipeline
type PipelineStage func(*storage.Node) (*storage.Node, bool)

// QueryPipeline chains multiple processing stages
type QueryPipeline struct {
	stages []PipelineStage
}

// NewQueryPipeline creates a new query pipeline
func NewQueryPipeline() *QueryPipeline {
	return &QueryPipeline{
		stages: make([]PipelineStage, 0),
	}
}

// AddStage adds a processing stage
func (qp *QueryPipeline) AddStage(stage PipelineStage) *QueryPipeline {
	qp.stages = append(qp.stages, stage)
	return qp
}

// Filter adds a filter stage
func (qp *QueryPipeline) Filter(predicate func(*storage.Node) bool) *QueryPipeline {
	return qp.AddStage(func(node *storage.Node) (*storage.Node, bool) {
		if predicate(node) {
			return node, true
		}
		return nil, false
	})
}

// Map adds a transformation stage
func (qp *QueryPipeline) Map(transform func(*storage.Node) *storage.Node) *QueryPipeline {
	return qp.AddStage(func(node *storage.Node) (*storage.Node, bool) {
		return transform(node), true
	})
}

// Execute executes the pipeline on a stream
func (qp *QueryPipeline) Execute(input *ResultStream) *ResultStream {
	output := NewResultStream(100)

	go func() {
		defer output.Close()

		for {
			node, err := input.Next()
			if err != nil {
				output.SendError(err)
				return
			}

			if node == nil {
				break // End of stream
			}

			// Apply all stages
			current := node
			keep := true

			for _, stage := range qp.stages {
				current, keep = stage(current)
				if !keep {
					break
				}
			}

			if keep && current != nil {
				if !output.Send(current) {
					return // Output cancelled
				}
			}
		}
	}()

	return output
}

// ParallelPipeline executes pipeline stages in parallel
type ParallelPipeline struct {
	pipeline   *QueryPipeline
	workers    int
	bufferSize int
}

// NewParallelPipeline creates a parallel pipeline
func NewParallelPipeline(workers int) *ParallelPipeline {
	return &ParallelPipeline{
		pipeline:   NewQueryPipeline(),
		workers:    workers,
		bufferSize: 1000,
	}
}

// AddStage adds a stage to the pipeline
func (pp *ParallelPipeline) AddStage(stage PipelineStage) *ParallelPipeline {
	pp.pipeline.AddStage(stage)
	return pp
}

// Execute executes the pipeline with parallel workers
func (pp *ParallelPipeline) Execute(input *ResultStream) *ResultStream {
	output := NewResultStream(pp.bufferSize)

	var wg sync.WaitGroup
	workChan := make(chan *storage.Node, pp.bufferSize)

	// Start workers
	for i := 0; i < pp.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for node := range workChan {
				// Apply pipeline stages
				current := node
				keep := true

				for _, stage := range pp.pipeline.stages {
					current, keep = stage(current)
					if !keep {
						break
					}
				}

				if keep && current != nil {
					if !output.Send(current) {
						return
					}
				}
			}
		}()
	}

	// Feed workers
	go func() {
		for {
			node, err := input.Next()
			if err != nil {
				output.SendError(err)
				break
			}

			if node == nil {
				break
			}

			workChan <- node
		}

		close(workChan)
		wg.Wait()
		output.Close()
	}()

	return output
}

// Collect collects all results from a stream into a slice
func Collect(stream *ResultStream) ([]*storage.Node, error) {
	results := make([]*storage.Node, 0)

	for {
		node, err := stream.Next()
		if err != nil {
			return nil, err
		}

		if node == nil {
			break
		}

		results = append(results, node)
	}

	return results, nil
}

// Count counts results in a stream
func Count(stream *ResultStream) (int, error) {
	count := 0

	for {
		node, err := stream.Next()
		if err != nil {
			return 0, err
		}

		if node == nil {
			break
		}

		count++
	}

	return count, nil
}
