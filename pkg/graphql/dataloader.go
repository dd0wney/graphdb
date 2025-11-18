package graphql

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// DataLoaderConfig configures a DataLoader instance
type DataLoaderConfig struct {
	BatchSize int           // Maximum number of keys to batch together
	Wait      time.Duration // How long to wait before dispatching a batch
}

// BatchFunc is called with a batch of keys and returns results and errors
type BatchFunc func(ctx context.Context, keys []string) ([]interface{}, []error)

// DataLoader batches and caches requests
type DataLoader struct {
	batchFn BatchFunc
	config  *DataLoaderConfig

	// Caching
	cache   map[string]*cacheEntry
	cacheMu sync.RWMutex

	// Batching
	batch   []*loadRequest
	batchMu sync.Mutex
	timer   *time.Timer
}

type cacheEntry struct {
	value interface{}
	err   error
}

type loadRequest struct {
	key     string
	resultC chan *cacheEntry
}

// NewDataLoader creates a new DataLoader
func NewDataLoader(batchFn BatchFunc, config *DataLoaderConfig) *DataLoader {
	if config == nil {
		config = &DataLoaderConfig{
			BatchSize: 100,
			Wait:      1 * time.Millisecond,
		}
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.Wait <= 0 {
		config.Wait = 1 * time.Millisecond
	}

	return &DataLoader{
		batchFn: batchFn,
		config:  config,
		cache:   make(map[string]*cacheEntry),
	}
}

// Load loads a value for the given key, batching and caching as configured
func (dl *DataLoader) Load(ctx context.Context, key string) (interface{}, error) {
	// Check cache first
	dl.cacheMu.RLock()
	if entry, ok := dl.cache[key]; ok {
		dl.cacheMu.RUnlock()
		return entry.value, entry.err
	}
	dl.cacheMu.RUnlock()

	// Create a request
	req := &loadRequest{
		key:     key,
		resultC: make(chan *cacheEntry, 1),
	}

	// Add to batch
	dl.batchMu.Lock()
	dl.batch = append(dl.batch, req)
	batchLen := len(dl.batch)

	// If batch is full, dispatch immediately
	if batchLen >= dl.config.BatchSize {
		batch := dl.batch
		dl.batch = nil
		if dl.timer != nil {
			dl.timer.Stop()
			dl.timer = nil
		}
		dl.batchMu.Unlock()
		dl.dispatchBatch(ctx, batch)
	} else {
		// Start or reset timer
		if dl.timer == nil {
			dl.timer = time.AfterFunc(dl.config.Wait, func() {
				dl.batchMu.Lock()
				batch := dl.batch
				dl.batch = nil
				dl.timer = nil
				dl.batchMu.Unlock()
				if len(batch) > 0 {
					dl.dispatchBatch(context.Background(), batch)
				}
			})
		}
		dl.batchMu.Unlock()
	}

	// Wait for result
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case entry := <-req.resultC:
		return entry.value, entry.err
	}
}

// dispatchBatch executes the batch function and sends results to waiting requests
func (dl *DataLoader) dispatchBatch(ctx context.Context, batch []*loadRequest) {
	keys := make([]string, len(batch))
	for i, req := range batch {
		keys[i] = req.key
	}

	// Call batch function
	results, errors := dl.batchFn(ctx, keys)

	// Cache and send results
	for i, req := range batch {
		var entry *cacheEntry
		if i < len(results) {
			entry = &cacheEntry{
				value: results[i],
				err:   errors[i],
			}
		} else {
			entry = &cacheEntry{
				err: fmt.Errorf("batch function returned fewer results than keys"),
			}
		}

		// Cache the result
		dl.cacheMu.Lock()
		dl.cache[req.key] = entry
		dl.cacheMu.Unlock()

		// Send to waiting goroutine
		req.resultC <- entry
	}
}

// Prime adds a value to the cache without calling the batch function
func (dl *DataLoader) Prime(key string, value interface{}) {
	dl.cacheMu.Lock()
	defer dl.cacheMu.Unlock()
	dl.cache[key] = &cacheEntry{value: value}
}

// Clear removes a key from the cache
func (dl *DataLoader) Clear(key string) {
	dl.cacheMu.Lock()
	defer dl.cacheMu.Unlock()
	delete(dl.cache, key)
}

// ClearAll removes all keys from the cache
func (dl *DataLoader) ClearAll() {
	dl.cacheMu.Lock()
	defer dl.cacheMu.Unlock()
	dl.cache = make(map[string]*cacheEntry)
}

// NewNodeDataLoader creates a DataLoader for loading nodes by ID
func NewNodeDataLoader(gs *storage.GraphStorage) *DataLoader {
	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		results := make([]interface{}, len(keys))
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

// NewOutgoingEdgesDataLoader creates a DataLoader for loading outgoing edges
func NewOutgoingEdgesDataLoader(gs *storage.GraphStorage) *DataLoader {
	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		results := make([]interface{}, len(keys))
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
	batchFn := func(ctx context.Context, keys []string) ([]interface{}, []error) {
		results := make([]interface{}, len(keys))
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
