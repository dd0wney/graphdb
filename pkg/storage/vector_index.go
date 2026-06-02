package storage

import (
	"fmt"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/tenantid"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// VectorIndex manages vector search indexes for node properties, partitioned
// by tenant.
//
// The outer map key is tenantid.TenantID; the inner key is propertyName.
// Per-tenant outer partitioning means a tenant's index cannot be observed
// (searched, listed, has-checked) by a request scoped to a different tenant —
// the existence-leak channel that motivated the F4 redesign. See
// docs/internals/design/F4_VECTOR_TENANT_REDESIGN.md.
//
// The tenant-blind public methods (CreateIndex, Search, etc.) are preserved
// for backwards compatibility; each delegates to its *ForTenant counterpart
// with tenantid.Default. The strict *ForTenant methods reject the empty
// tenantID — they are the path R1.2's GraphStorage.*VectorIndexForTenant
// wrappers route through.
type VectorIndex struct {
	mu      sync.RWMutex
	indexes map[tenantid.TenantID]map[string]*vector.HNSWIndex
}

// NewVectorIndex creates a new vector index manager.
func NewVectorIndex() *VectorIndex {
	return &VectorIndex{
		indexes: make(map[tenantid.TenantID]map[string]*vector.HNSWIndex),
	}
}

// errEmptyTenantID is returned by *ForTenant methods when the caller passes
// an empty tenantID. Tenant-blind callers should use the non-ForTenant
// methods, which default to tenantid.Default rather than surfacing this
// error.
var errEmptyTenantID = fmt.Errorf("vector index: tenantID must not be empty (use the tenant-blind method to target tenantid.Default)")

// CreateIndex creates a new HNSW index for a property under the default
// tenant. Equivalent to CreateIndexForTenant(tenantid.Default, ...).
func (vi *VectorIndex) CreateIndex(
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	return vi.CreateIndexForTenant(tenantid.Default, propertyName, dimensions, m, efConstruction, metric)
}

// CreateIndexForTenant creates a new HNSW index for a property under the
// given tenant. Returns an error if tenantID is empty or an index already
// exists for (tenantID, propertyName).
func (vi *VectorIndex) CreateIndexForTenant(
	tenantID tenantid.TenantID,
	propertyName string,
	dimensions int,
	m int,
	efConstruction int,
	metric vector.DistanceMetric,
) error {
	if tenantID.IsEmpty() {
		return errEmptyTenantID
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	inner, ok := vi.indexes[tenantID]
	if ok {
		if _, exists := inner[propertyName]; exists {
			return fmt.Errorf("vector index already exists for property %s", propertyName)
		}
	}

	index, err := vector.NewHNSWIndex(dimensions, m, efConstruction, metric)
	if err != nil {
		return fmt.Errorf("failed to create HNSW index: %w", err)
	}

	if inner == nil {
		inner = make(map[string]*vector.HNSWIndex)
		vi.indexes[tenantID] = inner
	}
	inner[propertyName] = index
	return nil
}

// AddVector adds a vector to the default-tenant index. Equivalent to
// AddVectorForTenant(tenantid.Default, ...).
func (vi *VectorIndex) AddVector(propertyName string, nodeID uint64, vec []float32) error {
	return vi.AddVectorForTenant(tenantid.Default, propertyName, nodeID, vec)
}

// AddVectorForTenant adds a vector to (tenantID, propertyName)'s index.
func (vi *VectorIndex) AddVectorForTenant(
	tenantID tenantid.TenantID,
	propertyName string,
	nodeID uint64,
	vec []float32,
) error {
	if tenantID.IsEmpty() {
		return errEmptyTenantID
	}

	vi.mu.RLock()
	index, exists := vi.lookupIndexLocked(tenantID, propertyName)
	vi.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	return index.Insert(nodeID, vec)
}

// Search performs k-NN search on the default-tenant index. Equivalent to
// SearchForTenant(tenantid.Default, ...).
func (vi *VectorIndex) Search(
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	return vi.SearchForTenant(tenantid.Default, propertyName, query, k, ef)
}

// SearchForTenant performs k-NN search on (tenantID, propertyName)'s index.
func (vi *VectorIndex) SearchForTenant(
	tenantID tenantid.TenantID,
	propertyName string,
	query []float32,
	k int,
	ef int,
) ([]vector.SearchResult, error) {
	if tenantID.IsEmpty() {
		return nil, errEmptyTenantID
	}

	vi.mu.RLock()
	index, exists := vi.lookupIndexLocked(tenantID, propertyName)
	vi.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	return index.Search(query, k, ef)
}

// RemoveVector removes a vector from the default-tenant index. Equivalent
// to RemoveVectorForTenant(tenantid.Default, ...).
func (vi *VectorIndex) RemoveVector(propertyName string, nodeID uint64) error {
	return vi.RemoveVectorForTenant(tenantid.Default, propertyName, nodeID)
}

// RemoveVectorForTenant removes nodeID's vector from (tenantID, propertyName)'s index.
func (vi *VectorIndex) RemoveVectorForTenant(
	tenantID tenantid.TenantID,
	propertyName string,
	nodeID uint64,
) error {
	if tenantID.IsEmpty() {
		return errEmptyTenantID
	}

	vi.mu.RLock()
	index, exists := vi.lookupIndexLocked(tenantID, propertyName)
	vi.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	return index.Delete(nodeID)
}

// DropIndex removes the default-tenant's index for propertyName. Equivalent
// to DropIndexForTenant(tenantid.Default, ...).
func (vi *VectorIndex) DropIndex(propertyName string) error {
	return vi.DropIndexForTenant(tenantid.Default, propertyName)
}

// DropIndexForTenant removes (tenantID, propertyName)'s index.
func (vi *VectorIndex) DropIndexForTenant(tenantID tenantid.TenantID, propertyName string) error {
	if tenantID.IsEmpty() {
		return errEmptyTenantID
	}

	vi.mu.Lock()
	defer vi.mu.Unlock()

	inner, ok := vi.indexes[tenantID]
	if !ok {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}
	if _, exists := inner[propertyName]; !exists {
		return fmt.Errorf("no vector index exists for property %s", propertyName)
	}

	delete(inner, propertyName)
	if len(inner) == 0 {
		delete(vi.indexes, tenantID)
	}
	return nil
}

// HasIndex reports whether the default-tenant has an index for propertyName.
// Equivalent to HasIndexForTenant(tenantid.Default, ...).
func (vi *VectorIndex) HasIndex(propertyName string) bool {
	return vi.HasIndexForTenant(tenantid.Default, propertyName)
}

// HasIndexForTenant reports whether (tenantID, propertyName) has an index.
// Returns false (not an error) for both "no such tenant" and "no index on
// this property" — the unified false response prevents tenant-existence
// probing.
func (vi *VectorIndex) HasIndexForTenant(tenantID tenantid.TenantID, propertyName string) bool {
	if tenantID.IsEmpty() {
		return false
	}
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	_, exists := vi.lookupIndexLocked(tenantID, propertyName)
	return exists
}

// ListIndexes returns the default-tenant's indexed property names. Equivalent
// to ListIndexesForTenant(tenantid.Default).
func (vi *VectorIndex) ListIndexes() []string {
	return vi.ListIndexesForTenant(tenantid.Default)
}

// ListIndexesForTenant returns tenantID's indexed property names. Returns
// an empty slice (not nil) for tenants with no indexes.
func (vi *VectorIndex) ListIndexesForTenant(tenantID tenantid.TenantID) []string {
	if tenantID.IsEmpty() {
		return []string{}
	}
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	inner, ok := vi.indexes[tenantID]
	if !ok {
		return []string{}
	}
	names := make([]string, 0, len(inner))
	for name := range inner {
		names = append(names, name)
	}
	return names
}

// GetIndexMetric returns the distance metric for the default-tenant's index
// on propertyName. Equivalent to GetIndexMetricForTenant(tenantid.Default, ...).
func (vi *VectorIndex) GetIndexMetric(propertyName string) (vector.DistanceMetric, error) {
	return vi.GetIndexMetricForTenant(tenantid.Default, propertyName)
}

// GetIndexMetricForTenant returns the distance metric for (tenantID, propertyName)'s
// index.
func (vi *VectorIndex) GetIndexMetricForTenant(
	tenantID tenantid.TenantID,
	propertyName string,
) (vector.DistanceMetric, error) {
	if tenantID.IsEmpty() {
		return "", errEmptyTenantID
	}
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	index, exists := vi.lookupIndexLocked(tenantID, propertyName)
	if !exists {
		return "", fmt.Errorf("no vector index exists for property %s", propertyName)
	}
	return index.Metric(), nil
}

// DimensionsForTenant returns the configured vector dimension of
// (tenantID, propertyName)'s index and a presence flag. Used to validate an
// incoming vector's length cheaply (no graph traversal) before the expensive
// off-lock HNSW insert (Track P item 3 / H2): the storage write path decodes
// + dimension-checks under gs.mu, then runs Insert after releasing it.
func (vi *VectorIndex) DimensionsForTenant(tenantID tenantid.TenantID, propertyName string) (int, bool) {
	if tenantID.IsEmpty() {
		return 0, false
	}
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	index, exists := vi.lookupIndexLocked(tenantID, propertyName)
	if !exists {
		return 0, false
	}
	return index.Dimensions(), true
}

// lookupIndexLocked returns (tenantID, propertyName)'s HNSWIndex and a
// presence flag. Caller must hold vi.mu (read or write).
func (vi *VectorIndex) lookupIndexLocked(tenantID tenantid.TenantID, propertyName string) (*vector.HNSWIndex, bool) {
	inner, ok := vi.indexes[tenantID]
	if !ok {
		return nil, false
	}
	index, ok := inner[propertyName]
	return index, ok
}
