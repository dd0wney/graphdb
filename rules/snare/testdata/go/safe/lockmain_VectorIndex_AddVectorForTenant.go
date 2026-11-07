// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/vector_index.go at b9bc9d2, lines 128-148, function VectorIndex.AddVectorForTenant.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): lookupIndexLocked only reads maps; unlock is early on purpose.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

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
