// Package storage's interface.go declares the Storage / StorageReader /
// StorageWriter interfaces extracted from the concrete *GraphStorage receiver.
//
// History:
//   - S1 (R1.0, PR #145): initial landing at 51 of 58 originally-declared
//     methods. Tenant-isolated vector methods and the NodeObserver hook
//     were deliberately omitted pending F4 (vector tenant redesign) and
//     S11 (auto-embedder redesign) — those tracks needed to ship before
//     the interface could be closed honestly.
//   - F4 / R1.x (PRs #184, #185): per-tenant VectorIndex data structure
//     and the 6 *VectorIndexForTenant methods land on *GraphStorage.
//   - S11 / R2.1 (PR #186): NodeObserver interface and AddObserver
//     method land on *GraphStorage.
//   - R3 (this file post-R3): the 6 *VectorIndexForTenant methods join
//     StorageReader; AddObserver joins Storage's admin section. The
//     interface surface is now complete with respect to the original
//     S1 / F4 / S11 designs.
//
// Snapshot signature: kept as `Snapshot() error` (no ctx). The archive
// parent had `Snapshot(ctx)` for cancellability, but no production
// caller passes a meaningful context — adding ctx now would be
// speculative. A future streaming/cancelable snapshot would be a new
// method (e.g., SnapshotStream(ctx, w)) rather than a signature change.
//
// See docs/internals/design/AUDIT_gemini_track_claims_2026-05-13.md for
// the bulk-stash audit framing that scoped these decisions, and
// docs/NEXT_STEPS_2026-05-14.md § Track R / R3 for the closure plan.

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
	FindNodesByLabelAcrossTenants(label string) ([]*Node, error)
	GetNode(nodeID uint64) (*Node, error)
	GetOutgoingEdges(nodeID uint64) ([]*Edge, error)
	GetIncomingEdges(nodeID uint64) ([]*Edge, error)
	GetEdge(edgeID uint64) (*Edge, error)

	// Search and Indexing
	FindNodesByPropertyForTenant(key string, value Value, tenantID string) ([]*Node, error)
	FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error)

	// Vector index — tenant-blind variants (legacy / single-tenant / test).
	// New callers should prefer the *ForTenant variants below.
	VectorSearch(propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error)
	ListVectorIndexes() []string
	HasVectorIndex(propertyName string) bool
	CreateVectorIndex(propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error
	DropVectorIndex(propertyName string) error

	// Vector index — tenant-scoped variants (R1.x / F4 spike). Per F4
	// spike §1.3, these reject empty tenantID at the public layer and
	// return ErrNodeNotFound (unified error) for cross-tenant lookups
	// to prevent existence-leak via response shape.
	VectorSearchForTenant(tenantID string, propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error)
	ListVectorIndexesForTenant(tenantID string) []string
	HasVectorIndexForTenant(tenantID string, propertyName string) bool
	CreateVectorIndexForTenant(tenantID string, propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error
	DropVectorIndexForTenant(tenantID string, propertyName string) error
	GetVectorIndexMetricForTenant(tenantID string, propertyName string) (vector.DistanceMetric, error)

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

	// Vector index maintenance. RemoveNodeFromVectorIndexes takes a tenantID
	// as of R1.2 so deletes route to the per-tenant index; empty tenantID
	// falls back to tenantid.Default for legacy tenant-blind callers.
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
	Snapshot() error

	// Observer registration (S11 / R2.1). AddObserver is typically called
	// once at startup before serving requests. Backends without
	// notification support implement this as a no-op (e.g.,
	// BTreeGraphStorage); observers attached to a no-op backend simply
	// never fire.
	AddObserver(obs NodeObserver)

	Close() error
}

// Compile-time assertion that *GraphStorage satisfies Storage.
// If this line ever fails to build, a method has drifted between
// the interface and the receiver — fix one or the other.
var _ Storage = (*GraphStorage)(nil)
