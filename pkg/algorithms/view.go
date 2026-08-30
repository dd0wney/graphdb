package algorithms

import (
	"github.com/dd0wney/graphdb/pkg/storage"
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
type graphView interface {
	// AllNodes returns every node visible to this view.
	//
	// The error is ADR 0003's completeness signal, threaded up from
	// storage.GetAllNodesForTenant. The slice holds every node that could be
	// read either way, so an adaptor never returns nil beside a non-nil error.
	// Every algorithm body treats a non-nil error as fatal and returns it:
	// an algorithm that ran on a silently short node set would report a
	// confident wrong answer, which is worse than no answer. That is a
	// stronger reaction than the per-edge degrade branches in scc.go,
	// triangles.go and node_similarity.go, and deliberately so — a missing
	// node removes a vertex from the graph, not one of its edges.
	AllNodes() ([]*storage.Node, error)

	// Node returns a single node by ID. Returns ErrNodeNotFound for
	// missing or out-of-view nodes (tenant-scoped views collapse the
	// two cases — see GetNodeForTenant).
	Node(id uint64) (*storage.Node, error)

	// OutgoingEdges returns the subset of outgoing edges from id
	// that this view exposes.
	OutgoingEdges(id uint64) ([]*storage.Edge, error)

	// IncomingEdges mirrors OutgoingEdges for incoming.
	IncomingEdges(id uint64) ([]*storage.Edge, error)

	// Edge returns a single edge by ID. Used by algorithms that
	// dereference edge IDs after a traversal — e.g., edge-betweenness
	// looking up endpoints by edge ID. Returns ErrEdgeNotFound for
	// missing or out-of-view edges.
	Edge(id uint64) (*storage.Edge, error)
}

// tenantBlindView delegates straight through to the storage's
// tenant-blind methods. Used by the legacy public algorithm
// functions (PageRank, DetectCycles, etc.) which intentionally
// operate across all tenants — CLI, demos, single-tenant
// deployments.
type tenantBlindView struct {
	g storage.Storage
}

func newTenantBlindView(g storage.Storage) *tenantBlindView {
	return &tenantBlindView{g: g}
}

func (v *tenantBlindView) AllNodes() ([]*storage.Node, error) {
	// GetAllNodesAcrossTenants is the deliberately-named tenant-blind
	// enumerator (see pkg/storage/node_operations.go). Algorithms
	// running through tenantBlindView are CLI / single-tenant /
	// admin paths — not API-reachable, so the cross-tenant
	// "everything" view is the correct semantic.
	//
	// The error is always nil here. GetAllNodesAcrossTenants was not one of
	// ADR 0003's seven methods and keeps its single return, so this adaptor
	// has nothing to report. Both adaptors satisfy the same interface, so the
	// algorithm bodies stay identical; the tenant-scoped one below is the half
	// that can actually fail.
	return v.g.GetAllNodesAcrossTenants(), nil
}

func (v *tenantBlindView) Node(id uint64) (*storage.Node, error) {
	return v.g.GetNode(id)
}

func (v *tenantBlindView) OutgoingEdges(id uint64) ([]*storage.Edge, error) {
	return v.g.GetOutgoingEdges(id)
}

func (v *tenantBlindView) IncomingEdges(id uint64) ([]*storage.Edge, error) {
	return v.g.GetIncomingEdges(id)
}

func (v *tenantBlindView) Edge(id uint64) (*storage.Edge, error) {
	return v.g.GetEdge(id)
}

// tenantScopedView delegates to the *ForTenant storage methods,
// pinning every read to a specific tenant. Used by the new
// XForTenant public algorithm functions.
type tenantScopedView struct {
	g        storage.Storage
	tenantID string
}

func newTenantScopedView(g storage.Storage, tenantID string) *tenantScopedView {
	return &tenantScopedView{g: g, tenantID: tenantID}
}

func (v *tenantScopedView) AllNodes() ([]*storage.Node, error) {
	return v.g.GetAllNodesForTenant(v.tenantID)
}

func (v *tenantScopedView) Node(id uint64) (*storage.Node, error) {
	return v.g.GetNodeForTenant(id, v.tenantID)
}

func (v *tenantScopedView) OutgoingEdges(id uint64) ([]*storage.Edge, error) {
	return v.g.GetOutgoingEdgesForTenant(id, v.tenantID)
}

func (v *tenantScopedView) IncomingEdges(id uint64) ([]*storage.Edge, error) {
	return v.g.GetIncomingEdgesForTenant(id, v.tenantID)
}

func (v *tenantScopedView) Edge(id uint64) (*storage.Edge, error) {
	return v.g.GetEdgeForTenant(id, v.tenantID)
}
