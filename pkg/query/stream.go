package query

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
