package storage

import (
	"context"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
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
	HasPropertyIndex(key string) bool
	GetVectorIndexMetricForTenant(tenantID string, propertyName string) (vector.DistanceMetric, error)
	GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error)
	FindNodesByLabel(label string) ([]*Node, error)
	GetNode(nodeID uint64) (*Node, error)
	GetOutgoingEdges(nodeID uint64) ([]*Edge, error)
	GetIncomingEdges(nodeID uint64) ([]*Edge, error)
	GetEdge(edgeID uint64) (*Edge, error)

	// Search and Indexing
	FindNodesByPropertyForTenant(key string, value Value, tenantID string) ([]*Node, error)
	FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error)
	VectorSearchForTenant(tenantID string, propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error)
	ListVectorIndexesForTenant(tenantID string) []string
	HasVectorIndexForTenant(tenantID string, propertyName string) bool
	CreateVectorIndexForTenant(tenantID string, propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error
	DropVectorIndexForTenant(tenantID string, propertyName string) error

	// Tenant-blind variants (Admin/Test)
	VectorSearch(propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error)
	ListVectorIndexes() []string
	HasVectorIndex(propertyName string) bool
	CreateVectorIndex(propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error
	DropVectorIndex(propertyName string) error

	// Performance optimization: callback with live reference (no clone)
	WithNodeRefForTenant(nodeID uint64, tenantID string, fn func(*Node) error) error

	// Statistics
	GetStatistics() Statistics
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

	// Tenant-blind mutations (Admin/Test)
	CreateNode(labels []string, properties map[string]Value) (*Node, error)
	CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error)
	UpdateNode(nodeID uint64, properties map[string]Value) error
	DeleteNode(nodeID uint64) error
	RemoveNodeProperties(nodeID uint64, keys []string) error
	UpdateEdge(edgeID uint64, properties map[string]Value, weight *float64) error
	DeleteEdge(edgeID uint64) error

	// Batching
	BeginBatch() *Batch

	// Vector index maintenance
	UpdateNodeVectorIndexes(node *Node) error
	RemoveNodeFromVectorIndexes(nodeID uint64, tenantID string) error
}

// Storage is the unified interface for GraphDB storage engines.
type Storage interface {
	StorageReader
	StorageWriter

	// Global/Admin operations
	GetAllLabels() []string
	GetAllNodesAcrossTenants() []*Node
	SetEncryption(engine encryption.EncryptDecrypter, keyManager encryption.KeyProvider)
	Snapshot(ctx context.Context) error
	
	// Observation (S11 Intelligence)
	AddObserver(observer NodeObserver)
	
	Close() error
}

// NodeObserver defines the interface for reacting to node mutations.
type NodeObserver interface {
	OnNodeCreated(node *Node)
	OnNodeUpdated(node *Node, oldNode *Node)
	OnNodeDeleted(nodeID uint64, tenantID string)
}

var _ Storage = (*GraphStorage)(nil)
