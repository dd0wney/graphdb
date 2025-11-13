package storage

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/lsm"
)

// EdgeStore provides disk-backed storage for adjacency lists using LSM
type EdgeStore struct {
	lsm       *lsm.LSMStorage
	cache     *EdgeCache
	mu        sync.RWMutex // Protects concurrent access
	cacheSize int
}

// NewEdgeStore creates a new disk-backed edge storage
func NewEdgeStore(dataDir string, cacheSize int) (*EdgeStore, error) {
	// Create LSM storage for edges
	lsmPath := filepath.Join(dataDir, "edges-lsm")
	lsmOpts := lsm.LSMOptions{
		DataDir:              lsmPath,
		MemTableSize:         4 * 1024 * 1024, // 4MB memtable
		CompactionStrategy:   lsm.DefaultLeveledCompaction(),
		EnableAutoCompaction: true,
	}

	lsmStore, err := lsm.NewLSMStorage(lsmOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create LSM storage: %w", err)
	}

	// Create LRU cache
	cache := NewEdgeCache(cacheSize)

	return &EdgeStore{
		lsm:       lsmStore,
		cache:     cache,
		cacheSize: cacheSize,
	}, nil
}

// StoreOutgoingEdges stores the outgoing edge list for a node
func (es *EdgeStore) StoreOutgoingEdges(nodeID uint64, edges []uint64) error {
	key := fmt.Sprintf("edges:out:%d", nodeID)

	// Compress edge list
	compressed := NewCompressedEdgeList(edges)

	// Serialize
	data, err := es.serializeEdgeList(compressed)
	if err != nil {
		return fmt.Errorf("failed to serialize edge list: %w", err)
	}

	// Store in LSM
	es.mu.Lock()
	err = es.lsm.Put([]byte(key), data)
	es.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to write to LSM: %w", err)
	}

	// Update cache
	es.cache.Put(key, compressed)

	return nil
}

// GetOutgoingEdges retrieves the outgoing edge list for a node
func (es *EdgeStore) GetOutgoingEdges(nodeID uint64) ([]uint64, error) {
	key := fmt.Sprintf("edges:out:%d", nodeID)

	// Check cache first
	if cached := es.cache.Get(key); cached != nil {
		return cached.Decompress(), nil
	}

	// Cache miss - load from LSM
	es.mu.RLock()
	data, found := es.lsm.Get([]byte(key))
	es.mu.RUnlock()

	if !found {
		// Key not found - return empty list
		return []uint64{}, nil
	}

	// Deserialize
	compressed, err := es.deserializeEdgeList(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize edge list: %w", err)
	}

	// Add to cache
	es.cache.Put(key, compressed)

	return compressed.Decompress(), nil
}

// StoreIncomingEdges stores the incoming edge list for a node
func (es *EdgeStore) StoreIncomingEdges(nodeID uint64, edges []uint64) error {
	key := fmt.Sprintf("edges:in:%d", nodeID)

	// Compress edge list
	compressed := NewCompressedEdgeList(edges)

	// Serialize
	data, err := es.serializeEdgeList(compressed)
	if err != nil {
		return fmt.Errorf("failed to serialize edge list: %w", err)
	}

	// Store in LSM
	es.mu.Lock()
	err = es.lsm.Put([]byte(key), data)
	es.mu.Unlock()

	if err != nil {
		return fmt.Errorf("failed to write to LSM: %w", err)
	}

	// Update cache
	es.cache.Put(key, compressed)

	return nil
}

// GetIncomingEdges retrieves the incoming edge list for a node
func (es *EdgeStore) GetIncomingEdges(nodeID uint64) ([]uint64, error) {
	key := fmt.Sprintf("edges:in:%d", nodeID)

	// Check cache first
	if cached := es.cache.Get(key); cached != nil {
		return cached.Decompress(), nil
	}

	// Cache miss - load from LSM
	es.mu.RLock()
	data, found := es.lsm.Get([]byte(key))
	es.mu.RUnlock()

	if !found {
		// Key not found - return empty list
		return []uint64{}, nil
	}

	// Deserialize
	compressed, err := es.deserializeEdgeList(data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize edge list: %w", err)
	}

	// Add to cache
	es.cache.Put(key, compressed)

	return compressed.Decompress(), nil
}

// Close closes the edge store and flushes all data
func (es *EdgeStore) Close() error {
	if es.lsm != nil {
		return es.lsm.Close()
	}
	return nil
}

// serializeEdgeList serializes a CompressedEdgeList to bytes using gob encoding
func (es *EdgeStore) serializeEdgeList(compressed *CompressedEdgeList) ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)

	// Encode the struct
	err := enc.Encode(compressed)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// deserializeEdgeList deserializes bytes back to CompressedEdgeList
func (es *EdgeStore) deserializeEdgeList(data []byte) (*CompressedEdgeList, error) {
	buf := bytes.NewBuffer(data)
	dec := gob.NewDecoder(buf)

	var compressed CompressedEdgeList
	err := dec.Decode(&compressed)
	if err != nil {
		return nil, err
	}

	return &compressed, nil
}
