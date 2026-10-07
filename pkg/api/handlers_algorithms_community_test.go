package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func communityTriangle(t *testing.T, s *Server, tenantID string) []uint64 {
	t.Helper()
	ids := make([]uint64, 0, 3)
	for i := 0; i < 3; i++ {
		n, err := s.graph.CreateNodeWithTenant(tenantID, []string{"Character"}, map[string]storage.Value{})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		ids = append(ids, n.ID)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.graph.CreateEdgeWithTenant(tenantID, ids[i], ids[(i+1)%3], "KNOWS", nil, 1); err != nil {
			t.Fatalf("create edge: %v", err)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

type communityWire struct {
	Results struct {
		Communities []map[string]json.RawMessage `json:"communities"`
		Count       int                          `json:"count"`
	} `json:"results"`
}

// CONSUMER CONTRACT: CC21-community-label-propagation — ulysses (this PR)
//
// Ulysses groups characters into communities in its Forge view. It calls
// POST /algorithms with "label_propagation" and reads results.communities
// as [{id, nodes}] with LOWERCASE keys (the scc route sends Go's untagged
// "ID"/"Nodes", so this shape is built explicitly and pinned here). Two
// clusters with no edge between them must come back as two communities, and
// another tenant's nodes must not appear.
func TestAlgorithms_LabelPropagationReturnsTenantCommunities(t *testing.T) {
	for _, alg := range []string{"label_propagation", "connected_components"} {
		t.Run(alg, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()

			a := communityTriangle(t, server, "owner")
			b := communityTriangle(t, server, "owner")
			_ = communityTriangle(t, server, "intruder")

			rr := httptest.NewRecorder()
			server.handleAlgorithm(rr, algorithmReqWithTenant(t, alg, "owner"))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d, want 200: %s", rr.Code, rr.Body.String())
			}

			var resp communityWire
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode: %v body=%s", err, rr.Body.String())
			}
			if resp.Results.Count != 2 || len(resp.Results.Communities) != 2 {
				t.Fatalf("want 2 communities, got count=%d len=%d: %s",
					resp.Results.Count, len(resp.Results.Communities), rr.Body.String())
			}
			var got [][]uint64
			for _, c := range resp.Results.Communities {
				for _, key := range []string{"id", "nodes", "size"} {
					if _, ok := c[key]; !ok {
						t.Fatalf("community lacks lowercase key %q: %s", key, rr.Body.String())
					}
				}
				var nodes []uint64
				if err := json.Unmarshal(c["nodes"], &nodes); err != nil {
					t.Fatalf("nodes: %v", err)
				}
				var size int
				if err := json.Unmarshal(c["size"], &size); err != nil {
					t.Fatalf("size: %v", err)
				}
				if size != len(nodes) {
					t.Errorf("community size = %d, want len(nodes) = %d: %s", size, len(nodes), rr.Body.String())
				}
				sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
				got = append(got, nodes)
			}
			sort.Slice(got, func(i, j int) bool { return got[i][0] < got[j][0] })
			for i, want := range [][]uint64{a, b} {
				if !reflect.DeepEqual(got[i], want) {
					t.Errorf("community %d = %v, want %v", i, got[i], want)
				}
			}
		})
	}
}

func TestAlgorithms_LabelPropagationRejectsABadIterationCount(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	for _, bad := range []any{0, 1001, 2.5, "ten"} {
		req := reqWithTenant(t, http.MethodPost, "/algorithms", map[string]any{
			"algorithm":  "label_propagation",
			"parameters": map[string]any{"max_iterations": bad},
		}, "owner")
		rr := httptest.NewRecorder()
		server.handleAlgorithm(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("max_iterations=%v: status %d, want 400", bad, rr.Code)
		}
		if !strings.Contains(rr.Body.String(), "max_iterations") {
			t.Errorf("max_iterations=%v: body %q does not name max_iterations", bad, rr.Body.String())
		}
	}
}

// The accepted range of max_iterations is 1 to 1000. The rejection test above
// pins the outside of the range, and this pins the edges and the default, so an
// off-by-one in the bound cannot turn a valid request into a 400.
func TestAlgorithms_LabelPropagationAcceptsTheIterationBoundaries(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	cases := map[string]map[string]any{
		"omitted": {},
		"one":     {"max_iterations": 1},
		"max":     {"max_iterations": 1000},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			req := reqWithTenant(t, http.MethodPost, "/algorithms", map[string]any{
				"algorithm":  "label_propagation",
				"parameters": params,
			}, "owner")
			rr := httptest.NewRecorder()
			server.handleAlgorithm(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("parameters %v: status %d, want 200: %s", params, rr.Code, rr.Body.String())
			}
		})
	}
}
