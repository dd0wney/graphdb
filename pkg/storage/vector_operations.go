package storage

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// CreateVectorIndex creates a vector index for a node property
func (gs *GraphStorage) CreateVectorIndex(
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	return gs.vectorIndex.CreateIndex(propertyName, dimensions, m, efConstruction, metric)
}

// VectorSearch performs k-NN search on a vector-indexed property
func (gs *GraphStorage) VectorSearch(
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	return gs.vectorIndex.Search(propertyName, query, k, ef)
}

// DropVectorIndex removes a vector index
func (gs *GraphStorage) DropVectorIndex(propertyName string) error {
	return gs.vectorIndex.DropIndex(propertyName)
}

// HasVectorIndex checks if a vector index exists
func (gs *GraphStorage) HasVectorIndex(propertyName string) bool {
	return gs.vectorIndex.HasIndex(propertyName)
}

// ListVectorIndexes returns names of all vector-indexed properties
func (gs *GraphStorage) ListVectorIndexes() []string {
	return gs.vectorIndex.ListIndexes()
}

// GetVectorIndexMetric returns the distance metric for a specific vector index
func (gs *GraphStorage) GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error) {
	return gs.vectorIndex.GetIndexMetric(propertyName)
}

// UpdateNodeVectorIndexes updates vector indexes when a node is added/updated
// This should be called after a node with vector properties is created/updated
func (gs *GraphStorage) UpdateNodeVectorIndexes(node *Node) error {
	// Check each property for vector type
	for propName, propVal := range node.Properties {
		if propVal.Type == TypeVector {
			// If index exists for this property, add/update the vector
			if gs.vectorIndex.HasIndex(propName) {
				vec, err := propVal.AsVector()
				if err != nil {
					return fmt.Errorf("failed to decode vector for property %s: %w", propName, err)
				}

				// Try to remove old vector first (in case of update)
				gs.vectorIndex.RemoveVector(propName, node.ID)

				// Add new vector
				if err := gs.vectorIndex.AddVector(propName, node.ID, vec); err != nil {
					return fmt.Errorf("failed to add vector to index %s: %w", propName, err)
				}
			}
		}
	}
	return nil
}

// RemoveNodeFromVectorIndexes removes node vectors from all indexes
// This should be called when a node is deleted
func (gs *GraphStorage) RemoveNodeFromVectorIndexes(nodeID uint64) error {
	// Remove from all vector indexes
	for _, indexName := range gs.vectorIndex.ListIndexes() {
		// Ignore errors - node might not be in all indexes
		gs.vectorIndex.RemoveVector(indexName, nodeID)
	}
	return nil
}
