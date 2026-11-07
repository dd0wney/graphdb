// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/vector_index.go at b9bc9d2, lines 190-209, function VectorIndex.RemoveVectorForTenant.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): same as AddVectorForTenant.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
