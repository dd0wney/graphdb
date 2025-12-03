package query

import (
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
			// Respect context cancellation
			select {
			case <-output.ctx.Done():
				return
			default:
			}

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
				// Respect context cancellation
				select {
				case <-output.ctx.Done():
					return
				default:
				}

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
		defer func() {
			close(workChan)
			wg.Wait()
			output.Close()
		}()

		for {
			// Respect context cancellation
			select {
			case <-output.ctx.Done():
				return
			default:
			}

			node, err := input.Next()
			if err != nil {
				output.SendError(err)
				return
			}

			if node == nil {
				return
			}

			select {
			case workChan <- node:
			case <-output.ctx.Done():
				return
			}
		}
	}()

	return output
}
