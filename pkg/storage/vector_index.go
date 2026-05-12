package storage

import (
	"fmt"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// VectorIndex manages vector search indexes for node properties, partitioned by tenant.
type VectorIndex struct {
	mu      sync.RWMutex
	indexes map[string]map[string]*vector.HNSWIndex // tenantID -> property name -> HNSW index
}

// NewVectorIndex creates a new multi-tenant vector index manager
func NewVectorIndex() *VectorIndex {
	return &VectorIndex{
		indexes: make(map[string]map[string]*vector.HNSWIndex),
	}
}

// CreateIndex creates a new HNSW index for a property within a specific tenant
func (vi *VectorIndex) CreateIndex(
	tenantID string,
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	tenantIndexes, exists := vi.indexes[tenantID]
	if !exists {
		tenantIndexes = make(map[string]*vector.HNSWIndex)
		vi.indexes[tenantID] = tenantIndexes
	}

	if _, exists := tenantIndexes[propertyName]; exists {
		return fmt.Errorf("vector index already exists for property %s in tenant %s", propertyName, tenantID)
	}

	index, err := vector.NewHNSWIndex(dimensions, m, efConstruction, metric)
	if err != nil {
		return fmt.Errorf("failed to create HNSW index: %w", err)
	}

	tenantIndexes[propertyName] = index
	return nil
}

// AddVector adds a vector to a tenant's specific index
func (vi *VectorIndex) AddVector(tenantID string, propertyName string, nodeID uint64, vec []float32) error {
	vi.mu.RLock()
	tenantIndexes, tenantExists := vi.indexes[tenantID]
	if !tenantExists {
		vi.mu.RUnlock()
		return fmt.Errorf("no vector indexes exist for tenant %s", tenantID)
	}
	index, exists := tenantIndexes[propertyName]
	vi.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no vector index exists for property %s in tenant %s", propertyName, tenantID)
	}

	return index.Insert(nodeID, vec)
}

// Search performs k-NN search on a tenant's vector index
func (vi *VectorIndex) Search(
	tenantID string,
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	vi.mu.RLock()
	tenantIndexes, tenantExists := vi.indexes[tenantID]
	if !tenantExists {
		vi.mu.RUnlock()
		return nil, fmt.Errorf("no vector indexes exist for tenant %s", tenantID)
	}
	index, exists := tenantIndexes[propertyName]
	vi.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no vector index exists for property %s in tenant %s", propertyName, tenantID)
	}

	return index.Search(query, k, ef)
}

// RemoveVector removes a vector from a tenant's index
func (vi *VectorIndex) RemoveVector(tenantID string, propertyName string, nodeID uint64) error {
	vi.mu.RLock()
	tenantIndexes, tenantExists := vi.indexes[tenantID]
	if !tenantExists {
		vi.mu.RUnlock()
		return nil // Safe default: tenant doesn't exist, so node isn't indexed
	}
	index, exists := tenantIndexes[propertyName]
	vi.mu.RUnlock()

	if !exists {
		return nil // Safe default: property isn't indexed for this tenant
	}

	return index.Delete(nodeID)
}

// DropIndex removes a vector index for a tenant
func (vi *VectorIndex) DropIndex(tenantID string, propertyName string) error {
	vi.mu.Lock()
	defer vi.mu.Unlock()

	tenantIndexes, exists := vi.indexes[tenantID]
	if !exists {
		return fmt.Errorf("no vector indexes exist for tenant %s", tenantID)
	}

	if _, exists := tenantIndexes[propertyName]; !exists {
		return fmt.Errorf("no vector index exists for property %s in tenant %s", propertyName, tenantID)
	}

	delete(tenantIndexes, propertyName)
	
	// Optional: if tenant has no more indexes, clean up the map
	if len(tenantIndexes) == 0 {
		delete(vi.indexes, tenantID)
	}
	
	return nil
}

// HasIndex checks if an index exists for a property within a tenant
func (vi *VectorIndex) HasIndex(tenantID string, propertyName string) bool {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	
	tenantIndexes, exists := vi.indexes[tenantID]
	if !exists {
		return false
	}
	_, exists = tenantIndexes[propertyName]
	return exists
}

// ListIndexes returns names of all indexed properties for a tenant
func (vi *VectorIndex) ListIndexes(tenantID string) []string {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	tenantIndexes, exists := vi.indexes[tenantID]
	if !exists {
		return []string{}
	}

	names := make([]string, 0, len(tenantIndexes))
	for name := range tenantIndexes {
		names = append(names, name)
	}
	return names
}

// GetIndexMetric returns the distance metric for a specific index in a tenant
func (vi *VectorIndex) GetIndexMetric(tenantID string, propertyName string) (vector.DistanceMetric, error) {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	tenantIndexes, tenantExists := vi.indexes[tenantID]
	if !tenantExists {
		return "", fmt.Errorf("no vector indexes exist for tenant %s", tenantID)
	}
	
	index, exists := tenantIndexes[propertyName]
	if !exists {
		return "", fmt.Errorf("no vector index exists for property %s in tenant %s", propertyName, tenantID)
	}
	return index.Metric(), nil
}
