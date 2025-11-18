package storage

import (
	"fmt"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// VectorIndex manages vector search indexes for node properties
type VectorIndex struct {
	mu      sync.RWMutex
	indexes map[string]*vector.HNSWIndex // property name -> HNSW index
}

// NewVectorIndex creates a new vector index manager
func NewVectorIndex() *VectorIndex {
	return &VectorIndex{
		indexes: make(map[string]*vector.HNSWIndex),
	}
}

// CreateIndex creates a new HNSW index for a property
func (vi *VectorIndex) CreateIndex(
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if _, exists := vi.indexes[propertyName]; exists {
		return fmt.Errorf("vector index already exists for property %s", propertyName)
	}

	index, err := vector.NewHNSWIndex(dimensions, m, efConstruction, metric)
	if err != nil {
		return fmt.Errorf("failed to create HNSW index: %w", err)
	}

	vi.indexes[propertyName] = index
	return nil
}

// AddVector adds a vector to the index
func (vi *VectorIndex) AddVector(propertyName string, nodeID uint64, vec []float32) error {
	vi.mu.RLock()
	index, exists := vi.indexes[propertyName]
	vi.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	return index.Insert(nodeID, vec)
}

// Search performs k-NN search on a vector index
func (vi *VectorIndex) Search(
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	vi.mu.RLock()
	index, exists := vi.indexes[propertyName]
	vi.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	return index.Search(query, k, ef)
}

// RemoveVector removes a vector from the index
func (vi *VectorIndex) RemoveVector(propertyName string, nodeID uint64) error {
	vi.mu.RLock()
	index, exists := vi.indexes[propertyName]
	vi.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	return index.Delete(nodeID)
}

// DropIndex removes a vector index
func (vi *VectorIndex) DropIndex(propertyName string) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	if _, exists := vi.indexes[propertyName]; !exists {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	delete(vi.indexes, propertyName)
	return nil
}

// HasIndex checks if an index exists for a property
func (vi *VectorIndex) HasIndex(propertyName string) bool {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	_, exists := vi.indexes[propertyName]
	return exists
}

// ListIndexes returns names of all indexed properties
func (vi *VectorIndex) ListIndexes() []string {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	names := make([]string, 0, len(vi.indexes))
	for name := range vi.indexes {
		names = append(names, name)
	}
	return names
}
