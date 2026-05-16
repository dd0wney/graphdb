package storage

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/tenantid"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// CreateVectorIndex creates a vector index for a node property under the
// default tenant. Tenant-blind; new callers should prefer
// CreateVectorIndexForTenant.
func (gs *GraphStorage) CreateVectorIndex(
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	return gs.vectorIndex.CreateIndex(propertyName, dimensions, m, efConstruction, metric)
}

// VectorSearch performs k-NN search on a vector-indexed property in the
// default tenant. Tenant-blind; new callers should prefer
// VectorSearchForTenant.
func (gs *GraphStorage) VectorSearch(
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	return gs.vectorIndex.Search(propertyName, query, k, ef)
}

// DropVectorIndex removes a vector index from the default tenant.
// Tenant-blind; new callers should prefer DropVectorIndexForTenant.
func (gs *GraphStorage) DropVectorIndex(propertyName string) error {
	return gs.vectorIndex.DropIndex(propertyName)
}

// HasVectorIndex checks if a vector index exists in the default tenant.
// Tenant-blind; new callers should prefer HasVectorIndexForTenant.
func (gs *GraphStorage) HasVectorIndex(propertyName string) bool {
	return gs.vectorIndex.HasIndex(propertyName)
}

// ListVectorIndexes returns the default tenant's vector-indexed property
// names. Tenant-blind; new callers should prefer ListVectorIndexesForTenant.
func (gs *GraphStorage) ListVectorIndexes() []string {
	return gs.vectorIndex.ListIndexes()
}

// GetVectorIndexMetric returns the distance metric for the default tenant's
// vector index on propertyName. Tenant-blind; new callers should prefer
// GetVectorIndexMetricForTenant.
func (gs *GraphStorage) GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error) {
	return gs.vectorIndex.GetIndexMetric(propertyName)
}

// --- Tenant-scoped vector operations (R1.2 / F4 spike §6) ---
//
// The 6 *VectorIndexForTenant methods below are the strict tenant-scoped
// surface. Per the F4 spike § 1.3 (existence-leak threat model):
//
// - Empty tenantID is rejected uniformly. The caller has an explicit
//   tenantID variable; passing "" is a programming error, not a tenant-blind
//   convenience. Empty tenantID does NOT silently route to "default" (which
//   would let an attacker probe the default tenant via the empty key).
//
// - Lookup methods (Search, GetMetric) return ErrNodeNotFound — the same
//   unified error used for cross-tenant node lookups (GetNodeForTenant).
//   This prevents existence-leak via error-shape inference: callers cannot
//   distinguish "tenant unknown" from "index missing on a known tenant"
//   from "tenant has no indexes at all."
//
// - Probing-style methods (Has, List) return false / []string{} for empty/
//   unknown tenants — same uniform-no-information rationale.
//
// - Admin methods (Create, Drop) return descriptive errors for empty
//   tenantID — they are explicit administrative actions; surfacing the bug
//   at the caller is the right outcome.

// CreateVectorIndexForTenant creates a vector index for a property under the
// given tenant. Returns an error for empty tenantID.
func (gs *GraphStorage) CreateVectorIndexForTenant(
	tenantID string,
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	if tenantID == "" {
		return fmt.Errorf("create vector index: tenantID must not be empty")
	}
	return gs.vectorIndex.CreateIndexForTenant(
		tenantid.TenantID(tenantID), propertyName, dimensions, m, efConstruction, metric,
	)
}

// VectorSearchForTenant performs k-NN search on a vector-indexed property,
// scoped to tenantID. Returns ErrNodeNotFound for empty tenantID or when
// the tenant has no index for propertyName — the unified error prevents
// existence-leak via error shape.
func (gs *GraphStorage) VectorSearchForTenant(
	tenantID string,
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	if tenantID == "" {
		return nil, ErrNodeNotFound
	}
	// Pre-check existence to give ErrNodeNotFound for the cross-tenant
	// case rather than the internal layer's descriptive error. TOCTOU
	// race between Has and Search is benign: a concurrent Drop just
	// surfaces as Search's "no vector index exists" error, which is no
	// worse than the current tenant-blind path.
	if !gs.vectorIndex.HasIndexForTenant(tenantid.TenantID(tenantID), propertyName) {
		return nil, ErrNodeNotFound
	}
	return gs.vectorIndex.SearchForTenant(tenantid.TenantID(tenantID), propertyName, query, k, ef)
}

// HasVectorIndexForTenant reports whether the given tenant has a vector
// index for propertyName. Returns false (not an error) for empty tenantID
// or unknown tenants — the unified-false response prevents tenant-existence
// probing.
func (gs *GraphStorage) HasVectorIndexForTenant(tenantID string, propertyName string) bool {
	if tenantID == "" {
		return false
	}
	return gs.vectorIndex.HasIndexForTenant(tenantid.TenantID(tenantID), propertyName)
}

// DropVectorIndexForTenant removes the given tenant's vector index for
// propertyName. Returns an error for empty tenantID.
func (gs *GraphStorage) DropVectorIndexForTenant(tenantID string, propertyName string) error {
	if tenantID == "" {
		return fmt.Errorf("drop vector index: tenantID must not be empty")
	}
	return gs.vectorIndex.DropIndexForTenant(tenantid.TenantID(tenantID), propertyName)
}

// ListVectorIndexesForTenant returns the given tenant's vector-indexed
// property names. Returns []string{} (not nil) for empty tenantID or
// tenants with no indexes.
func (gs *GraphStorage) ListVectorIndexesForTenant(tenantID string) []string {
	if tenantID == "" {
		return []string{}
	}
	return gs.vectorIndex.ListIndexesForTenant(tenantid.TenantID(tenantID))
}

// GetVectorIndexMetricForTenant returns the distance metric for the given
// tenant's vector index on propertyName. Returns ErrNodeNotFound for empty
// tenantID or when the tenant has no index for propertyName.
func (gs *GraphStorage) GetVectorIndexMetricForTenant(
	tenantID string,
	propertyName string,
) (vector.DistanceMetric, error) {
	if tenantID == "" {
		return "", ErrNodeNotFound
	}
	if !gs.vectorIndex.HasIndexForTenant(tenantid.TenantID(tenantID), propertyName) {
		return "", ErrNodeNotFound
	}
	return gs.vectorIndex.GetIndexMetricForTenant(tenantid.TenantID(tenantID), propertyName)
}

// UpdateNodeVectorIndexes updates vector indexes when a node is added or
// updated. Routes by node.TenantID; empty TenantID falls back to
// tenantid.Default so tenant-blind callers (legacy CreateNode path, tests
// that don't set TenantID) continue to land vectors in the default-tenant
// namespace transparently.
//
// Behavior change relative to pre-R1.2 (this PR): a node with an explicit
// non-default TenantID now lands its vectors under its tenant's index, not
// the global namespace. Previously all vectors landed globally; the
// existence-leak prevention happened post-search in handlers_vectors.go via
// WithNodeRefForTenant filtering. After this PR, isolation is structural
// (per-tenant indexes via R1.1), and the handler post-filter becomes
// defense-in-depth + lock discipline for label/property filtering.
func (gs *GraphStorage) UpdateNodeVectorIndexes(node *Node) error {
	tenantID := tenantid.TenantID(node.TenantID)
	if tenantID.IsEmpty() {
		tenantID = tenantid.Default
	}
	for propName, propVal := range node.Properties {
		// Accept both TypeVector (native float32 embeddings) and TypeFloatArray
		// (float64 arrays produced by ValueFromJSON when the client sends a JSON
		// float array). Both carry vector data; TypeFloatArray values are
		// truncated to float32 precision for HNSW storage.
		var vec []float32
		switch propVal.Type {
		case TypeVector:
			v, err := propVal.AsVector()
			if err != nil {
				return fmt.Errorf("failed to decode vector for property %s: %w", propName, err)
			}
			vec = v
		case TypeFloatArray:
			f64s, err := propVal.AsFloatArray()
			if err != nil {
				return fmt.Errorf("failed to decode float array for property %s: %w", propName, err)
			}
			vec = make([]float32, len(f64s))
			for i, v := range f64s {
				vec[i] = float32(v)
			}
		default:
			continue
		}

		if gs.vectorIndex.HasIndexForTenant(tenantID, propName) {
			// Try to remove old vector first (in case of update).
			// Ignore errors — if the index is empty for this node,
			// RemoveVector is a no-op and returns a missing-entry error.
			_ = gs.vectorIndex.RemoveVectorForTenant(tenantID, propName, node.ID)

			if err := gs.vectorIndex.AddVectorForTenant(tenantID, propName, node.ID, vec); err != nil {
				return fmt.Errorf("failed to add vector to index %s: %w", propName, err)
			}
		}
	}
	return nil
}

// RemoveNodeFromVectorIndexes removes a node's vectors from all of the
// given tenant's vector indexes. Called from DeleteNode (passing the
// looked-up node's TenantID) and from cleanup paths.
//
// Empty tenantID falls back to tenantid.Default so tenant-blind callers
// (legacy DeleteNode path, tests that don't set TenantID) continue to clean
// up the default-tenant namespace transparently.
//
// Signature change relative to pre-R1.2 (this PR): now takes tenantID as
// a second parameter. Callers that previously passed only nodeID must
// supply the node's tenant (or "" for tenant-blind / default semantics).
func (gs *GraphStorage) RemoveNodeFromVectorIndexes(nodeID uint64, tenantID string) error {
	tid := tenantid.TenantID(tenantID)
	if tid.IsEmpty() {
		tid = tenantid.Default
	}
	for _, indexName := range gs.vectorIndex.ListIndexesForTenant(tid) {
		// Ignore errors - node might not be in all indexes
		_ = gs.vectorIndex.RemoveVectorForTenant(tid, indexName, nodeID)
	}
	return nil
}
