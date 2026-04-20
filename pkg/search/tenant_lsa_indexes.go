package search

import "sync"

// TenantLSAIndexes holds a per-tenant LSAIndex. Unlike TenantIndexes
// (which lazily constructs an empty FullTextIndex on first Get), LSA
// indexes require an expensive SVD build up front — so callers are
// expected to build explicitly via BuildLSAIndex and register the
// result via Set. Get returns nil for tenants that haven't been
// registered, signaling "no semantic search available for this
// tenant yet" to callers; the /hybrid-search handler uses this to
// degrade gracefully to a pure-FTS response.
//
// Not coupled to storage — LSA builds take a []Document that the
// caller is responsible for gathering (from any source, scoped by
// whatever means). This keeps the tenant scoping concern at the
// build-time layer, not inside this map.
type TenantLSAIndexes struct {
	mu      sync.RWMutex
	indexes map[string]*LSAIndex
}

// NewTenantLSAIndexes returns an empty per-tenant LSA registry.
func NewTenantLSAIndexes() *TenantLSAIndexes {
	return &TenantLSAIndexes{
		indexes: make(map[string]*LSAIndex),
	}
}

// Get returns the LSA index registered for tenantID, or nil if none
// has been registered. Callers MUST nil-check; the zero value is a
// deliberate signal ("LSA not available for this tenant").
func (tli *TenantLSAIndexes) Get(tenantID string) *LSAIndex {
	tli.mu.RLock()
	defer tli.mu.RUnlock()
	return tli.indexes[tenantID]
}

// Set registers idx as the LSA index for tenantID. A subsequent Set
// for the same tenantID replaces the prior index (supports rebuild).
// Set(tenantID, nil) removes the entry so callers can explicitly
// revoke LSA for a tenant (e.g. during corpus wipe).
func (tli *TenantLSAIndexes) Set(tenantID string, idx *LSAIndex) {
	tli.mu.Lock()
	defer tli.mu.Unlock()
	if idx == nil {
		delete(tli.indexes, tenantID)
		return
	}
	tli.indexes[tenantID] = idx
}

// Tenants returns the IDs of tenants with a registered LSA index.
// Order is unspecified.
func (tli *TenantLSAIndexes) Tenants() []string {
	tli.mu.RLock()
	defer tli.mu.RUnlock()
	out := make([]string, 0, len(tli.indexes))
	for t := range tli.indexes {
		out = append(out, t)
	}
	return out
}
