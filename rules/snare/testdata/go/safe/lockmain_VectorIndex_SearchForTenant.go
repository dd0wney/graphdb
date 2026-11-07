// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/vector_index.go at b9bc9d2, lines 161-182, function VectorIndex.SearchForTenant.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): same as AddVectorForTenant.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
