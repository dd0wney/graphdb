package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestListNodes_LabelFilter pins the ?label= query parameter behavior on
// GET /nodes. Empty values are treated as missing (so a typo like
// `?label=` returns all nodes rather than silently returning zero); a
// present-and-non-empty value routes through the typed storage primitive
// GetNodesByLabelForTenant.
//
// Tenant isolation is covered by the storage primitive's own tests and
// by the TestA6a_* tests in handlers_a6a_tenant_test.go; this test
// focuses on the new query-param dispatch.
func TestListNodes_LabelFilter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Seed a corpus mixing labels under one tenant.
	for i := 0; i < 3; i++ {
		_, _ = server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
			"title": storage.StringValue(fmt.Sprintf("doc-%d", i)),
		})
	}
	for i := 0; i < 2; i++ {
		_, _ = server.graph.CreateNodeWithTenant("tenant-A", []string{"Note"}, map[string]storage.Value{
			"title": storage.StringValue(fmt.Sprintf("note-%d", i)),
		})
	}
	// Tenant isolation control: tenant-B owns one Doc. The ?label=Doc
	// request from tenant-A must not see it.
	_, _ = server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, map[string]storage.Value{
		"title": storage.StringValue("B-secret"),
	})

	tests := []struct {
		name       string
		query      string
		wantCount  int
		wantLabels map[string]bool // any label found in the response must be one of these
	}{
		{name: "no filter returns all caller-tenant nodes", query: "", wantCount: 5, wantLabels: map[string]bool{"Doc": true, "Note": true}},
		{name: "label=Doc returns only Doc-labeled", query: "?label=Doc", wantCount: 3, wantLabels: map[string]bool{"Doc": true}},
		{name: "label=Note returns only Note-labeled", query: "?label=Note", wantCount: 2, wantLabels: map[string]bool{"Note": true}},
		{name: "label=Unknown returns empty", query: "?label=Unknown", wantCount: 0, wantLabels: map[string]bool{}},
		{name: "empty label value treated as missing", query: "?label=", wantCount: 5, wantLabels: map[string]bool{"Doc": true, "Note": true}},
		{name: "cross-tenant: tenant-A query never sees tenant-B's Doc", query: "?label=Doc", wantCount: 3, wantLabels: map[string]bool{"Doc": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithTenant(t, http.MethodGet, "/nodes"+tt.query, nil, "tenant-A")
			rr := httptest.NewRecorder()
			server.handleNodes(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
			}
			var got []NodeResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("len(nodes) = %d, want %d", len(got), tt.wantCount)
			}
			for _, n := range got {
				for _, l := range n.Labels {
					if !tt.wantLabels[l] {
						t.Errorf("unexpected label %q in response (allowed: %v)", l, tt.wantLabels)
					}
				}
			}
		})
	}
}

// TestListEdges_NewHandler_TypeFilter pins both (a) the new GET /edges
// endpoint exists at all (prior to this PR, handleEdges only routed Post
// and GET /edges returned 405 — see issue #225) and (b) the ?type=
// filter routes through GetEdgesByTypeForTenant when present, empty
// value treated as missing.
func TestListEdges_NewHandler_TypeFilter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Seed two pairs of nodes under tenant-A so the edges have valid endpoints.
	a1, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	a2, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	a3, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)

	for i := 0; i < 3; i++ {
		_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "KNOWS", nil, 1.0)
	}
	for i := 0; i < 2; i++ {
		_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a2.ID, a3.ID, "LIKES", nil, 1.0)
	}
	// Tenant isolation control: tenant-B owns one KNOWS. tenant-A's
	// ?type=KNOWS must not see it.
	b1, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"Node"}, nil)
	b2, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"Node"}, nil)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "KNOWS", nil, 1.0)

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantTypes map[string]bool
	}{
		{name: "no filter returns all caller-tenant edges", query: "", wantCount: 5, wantTypes: map[string]bool{"KNOWS": true, "LIKES": true}},
		{name: "type=KNOWS returns only KNOWS-typed", query: "?type=KNOWS", wantCount: 3, wantTypes: map[string]bool{"KNOWS": true}},
		{name: "type=LIKES returns only LIKES-typed", query: "?type=LIKES", wantCount: 2, wantTypes: map[string]bool{"LIKES": true}},
		{name: "type=Unknown returns empty", query: "?type=Unknown", wantCount: 0, wantTypes: map[string]bool{}},
		{name: "empty type value treated as missing", query: "?type=", wantCount: 5, wantTypes: map[string]bool{"KNOWS": true, "LIKES": true}},
		{name: "cross-tenant: tenant-A query never sees tenant-B's KNOWS", query: "?type=KNOWS", wantCount: 3, wantTypes: map[string]bool{"KNOWS": true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithTenant(t, http.MethodGet, "/edges"+tt.query, nil, "tenant-A")
			rr := httptest.NewRecorder()
			server.handleEdges(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status: want 200, got %d body=%s", rr.Code, rr.Body.String())
			}
			var got []EdgeResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("len(edges) = %d, want %d", len(got), tt.wantCount)
			}
			for _, e := range got {
				if !tt.wantTypes[e.Type] {
					t.Errorf("unexpected edge type %q in response (allowed: %v)", e.Type, tt.wantTypes)
				}
			}
		})
	}
}

// TestListEdges_FromToFilter pins the ?from= / ?to= filters on GET /edges,
// individually and combined (the "between" query ?from=A&to=B), and
// composed with ?type=. Validates 400 on non-numeric ID, empty value
// treated as missing, and tenant isolation under filter.
func TestListEdges_FromToFilter(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Tenant-A graph:
	//   A1 -KNOWS-> A2  (×2: two parallel KNOWS edges)
	//   A1 -KNOWS-> A3
	//   A1 -LIKES-> A3
	//   A2 -KNOWS-> A3
	a1, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	a2, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	a3, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "KNOWS", nil, 1.0)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "KNOWS", nil, 1.0)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a3.ID, "KNOWS", nil, 1.0)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a3.ID, "LIKES", nil, 1.0)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a2.ID, a3.ID, "KNOWS", nil, 1.0)

	// Tenant-B isolation control: a1-shaped ID space, a B-owned outgoing edge.
	b1, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"Node"}, nil)
	b2, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"Node"}, nil)
	_, _ = server.graph.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "KNOWS", nil, 1.0)

	a1s := strconv.FormatUint(a1.ID, 10)
	a2s := strconv.FormatUint(a2.ID, 10)
	a3s := strconv.FormatUint(a3.ID, 10)

	tests := []struct {
		name          string
		query         string
		wantStatus    int
		wantCount     int
		wantAllType   string // if non-empty, every returned edge must have this Type
		wantAllToID   uint64 // if non-zero, every returned edge must have this ToNodeID
		wantAllFromID uint64 // if non-zero, every returned edge must have this FromNodeID
	}{
		{name: "?from=A1 returns all 4 outgoing edges from A1", query: "?from=" + a1s, wantStatus: 200, wantCount: 4, wantAllFromID: a1.ID},
		{name: "?from=A2 returns A2's 1 outgoing edge", query: "?from=" + a2s, wantStatus: 200, wantCount: 1, wantAllFromID: a2.ID},
		{name: "?from=A3 returns empty (no outgoing)", query: "?from=" + a3s, wantStatus: 200, wantCount: 0},
		{name: "?to=A3 returns 3 incoming edges", query: "?to=" + a3s, wantStatus: 200, wantCount: 3, wantAllToID: a3.ID},
		{name: "?to=A1 returns empty (no incoming)", query: "?to=" + a1s, wantStatus: 200, wantCount: 0},
		{name: "?from=A1&to=A2 returns 2 (parallel edges)", query: "?from=" + a1s + "&to=" + a2s, wantStatus: 200, wantCount: 2, wantAllFromID: a1.ID, wantAllToID: a2.ID},
		{name: "?from=A1&type=KNOWS returns 3 (2 to A2 + 1 to A3)", query: "?from=" + a1s + "&type=KNOWS", wantStatus: 200, wantCount: 3, wantAllType: "KNOWS", wantAllFromID: a1.ID},
		{name: "?from=A1&type=LIKES returns 1", query: "?from=" + a1s + "&type=LIKES", wantStatus: 200, wantCount: 1, wantAllType: "LIKES", wantAllFromID: a1.ID},
		{name: "?to=A3&type=KNOWS returns 2", query: "?to=" + a3s + "&type=KNOWS", wantStatus: 200, wantCount: 2, wantAllType: "KNOWS", wantAllToID: a3.ID},
		{name: "?from=A1&to=A3&type=KNOWS returns 1", query: "?from=" + a1s + "&to=" + a3s + "&type=KNOWS", wantStatus: 200, wantCount: 1, wantAllType: "KNOWS", wantAllFromID: a1.ID, wantAllToID: a3.ID},
		{name: "?from=invalid returns 400", query: "?from=not-a-number", wantStatus: 400},
		{name: "?to=invalid returns 400", query: "?to=abc", wantStatus: 400},
		{name: "empty ?from= treated as missing", query: "?from=", wantStatus: 200, wantCount: 5},
		{name: "empty ?to= treated as missing", query: "?to=", wantStatus: 200, wantCount: 5},
		{name: "cross-tenant: tenant-A ?from=B1 (an A-tenant ID is being queried; B's edge belongs to B's node space so primitive returns empty)", query: "?from=" + strconv.FormatUint(b1.ID, 10), wantStatus: 200, wantCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithTenant(t, http.MethodGet, "/edges"+tt.query, nil, "tenant-A")
			rr := httptest.NewRecorder()
			server.handleEdges(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status: want %d, got %d body=%s", tt.wantStatus, rr.Code, rr.Body.String())
			}
			if tt.wantStatus != http.StatusOK {
				return
			}
			var got []EdgeResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(got) != tt.wantCount {
				t.Errorf("len(edges) = %d, want %d (got=%+v)", len(got), tt.wantCount, got)
			}
			for _, e := range got {
				if tt.wantAllType != "" && e.Type != tt.wantAllType {
					t.Errorf("edge type = %q, want %q", e.Type, tt.wantAllType)
				}
				if tt.wantAllFromID != 0 && e.FromNodeID != tt.wantAllFromID {
					t.Errorf("edge FromNodeID = %d, want %d", e.FromNodeID, tt.wantAllFromID)
				}
				if tt.wantAllToID != 0 && e.ToNodeID != tt.wantAllToID {
					t.Errorf("edge ToNodeID = %d, want %d", e.ToNodeID, tt.wantAllToID)
				}
			}
		})
	}
}
