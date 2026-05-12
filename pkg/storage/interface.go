package storage

import (
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// StorageReader defines the read-only surface of the graph database.
// All methods are tenant-scoped to ensure isolation.
type StorageReader interface {
	// Node retrieval
	GetNodeForTenant(nodeID uint64, tenantID string) (*Node, error)
	GetNodesByLabelForTenant(tenantID string, label string) []*Node
	GetAllNodesForTenant(tenantID string) []*Node
	CountNodesForTenant(tenantID string) uint64

	// Edge retrieval
	GetEdgeForTenant(edgeID uint64, tenantID string) (*Edge, error)
	GetEdgesByTypeForTenant(tenantID string, edgeType string) []*Edge
	GetAllEdgesForTenant(tenantID string) []*Edge
	CountEdgesForTenant(tenantID string) uint64

	// Adjacency
	GetOutgoingEdgesForTenant(nodeID uint64, tenantID string) ([]*Edge, error)
	GetIncomingEdgesForTenant(nodeID uint64, tenantID string) ([]*Edge, error)

	// Metadata
	GetLabelsForTenant(tenantID string) []string
	GetEdgeTypesForTenant(tenantID string) []string

	// Search and Indexing
	FindNodesByPropertyForTenant(key string, value Value, tenantID string) ([]*Node, error)
	FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error)
	VectorSearchForTenant(tenantID string, propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error)

	// Performance optimization: callback with live reference (no clone)
	WithNodeRefForTenant(nodeID uint64, tenantID string, fn func(*Node) error) error
}

// StorageWriter defines the mutative surface of the graph database.
type StorageWriter interface {
	// Node mutations
	CreateNodeWithTenant(tenantID string, labels []string, properties map[string]Value) (*Node, error)
	CreateNodeWithUniquePropertyForTenant(tenantID string, labels []string, properties map[string]Value, uniqueLabel string, uniquePropertyKey string) (*Node, error)
	UpdateNodeForTenant(nodeID uint64, properties map[string]Value, tenantID string) error
	DeleteNodeForTenant(nodeID uint64, tenantID string) error
	RemoveNodePropertiesForTenant(nodeID uint64, keys []string, tenantID string) error

	// Edge mutations
	CreateEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error)
	UpdateEdgeForTenant(edgeID uint64, properties map[string]Value, weight *float64, tenantID string) error
	DeleteEdgeForTenant(edgeID uint64, tenantID string) error
	UpsertEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error)
}

// Storage is the unified interface for GraphDB storage engines.
type Storage interface {
	StorageReader
	StorageWriter

	// Global/Admin operations
	GetAllLabels() []string
	Close() error
}
