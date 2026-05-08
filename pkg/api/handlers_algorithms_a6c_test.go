package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// Audit A6c-algorithms (2026-05-08): HTTP-level cross-tenant
// isolation gate for /algorithms. Pre-fix, every algorithm computed
// against the entire multi-tenant graph regardless of caller — a
// JWT-authenticated tenant-A caller could inspect tenant-B's
// PageRank, betweenness, cycles, etc. This test pins the contract
// end-to-end via the algorithm dispatcher.

// algorithmReqWithTenant builds a /algorithms POST with the tenant
// context wired the same way withTenant middleware does.
func algorithmReqWithTenant(t *testing.T, algName string, tenantID string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"algorithm":  algName,
		"parameters": map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/algorithms", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

// TestA6c_Algorithms_PageRankIsolation pins that PageRank only ranks
// the caller's tenant nodes. Two tenants each get a 3-node chain;
// each tenant's PageRank result must contain only its own 3 IDs.
func TestA6c_Algorithms_PageRankIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a1, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a2, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a3, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "REL", nil, 1.0); err != nil {
		t.Fatalf("seed a1→a2: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a2.ID, a3.ID, "REL", nil, 1.0); err != nil {
		t.Fatalf("seed a2→a3: %v", err)
	}

	b1, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b2, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b3, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "REL", nil, 1.0); err != nil {
		t.Fatalf("seed b1→b2: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", b2.ID, b3.ID, "REL", nil, 1.0); err != nil {
		t.Fatalf("seed b2→b3: %v", err)
	}

	scoresFor := func(t *testing.T, tenantID string) map[uint64]float64 {
		t.Helper()
		rr := httptest.NewRecorder()
		server.handleAlgorithm(rr, algorithmReqWithTenant(t, "pagerank", tenantID))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp struct {
			Results map[string]any `json:"results"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		raw, _ := resp.Results["scores"].(map[string]any)
		out := make(map[uint64]float64, len(raw))
		for k, v := range raw {
			var id uint64
			for _, c := range []byte(k) {
				if c < '0' || c > '9' {
					id = 0
					break
				}
				id = id*10 + uint64(c-'0')
			}
			if score, ok := v.(float64); ok {
				out[id] = score
			}
		}
		return out
	}

	t.Run("tenant-A only ranks A's nodes", func(t *testing.T) {
		s := scoresFor(t, "tenant-A")
		if len(s) != 3 {
			t.Errorf("tenant-A: want 3 scored nodes, got %d (%v)", len(s), s)
		}
		for _, want := range []uint64{a1.ID, a2.ID, a3.ID} {
			if _, ok := s[want]; !ok {
				t.Errorf("tenant-A missing own node %d", want)
			}
		}
		for _, leak := range []uint64{b1.ID, b2.ID, b3.ID} {
			if _, ok := s[leak]; ok {
				t.Errorf("tenant-A leaked tenant-B node %d", leak)
			}
		}
	})

	t.Run("tenant-B only ranks B's nodes", func(t *testing.T) {
		s := scoresFor(t, "tenant-B")
		if len(s) != 3 {
			t.Errorf("tenant-B: want 3 scored nodes, got %d (%v)", len(s), s)
		}
		for _, want := range []uint64{b1.ID, b2.ID, b3.ID} {
			if _, ok := s[want]; !ok {
				t.Errorf("tenant-B missing own node %d", want)
			}
		}
	})
}

// TestA6c_Algorithms_TrianglesIsolation pins that CountTriangles
// scopes to the caller's tenant. Each tenant gets a triangle; the
// /algorithms triangles result must show count=1 for each.
func TestA6c_Algorithms_TrianglesIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Tenant-A triangle: a1↔a2↔a3↔a1.
	a1, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a2, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	a3, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"N"}, nil)
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "R", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a2.ID, a3.ID, "R", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-A", a3.ID, a1.ID, "R", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Tenant-B triangle: b1↔b2↔b3↔b1.
	b1, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b2, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	b3, _ := server.graph.CreateNodeWithTenant("tenant-B", []string{"N"}, nil)
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", b1.ID, b2.ID, "R", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", b2.ID, b3.ID, "R", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := server.graph.CreateEdgeWithTenant("tenant-B", b3.ID, b1.ID, "R", nil, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, tn := range []string{"tenant-A", "tenant-B"} {
		t.Run(tn+" sees only its own triangle", func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.handleAlgorithm(rr, algorithmReqWithTenant(t, "triangles", tn))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
			}
			var resp struct {
				Results map[string]any `json:"results"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			gc, _ := resp.Results["global_count"].(float64)
			if int(gc) != 1 {
				t.Errorf("%s global triangle count: want 1, got %d (results=%v)", tn, int(gc), resp.Results)
			}
		})
	}
}
