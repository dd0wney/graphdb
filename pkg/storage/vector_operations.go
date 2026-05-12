package storage

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// CreateVectorIndexForTenant creates a vector index for a node property within a specific tenant
func (gs *GraphStorage) CreateVectorIndexForTenant(
	tenantID string,
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	if tenantID == "" {
		tenantID = "default"
	}
	return gs.vectorIndex.CreateIndex(tenantID, propertyName, dimensions, m, efConstruction, metric)
}

// CreateVectorIndex creates a vector index for the default tenant
func (gs *GraphStorage) CreateVectorIndex(
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	return gs.CreateVectorIndexForTenant("default", propertyName, dimensions, m, efConstruction, metric)
}

// VectorSearch performs k-NN search on a vector-indexed property for the default tenant
func (gs *GraphStorage) VectorSearch(
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	return gs.vectorIndex.Search("default", propertyName, query, k, ef)
}

// VectorSearchForTenant performs k-NN search on a vector-indexed property,
// scoped to a specific tenant.
func (gs *GraphStorage) VectorSearchForTenant(
	tenantID string,
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	return gs.vectorIndex.Search(tenantID, propertyName, query, k, ef)
}

// DropVectorIndexForTenant removes a vector index for a specific tenant
func (gs *GraphStorage) DropVectorIndexForTenant(tenantID string, propertyName string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	return gs.vectorIndex.DropIndex(tenantID, propertyName)
}

// DropVectorIndex removes a vector index for the default tenant
func (gs *GraphStorage) DropVectorIndex(propertyName string) error {
	return gs.DropVectorIndexForTenant("default", propertyName)
}

// HasVectorIndexForTenant checks if a vector index exists for a specific tenant
func (gs *GraphStorage) HasVectorIndexForTenant(tenantID string, propertyName string) bool {
	if tenantID == "" {
		tenantID = "default"
	}
	return gs.vectorIndex.HasIndex(tenantID, propertyName)
}

// HasVectorIndex checks if a vector index exists for the default tenant
func (gs *GraphStorage) HasVectorIndex(propertyName string) bool {
	return gs.HasVectorIndexForTenant("default", propertyName)
}

// ListVectorIndexesForTenant returns names of all vector-indexed properties for a tenant
func (gs *GraphStorage) ListVectorIndexesForTenant(tenantID string) []string {
	if tenantID == "" {
		tenantID = "default"
	}
	return gs.vectorIndex.ListIndexes(tenantID)
}

// ListVectorIndexes returns names of all vector-indexed properties for the default tenant
func (gs *GraphStorage) ListVectorIndexes() []string {
	return gs.ListVectorIndexesForTenant("default")
}

// GetVectorIndexMetricForTenant returns the distance metric for a specific vector index for a tenant
func (gs *GraphStorage) GetVectorIndexMetricForTenant(tenantID string, propertyName string) (vector.DistanceMetric, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	return gs.vectorIndex.GetIndexMetric(tenantID, propertyName)
}

// GetVectorIndexMetric returns the distance metric for a specific vector index (default tenant)
func (gs *GraphStorage) GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error) {
	return gs.GetVectorIndexMetricForTenant("default", propertyName)
}


// UpdateNodeVectorIndexes updates vector indexes when a node is added/updated
func (gs *GraphStorage) UpdateNodeVectorIndexes(node *Node) error {
	tenantID := node.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	// Check each property for vector type
	for propName, propVal := range node.Properties {
		if propVal.Type == TypeVector {
			// If index exists for this property and tenant, add/update the vector
			if gs.vectorIndex.HasIndex(tenantID, propName) {
				vec, err := propVal.AsVector()
				if err != nil {
					return fmt.Errorf("failed to decode vector for property %s: %w", propName, err)
				}

				// Try to remove old vector first (in case of update)
				_ = gs.vectorIndex.RemoveVector(tenantID, propName, node.ID)

				// Add new vector
				if err := gs.vectorIndex.AddVector(tenantID, propName, node.ID, vec); err != nil {
					return fmt.Errorf("failed to add vector to index %s: %w", propName, err)
				}
			}
		}
	}
	return nil
}

// RemoveNodeFromVectorIndexes removes node vectors from all indexes for a tenant
func (gs *GraphStorage) RemoveNodeFromVectorIndexes(nodeID uint64, tenantID string) error {
	if tenantID == "" {
		tenantID = "default"
	}

	// Remove from all vector indexes for this tenant
	for _, indexName := range gs.vectorIndex.ListIndexes(tenantID) {
		_ = gs.vectorIndex.RemoveVector(tenantID, indexName, nodeID)
	}
	return nil
}
