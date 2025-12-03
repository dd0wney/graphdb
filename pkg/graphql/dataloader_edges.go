package graphql

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// NewOutgoingEdgesDataLoader creates a DataLoader for loading outgoing edges
func NewOutgoingEdgesDataLoader(gs *storage.GraphStorage) *DataLoader {
	batchFn := func(ctx context.Context, keys []string) ([]any, []error) {
		results := make([]any, len(keys))
		errors := make([]error, len(keys))

		// Collect all node IDs
		nodeIDs := make([]uint64, 0, len(keys))
		keyToIndex := make(map[uint64]int)

		for i, key := range keys {
			nodeID, err := strconv.ParseUint(key, 10, 64)
			if err != nil {
				errors[i] = fmt.Errorf("invalid node ID: %s", key)
				continue
			}
			nodeIDs = append(nodeIDs, nodeID)
			keyToIndex[nodeID] = i
		}

		// Batch load all outgoing edges for all nodes
		// This is more efficient than loading one by one
		edgesByNode := make(map[uint64][]*storage.Edge)
		for _, nodeID := range nodeIDs {
			edges, err := gs.GetOutgoingEdges(nodeID)
			if err != nil {
				idx := keyToIndex[nodeID]
				errors[idx] = err
			} else {
				edgesByNode[nodeID] = edges
			}
		}

		// Populate results
		for nodeID, idx := range keyToIndex {
			if errors[idx] == nil {
				results[idx] = edgesByNode[nodeID]
			}
		}

		return results, errors
	}

	return NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})
}

// NewIncomingEdgesDataLoader creates a DataLoader for loading incoming edges
func NewIncomingEdgesDataLoader(gs *storage.GraphStorage) *DataLoader {
	batchFn := func(ctx context.Context, keys []string) ([]any, []error) {
		results := make([]any, len(keys))
		errors := make([]error, len(keys))

		nodeIDs := make([]uint64, 0, len(keys))
		keyToIndex := make(map[uint64]int)

		for i, key := range keys {
			nodeID, err := strconv.ParseUint(key, 10, 64)
			if err != nil {
				errors[i] = fmt.Errorf("invalid node ID: %s", key)
				continue
			}
			nodeIDs = append(nodeIDs, nodeID)
			keyToIndex[nodeID] = i
		}

		edgesByNode := make(map[uint64][]*storage.Edge)
		for _, nodeID := range nodeIDs {
			edges, err := gs.GetIncomingEdges(nodeID)
			if err != nil {
				idx := keyToIndex[nodeID]
				errors[idx] = err
			} else {
				edgesByNode[nodeID] = edges
			}
		}

		for nodeID, idx := range keyToIndex {
			if errors[idx] == nil {
				results[idx] = edgesByNode[nodeID]
			}
		}

		return results, errors
	}

	return NewDataLoader(batchFn, &DataLoaderConfig{
		BatchSize: 100,
		Wait:      1 * time.Millisecond,
	})
}
