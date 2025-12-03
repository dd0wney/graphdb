package graphql

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// NewNodeDataLoader creates a DataLoader for loading nodes by ID
func NewNodeDataLoader(gs *storage.GraphStorage) *DataLoader {
	batchFn := func(ctx context.Context, keys []string) ([]any, []error) {
		results := make([]any, len(keys))
		errors := make([]error, len(keys))

		for i, key := range keys {
			nodeID, err := strconv.ParseUint(key, 10, 64)
			if err != nil {
				errors[i] = fmt.Errorf("invalid node ID: %s", key)
				continue
			}

			node, err := gs.GetNode(nodeID)
			if err != nil {
				errors[i] = err
			} else {
				results[i] = node
			}
		}

		return results, errors
	}

	return NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})
}
