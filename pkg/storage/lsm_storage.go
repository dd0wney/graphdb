package storage

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"github.com/dd0wney/cluso-graphdb/pkg/lsm"
)

// LSMGraphStorage is a disk-backed graph storage using LSM trees
type LSMGraphStorage struct {
	lsm *lsm.LSMStorage

	// Counters
	nextNodeID uint64
	nextEdgeID uint64

	mu sync.RWMutex
}

// NewLSMGraphStorage creates a new LSM-backed graph storage
func NewLSMGraphStorage(dataDir string) (*LSMGraphStorage, error) {
	opts := lsm.DefaultLSMOptions(dataDir)
	opts.MemTableSize = 16 * 1024 * 1024 // 16MB MemTable

	storage, err := lsm.NewLSMStorage(opts)
	if err != nil {
		return nil, err
	}

	gs := &LSMGraphStorage{
		lsm:        storage,
		nextNodeID: 1,
		nextEdgeID: 1,
	}

	// Load counters from disk
	if err := gs.loadCounters(); err != nil {
		return nil, err
	}

	return gs, nil
}

// loadCounters loads node/edge counters from LSM
func (gs *LSMGraphStorage) loadCounters() error {
	// Load next node ID
	if value, ok := gs.lsm.Get(makeMetaKey("next_node_id")); ok {
		gs.nextNodeID = binary.BigEndian.Uint64(value)
	}

	// Load next edge ID
	if value, ok := gs.lsm.Get(makeMetaKey("next_edge_id")); ok {
		gs.nextEdgeID = binary.BigEndian.Uint64(value)
	}

	return nil
}

// saveCounters persists node/edge counters to LSM
func (gs *LSMGraphStorage) saveCounters() error {
	// Save next node ID
	nodeValue := make([]byte, 8)
	binary.BigEndian.PutUint64(nodeValue, gs.nextNodeID)
	if err := gs.lsm.Put(makeMetaKey("next_node_id"), nodeValue); err != nil {
		return err
	}

	// Save next edge ID
	edgeValue := make([]byte, 8)
	binary.BigEndian.PutUint64(edgeValue, gs.nextEdgeID)
	if err := gs.lsm.Put(makeMetaKey("next_edge_id"), edgeValue); err != nil {
		return err
	}

	return nil
}

// GetStatistics returns graph statistics
func (gs *LSMGraphStorage) GetStatistics() Statistics {
	return Statistics{
		NodeCount: atomic.LoadUint64(&gs.nextNodeID) - 1,
		EdgeCount: atomic.LoadUint64(&gs.nextEdgeID) - 1,
	}
}

// Close closes the LSM storage
func (gs *LSMGraphStorage) Close() error {
	// Save counters before closing
	if err := gs.saveCounters(); err != nil {
		return err
	}
	return gs.lsm.Close()
}
