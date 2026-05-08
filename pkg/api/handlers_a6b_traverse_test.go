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
// gate. Tenant-A and tenant-B each have their own connected subgraph;
// a tenant-A traversal must stay in A's subgraph and never surface
// any of B's nodes.
//
// Pre-A6b leak path was direct: GetOutgoingEdges and GetNode were
// tenant-blind, so any reachable node was returned regardless of
// owner. The A6a-followup gap-closure (verifyNodeExists tenant-strict
// in CreateEdgeWithTenant) means cross-tenant edges can no longer be
// constructed via public API; this test now validates the simpler
// "two disjoint subgraphs" scenario, which is the realistic shape
// after gap closure.
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
	b2, err := server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	if err != nil {
		t.Fatalf("seed b2: %v", err)
	}

	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "REL", nil, 0); err != nil {
		t.Fatalf("seed edge a1→a2: %v", err)
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
	if got[b1.ID] || got[b2.ID] {
		t.Errorf("tenant-B nodes leaked into tenant-A traversal: b1=%v b2=%v", got[b1.ID], got[b2.ID])
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

// (TestA6b_ShortestPath_FindsTenantPathWhenShorterCrossTenantExists
// was removed in the A6a follow-up: after CreateEdgeWithTenant became
// tenant-strict on node verification, cross-tenant-stamped edges can
// no longer be constructed via public API. The "shorter cross-tenant
// shortcut" scenario it pinned is now mathematically prevented by
// pkg/storage/tenant_signatures_test.go's
// TestCreateEdgeWithTenant_CrossTenantNodeRefIsRefused. Re-enabling
// this test would require an unsafe internal-edge-insertion helper
// whose cost outweighs the defense-in-depth value.)
