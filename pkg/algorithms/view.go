package algorithms

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// graphView abstracts graph access so tenant-blind and tenant-scoped
// algorithm callers share the same algorithm bodies.
//
// Audit A6c-algorithms (2026-05-08): the duplication pattern used by
// A6b's ShortestPath/ShortestPathForTenant works for one algorithm
// but doesn't scale to the full /algorithms/* surface — eight
// algorithms × ~150 LOC each is a maintenance trap. Each algorithm
// instead operates against this small interface, with two adaptors
// providing tenant-blind and tenant-scoped behavior.
//
// Methods are added to graphView only when an algorithm needs them —
// keep the interface minimal so adaptors stay shallow.
// graphView abstracts graph access so tenant-blind and tenant-scoped
// algorithm callers share the same algorithm bodies.
//
// Audit A6c-algorithms (2026-05-08): updated 2026-05-12 to be backed
// by the new storage.StorageReader interface (S1).
type graphView interface {
	AllNodes() []*storage.Node
	Node(id uint64) (*storage.Node, error)
	OutgoingEdges(id uint64) ([]*storage.Edge, error)
	IncomingEdges(id uint64) ([]*storage.Edge, error)
	Edge(id uint64) (*storage.Edge, error)
}

// tenantBlindView delegates to storage.StorageReader using the
// cross-tenant / admin methods where relevant.
type tenantBlindView struct {
	reader storage.StorageReader
}

func newTenantBlindView(reader storage.StorageReader) *tenantBlindView {
	return &tenantBlindView{reader: reader}
}

func (v *tenantBlindView) AllNodes() []*storage.Node {
	// For tenant-blind view (CLI/Admin), we use an empty tenantID
	// which storage interprets as the default tenant, OR we might
	// need a way to get "all" if the interface supports it.
	// Currently GetAllNodesAcrossTenants is not in StorageReader
	// because it's a security-sensitive admin operation.
	// GraphStorage still has it.
	if gs, ok := v.reader.(storage.Storage); ok {
		return gs.GetAllNodesAcrossTenants()
	}
	// Fallback to default tenant if not GraphStorage (less than ideal but safe)
	return v.reader.GetAllNodesForTenant("")
}

func (v *tenantBlindView) Node(id uint64) (*storage.Node, error) {
	// For tenant-blind view, we try to get the node from the default tenant.
	// Actually, the legacy tenant-blind methods in GraphStorage (GetNode)
	// were truly tenant-blind.
	if gs, ok := v.reader.(storage.Storage); ok {
		return gs.GetNode(id)
	}
	return v.reader.GetNodeForTenant(id, "")
}

func (v *tenantBlindView) OutgoingEdges(id uint64) ([]*storage.Edge, error) {
	if gs, ok := v.reader.(storage.Storage); ok {
		return gs.GetOutgoingEdges(id)
	}
	return v.reader.GetOutgoingEdgesForTenant(id, "")
}

func (v *tenantBlindView) IncomingEdges(id uint64) ([]*storage.Edge, error) {
	if gs, ok := v.reader.(storage.Storage); ok {
		return gs.GetIncomingEdges(id)
	}
	return v.reader.GetIncomingEdgesForTenant(id, "")
}

func (v *tenantBlindView) Edge(id uint64) (*storage.Edge, error) {
	if gs, ok := v.reader.(storage.Storage); ok {
		return gs.GetEdge(id)
	}
	return v.reader.GetEdgeForTenant(id, "")
}

// tenantScopedView delegates to storage.StorageReader pinning
// every read to a specific tenant.
type tenantScopedView struct {
	reader   storage.StorageReader
	tenantID string
}

func newTenantScopedView(reader storage.StorageReader, tenantID string) *tenantScopedView {
	return &tenantScopedView{reader: reader, tenantID: tenantID}
}

func (v *tenantScopedView) AllNodes() []*storage.Node {
	return v.reader.GetAllNodesForTenant(v.tenantID)
}

func (v *tenantScopedView) Node(id uint64) (*storage.Node, error) {
	return v.reader.GetNodeForTenant(id, v.tenantID)
}

func (v *tenantScopedView) OutgoingEdges(id uint64) ([]*storage.Edge, error) {
	return v.reader.GetOutgoingEdgesForTenant(id, v.tenantID)
}

func (v *tenantScopedView) IncomingEdges(id uint64) ([]*storage.Edge, error) {
	return v.reader.GetIncomingEdgesForTenant(id, v.tenantID)
}

func (v *tenantScopedView) Edge(id uint64) (*storage.Edge, error) {
	return v.reader.GetEdgeForTenant(id, v.tenantID)
}
