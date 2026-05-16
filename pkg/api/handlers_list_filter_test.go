package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
