package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Audit A6b (2026-05-08): cross-tenant contract for /traverse and
// /shortest-path. Pre-A6b these handlers walked tenant-blind storage
// methods (GetOutgoingEdges, GetNode), so any caller could traverse
// from a known tenant-A node and harvest the reachable tenant-B
// subgraph. The reqWithTenant helper is shared with the A6a tests.

// TestA6b_Traverse_CrossTenantStartIs404 mirrors the A6a getNode
// existence-leak guard. A tenant-B caller asking to traverse from
// tenant-A's node must get 404, indistinguishable from a missing ID.
func TestA6b_Traverse_CrossTenantStartIs404(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleTraversal(rr, reqWithTenant(t, http.MethodPost, "/traverse", TraversalRequest{
		StartNodeID: a.ID,
		MaxDepth:    2,
	}, "tenant-B"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant start: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestA6b_Traverse_StopsAtTenantBoundary is the harvest-prevention
// gate. Build A → A' (tenant-A) and A → B (cross-tenant edge); a
// tenant-A traversal must visit A' but never B.
//
// Two ways the leak could happen pre-A6b:
//  1. Tenant-stamping at storage allowed any caller to surface
//     tenant-B's reachable nodes through tenant-A's BFS.
//  2. The verifyNodeExists gap (A6a follow-up) lets an edge from A
//     point at a B-owned node ID; the dual-filter in
//     traverseFromWithContext (edge tenant + node tenant) catches
//     case 2 specifically.
func TestA6b_Traverse_StopsAtTenantBoundary(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a1, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed a1: %v", err)
	}
	a2, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed a2: %v", err)
	}
	b1, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed b1: %v", err)
	}

	// Tenant-A's own edge inside its subgraph.
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed edge a1→a2: %v", err)
	}
	// A tenant-A-stamped edge that *points at* a tenant-B node — the
	// A6a follow-up gap lets this happen because verifyNodeExists is
	// tenant-blind. The dual-filter must drop this branch anyway.
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, b1.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed edge a1→b1: %v", err)
	}
	// A tenant-B edge between tenant-B's own nodes (irrelevant to A's
	// traversal, but proves the filter is by tenant ownership not
	// reachability).
	b2, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed b2: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed edge b1→b2: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleTraversal(rr, reqWithTenant(t, http.MethodPost, "/traverse", TraversalRequest{
		StartNodeID: a1.ID,
		MaxDepth:    5,
	}, "tenant-A"))
	if rr.Code != http.StatusOK {
		t.Fatalf("traverse: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp TraversalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := make(map[uint64]bool, len(resp.Nodes))
	for _, n := range resp.Nodes {
		got[n.ID] = true
	}
	if !got[a1.ID] || !got[a2.ID] {
		t.Errorf("missing tenant-A reachable nodes: a1=%v a2=%v (got %v)", got[a1.ID], got[a2.ID], got)
	}
	if got[b1.ID] {
		t.Errorf("tenant-B node b1=%d leaked into tenant-A traversal — node-tenant filter is broken", b1.ID)
	}
	if got[b2.ID] {
		t.Errorf("tenant-B node b2=%d leaked into tenant-A traversal", b2.ID)
	}
}

// TestA6b_ShortestPath_CrossTenantStartIs404 — start endpoint case.
func TestA6b_ShortestPath_CrossTenantStartIs404(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Need a valid B-side node for end so the test isolates the start check.
	b, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleShortestPath(rr, reqWithTenant(t, http.MethodPost, "/shortest-path", ShortestPathRequest{
		StartNodeID: a.ID, // tenant-A's
		EndNodeID:   b.ID, // visible to tenant-B
	}, "tenant-B"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant start: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestA6b_ShortestPath_CrossTenantEndIs404 — end endpoint case.
// Easy to miss when adding A6a-style scoping; see the A6a
// TestUpdateNode regression for the same pattern.
func TestA6b_ShortestPath_CrossTenantEndIs404(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	b, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleShortestPath(rr, reqWithTenant(t, http.MethodPost, "/shortest-path", ShortestPathRequest{
		StartNodeID: a.ID, // visible to tenant-A
		EndNodeID:   b.ID, // tenant-B's
	}, "tenant-A"))
	if rr.Code != http.StatusNotFound {
		t.Errorf("cross-tenant end: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestA6b_ShortestPath_FindsTenantPathWhenShorterCrossTenantExists
// is the trickiest gate. Topology:
//
//	A1 ─(A)─→ X ─(A)─→ A2          (tenant-A path, length 3)
//	A1 ─(B)─→ A2                   (cross-tenant shortcut, length 2)
//
// The tenant-B-stamped shortcut is shorter. A correct A6b
// implementation filters at edge expansion: the forward queue from A1
// fetches outgoing edges via GetOutgoingEdgesForTenant("tenant-A"),
// the cross-tenant shortcut is dropped, and BFS finds A1→X→A2.
//
// Post-filtering the algorithm's *returned* path would incorrectly
// reject A1→A2 (the shortcut) and report "no path" — the regression
// this test prevents.
//
// Note: A2 has no outgoing edges, so the backward queue is a no-op
// and the assertion is really testing the forward expand. That's the
// scoping path that matters; a dedicated bidirectional test would
// belong in pkg/algorithms.
func TestA6b_ShortestPath_FindsTenantPathWhenShorterCrossTenantExists(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a1, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed a1: %v", err)
	}
	x, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed x: %v", err)
	}
	a2, err := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed a2: %v", err)
	}

	// Tenant-A's own path: a1 → x → a2 (length 3 nodes).
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, x.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed a1→x: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", x.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed x→a2: %v", err)
	}
	// Tenant-B's shortcut directly between two tenant-A node IDs —
	// uses the verifyNodeExists tenant-blindness gap to land. A
	// correct filter at edge expansion makes this invisible to A.
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed cross-tenant shortcut: %v", err)
	}

	rr := httptest.NewRecorder()
	server.handleShortestPath(rr, reqWithTenant(t, http.MethodPost, "/shortest-path", ShortestPathRequest{
		StartNodeID: a1.ID,
		EndNodeID:   a2.ID,
	}, "tenant-A"))
	if rr.Code != http.StatusOK {
		t.Fatalf("tenant-A shortest-path: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp ShortestPathResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Found {
		t.Fatalf("tenant-A path a1→x→a2 exists but BFS reported Found=false — likely post-filter regression")
	}
	if resp.Length != 3 {
		t.Errorf("want length 3 (a1→x→a2), got %d (path=%v) — possible cross-tenant shortcut leak", resp.Length, resp.Path)
	}
	wantPath := []uint64{a1.ID, x.ID, a2.ID}
	if len(resp.Path) != 3 || resp.Path[0] != wantPath[0] || resp.Path[1] != wantPath[1] || resp.Path[2] != wantPath[2] {
		t.Errorf("want path %v, got %v", wantPath, resp.Path)
	}
}
