package storage

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/lsm"
)

// makeEdgeStoreKey generates edge storage key more efficiently than fmt.Sprintf
// Reduces allocations in hot path
func makeEdgeStoreKey(direction string, nodeID uint64) string {
	// Pre-allocate buffer: "edges:" (6) + "out"/"in" (2-3) + ":" (1) + digits (~10) = ~20 bytes
	buf := make([]byte, 0, 24)
	buf = append(buf, "edges:"...)
	buf = append(buf, direction...)
	buf = append(buf, ':')
	buf = strconv.AppendUint(buf, nodeID, 10)
	return string(buf)
}

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
		MemTableSize:         64 * 1024 * 1024, // 64MB memtable (reduces SSTable count 16x)
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
	key := makeEdgeStoreKey("out", nodeID)

	// Compress edge list
	compressed, err := NewCompressedEdgeList(edges)
	if err != nil {
		return fmt.Errorf("failed to compress edge list: %w", err)
	}

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
	key := makeEdgeStoreKey("out", nodeID)

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
	key := makeEdgeStoreKey("in", nodeID)

	// Compress edge list
	compressed, err := NewCompressedEdgeList(edges)
	if err != nil {
		return fmt.Errorf("failed to compress edge list: %w", err)
	}

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
	key := makeEdgeStoreKey("in", nodeID)

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

// Sync forces a flush of pending writes to disk
func (es *EdgeStore) Sync() error {
	if es.lsm != nil {
		return es.lsm.Sync()
	}
	return nil
}

// Close closes the edge store and flushes all data
func (es *EdgeStore) Close() error {
	if es.lsm != nil {
		return es.lsm.Close()
	}
	return nil
}

// serializeEdgeList serializes a CompressedEdgeList to bytes using binary format
// Format: [BaseNodeID:8][EdgeCount:4][DeltasLen:4][Deltas:N]
func (es *EdgeStore) serializeEdgeList(compressed *CompressedEdgeList) ([]byte, error) {
	deltasLen := len(compressed.Deltas)
	buf := make([]byte, 8+4+4+deltasLen)

	binary.LittleEndian.PutUint64(buf[0:8], compressed.BaseNodeID)
	binary.LittleEndian.PutUint32(buf[8:12], uint32(compressed.EdgeCount))
	binary.LittleEndian.PutUint32(buf[12:16], uint32(deltasLen))
	copy(buf[16:], compressed.Deltas)

	return buf, nil
}

// deserializeEdgeList deserializes bytes back to CompressedEdgeList
func (es *EdgeStore) deserializeEdgeList(data []byte) (*CompressedEdgeList, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("invalid data: too short")
	}

	baseNodeID := binary.LittleEndian.Uint64(data[0:8])
	edgeCount := int(binary.LittleEndian.Uint32(data[8:12]))
	deltasLen := int(binary.LittleEndian.Uint32(data[12:16]))

	if len(data) < 16+deltasLen {
		return nil, fmt.Errorf("invalid data: deltas truncated")
	}

	// Zero-copy: share backing array (safe since data comes from LSM)
	deltas := data[16 : 16+deltasLen]

	return &CompressedEdgeList{
		BaseNodeID: baseNodeID,
		Deltas:     deltas,
		EdgeCount:  edgeCount,
	}, nil
}
