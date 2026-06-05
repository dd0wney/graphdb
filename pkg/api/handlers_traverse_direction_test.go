package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// #331: /traverse now honours `direction` and `edge_types` (previously decoded
// and silently ignored).

func traverseIDs(t *testing.T, s *Server, tenantID string, req TraversalRequest) (int, map[uint64]bool) {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleTraversal(rr, reqWithTenant(t, http.MethodPost, "/traverse", req, tenantID))
	ids := map[uint64]bool{}
	if rr.Code == http.StatusOK {
		var resp TraversalResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode traverse: %v", err)
		}
		for _, n := range resp.Nodes {
			ids[n.ID] = true
		}
	}
	return rr.Code, ids
}

func TestTraverse_DirectionHonored(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tn = "default"
	a, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)
	b, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)
	c, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)
	// a -> b (a's outgoing); c -> a (a's incoming)
	if _, err := server.graph.CreateEdgeWithTenant(tn, a.ID, b.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge a->b: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant(tn, c.ID, a.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge c->a: %v", err)
	}

	cases := []struct {
		dir  string
		want map[uint64]bool
	}{
		{"outgoing", map[uint64]bool{a.ID: true, b.ID: true}},
		{"incoming", map[uint64]bool{a.ID: true, c.ID: true}},
		{"both", map[uint64]bool{a.ID: true, b.ID: true, c.ID: true}},
		{"", map[uint64]bool{a.ID: true, b.ID: true}}, // default = outgoing
	}
	for _, tc := range cases {
		code, got := traverseIDs(t, server, tn, TraversalRequest{StartNodeID: a.ID, MaxDepth: 1, Direction: tc.dir})
		if code != http.StatusOK {
			t.Fatalf("direction %q: status %d, want 200", tc.dir, code)
		}
		if len(got) != len(tc.want) {
			t.Errorf("direction %q: got %v, want %v", tc.dir, got, tc.want)
			continue
		}
		for id := range tc.want {
			if !got[id] {
				t.Errorf("direction %q: missing node %d (got %v)", tc.dir, id, got)
			}
		}
	}
}

func TestTraverse_EdgeTypeFilter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tn = "default"
	a, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)
	b, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)
	d, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)
	server.graph.CreateEdgeWithTenant(tn, a.ID, b.ID, "T1", nil, 1.0) //nolint:errcheck // test setup
	server.graph.CreateEdgeWithTenant(tn, a.ID, d.ID, "T2", nil, 1.0) //nolint:errcheck // test setup

	code, got := traverseIDs(t, server, tn, TraversalRequest{StartNodeID: a.ID, MaxDepth: 1, EdgeTypes: []string{"T1"}})
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if !got[a.ID] || !got[b.ID] {
		t.Errorf("want {a,b} via T1, got %v", got)
	}
	if got[d.ID] {
		t.Errorf("node %d (reached via T2) leaked past edge_types=[T1]", d.ID)
	}
}

func TestTraverse_InvalidDirection(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	const tn = "default"
	a, _ := server.graph.CreateNodeWithTenant(tn, []string{"N"}, nil)

	code, _ := traverseIDs(t, server, tn, TraversalRequest{StartNodeID: a.ID, MaxDepth: 1, Direction: "sideways"})
	if code != http.StatusBadRequest {
		t.Errorf("invalid direction: status %d, want 400", code)
	}
}
