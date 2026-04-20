package search

import (
	"fmt"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TenantIndexes holds a FullTextIndex per tenant. Each tenant's index
// sees only its own nodes — isolation is enforced at build time via
// storage.GetNodesByLabelForTenant, not by filtering a shared index.
//
// Indexes are constructed lazily on first Get. A tenant that has never
// been indexed returns an empty index, which produces zero search
// results — the safe default.
//
// Design note: the API server owns the TenantIndexes; the query DSL's
// executor keeps its own (currently unused) shared index, so DSL
// search() is not yet tenant-scoped. Tenant-aware DSL search is a
// follow-up that requires threading tenant context through the
// executor. For now, callers of DSL search() are internal/trusted.
type TenantIndexes struct {
	gs *storage.GraphStorage

	mu      sync.RWMutex
	indexes map[string]*FullTextIndex
}

// NewTenantIndexes returns an empty TenantIndexes backed by gs.
func NewTenantIndexes(gs *storage.GraphStorage) *TenantIndexes {
	return &TenantIndexes{
		gs:      gs,
		indexes: make(map[string]*FullTextIndex),
	}
}

// Get returns the FullTextIndex for tenantID, constructing one lazily
// on first access. Safe for concurrent use. The returned index is
// populated only after IndexForTenant has been called for this tenant.
func (ti *TenantIndexes) Get(tenantID string) *FullTextIndex {
	ti.mu.RLock()
	if idx, ok := ti.indexes[tenantID]; ok {
		ti.mu.RUnlock()
		return idx
	}
	ti.mu.RUnlock()

	ti.mu.Lock()
	defer ti.mu.Unlock()
	if idx, ok := ti.indexes[tenantID]; ok {
		return idx
	}
	idx := NewFullTextIndex(ti.gs)
	ti.indexes[tenantID] = idx
	return idx
}

// IndexForTenant builds (or rebuilds) the index for tenantID from nodes
// that match the given labels AND belong to that tenant. Cross-tenant
// nodes are never passed to the index because the caller uses the
// tenant-scoped storage accessor.
func (ti *TenantIndexes) IndexForTenant(tenantID string, labels, properties []string) error {
	idx := ti.Get(tenantID)

	var nodes []*storage.Node
	for _, label := range labels {
		nodes = append(nodes, ti.gs.GetNodesByLabelForTenant(tenantID, label)...)
	}
	if err := idx.IndexPrepared(nodes, labels, properties); err != nil {
		return fmt.Errorf("index tenant %q: %w", tenantID, err)
	}
	return nil
}

// Tenants returns the IDs of tenants that currently have an index
// (whether populated or just touched via Get). Order is unspecified.
func (ti *TenantIndexes) Tenants() []string {
	ti.mu.RLock()
	defer ti.mu.RUnlock()
	out := make([]string, 0, len(ti.indexes))
	for t := range ti.indexes {
		out = append(out, t)
	}
	return out
}
