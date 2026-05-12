// Package storage's interface.go declares the Storage / StorageReader /
// StorageWriter interfaces extracted from the concrete *GraphStorage receiver.
//
// Scope: this is S1's first landing — narrow to the surface today's
// GraphStorage already implements (51 methods). Tenant-isolated vector
// methods (CreateVectorIndexForTenant, etc.) and the NodeObserver
// observation hook are deliberately omitted; they expand the interface
// in later tracks (F4 once redesigned with tenant-strict semantics, S11
// once auto-embeddings stops being a 3-float mock). See
// docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md for the
// rationale.

package storage

import (
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// StorageReader defines the read-only surface of the graph database.
// All *ForTenant methods are tenant-scoped per the repo's tenant-strict
// convention (cross-tenant lookups return ErrNodeNotFound, not a distinct
// error, to avoid existence-leak side channels).
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
	GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error)
	FindNodesByLabel(label string) ([]*Node, error)
	GetNode(nodeID uint64) (*Node, error)
	GetOutgoingEdges(nodeID uint64) ([]*Edge, error)
	GetIncomingEdges(nodeID uint64) ([]*Edge, error)
	GetEdge(edgeID uint64) (*Edge, error)

	// Search and Indexing
	FindNodesByPropertyForTenant(key string, value Value, tenantID string) ([]*Node, error)
	FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error)

	// Vector index (tenant-blind; tenant-scoped variants will be added by F4)
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
	RemoveNodeFromVectorIndexes(nodeID uint64) error
}

// Storage is the unified interface for GraphDB storage engines.
type Storage interface {
	StorageReader
	StorageWriter

	// Global/Admin operations
	GetAllLabels() []string
	GetAllNodesAcrossTenants() []*Node
	SetEncryption(engine encryption.EncryptDecrypter, keyManager encryption.KeyProvider)
	Snapshot() error

	Close() error
}

// Compile-time assertion that *GraphStorage satisfies Storage.
// If this line ever fails to build, a method has drifted between
// the interface and the receiver — fix one or the other.
var _ Storage = (*GraphStorage)(nil)
