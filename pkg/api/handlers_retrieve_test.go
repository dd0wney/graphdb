package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// retrieveSeedTenant creates a small connected graph for one tenant
// in the form: hub —REFERENCES→ leaf1 —REFERENCES→ leaf2.
// hub matches the query "graph database"; leaf1 and leaf2 are reachable
// only via traversal expansion.
func retrieveSeedTenant(t *testing.T, server *Server, name, suffix string) (hub, leaf1, leaf2 uint64) {
	t.Helper()
	gs := server.graph

	hubNode, err := gs.CreateNodeWithTenant(name, []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("graph database " + suffix + " hub content"),
	})
	if err != nil {
		t.Fatalf("hub %s: %v", name, err)
	}
	leaf1Node, err := gs.CreateNodeWithTenant(name, []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("first level " + suffix + " content"),
	})
	if err != nil {
		t.Fatalf("leaf1 %s: %v", name, err)
	}
	leaf2Node, err := gs.CreateNodeWithTenant(name, []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("second level " + suffix + " content"),
	})
	if err != nil {
		t.Fatalf("leaf2 %s: %v", name, err)
	}
	if _, err := gs.CreateEdgeWithTenant(name, hubNode.ID, leaf1Node.ID, "REFERENCES", nil, 1.0); err != nil {
		t.Fatalf("hub→leaf1 %s: %v", name, err)
	}
	if _, err := gs.CreateEdgeWithTenant(name, leaf1Node.ID, leaf2Node.ID, "REFERENCES", nil, 1.0); err != nil {
		t.Fatalf("leaf1→leaf2 %s: %v", name, err)
	}
	if err := server.searchIndexes.IndexForTenant(name, []string{"Doc"}, []string{"body"}); err != nil {
		t.Fatalf("index %s: %v", name, err)
	}
	return hubNode.ID, leaf1Node.ID, leaf2Node.ID
}

// retrieveReq builds a /v1/retrieve POST with the tenant context
// wired the same way withTenant middleware does.
func retrieveReq(t *testing.T, body RetrieveRequest, tenantID string) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/retrieve", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

// TestRetrieveHTTP_HappyPath pins the LangChain document shape and
// the load-bearing graph signal (Source.Path is non-empty for every
// document, ends at the document's NodeID).
func TestRetrieveHTTP_HappyPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	hub, leaf1, leaf2 := retrieveSeedTenant(t, server, "tenant-A", "A")

	rr := httptest.NewRecorder()
	server.handleRetrieve(rr, retrieveReq(t, RetrieveRequest{
		Query:   "graph database",
		K:       10,
		MaxHops: 2,
	}, "tenant-A"))

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp RetrieveResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Documents) == 0 {
		t.Fatal("want non-empty documents")
	}

	gotIDs := make(map[uint64]bool, len(resp.Documents))
	for _, d := range resp.Documents {
		gotIDs[d.Metadata.NodeID] = true
		// The graph signal — every document has a non-empty path
		// ending at its own NodeID.
		if len(d.Metadata.Source.Path) == 0 {
			t.Errorf("doc %d: empty Source.Path (load-bearing graph signal missing)", d.Metadata.NodeID)
		}
		if last := d.Metadata.Source.Path[len(d.Metadata.Source.Path)-1]; last != d.Metadata.NodeID {
			t.Errorf("doc %d: Source.Path must end at NodeID, got %v", d.Metadata.NodeID, d.Metadata.Source.Path)
		}
	}
	for _, want := range []uint64{hub, leaf1, leaf2} {
		if !gotIDs[want] {
			t.Errorf("missing node %d in result (got %v)", want, gotIDs)
		}
	}
}

// TestRetrieveHTTP_TenantIsolation: two tenants seeded with the same
// shapes; tenant-A's query must return only A's nodes, tenant-B's
// only B's. End-to-end gate covering search, expansion, and response
// shape together.
func TestRetrieveHTTP_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	aHub, aLeaf1, aLeaf2 := retrieveSeedTenant(t, server, "tenant-A", "A")
	bHub, bLeaf1, bLeaf2 := retrieveSeedTenant(t, server, "tenant-B", "B")

	for _, tn := range []struct {
		name string
		ours []uint64
		leak []uint64
	}{
		{"tenant-A", []uint64{aHub, aLeaf1, aLeaf2}, []uint64{bHub, bLeaf1, bLeaf2}},
		{"tenant-B", []uint64{bHub, bLeaf1, bLeaf2}, []uint64{aHub, aLeaf1, aLeaf2}},
	} {
		t.Run(tn.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.handleRetrieve(rr, retrieveReq(t, RetrieveRequest{
				Query:   "graph database",
				K:       10,
				MaxHops: 2,
			}, tn.name))
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
			}
			var resp RetrieveResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			got := make(map[uint64]bool, len(resp.Documents))
			for _, d := range resp.Documents {
				got[d.Metadata.NodeID] = true
			}
			for _, leak := range tn.leak {
				if got[leak] {
					t.Errorf("%s leaked foreign-tenant node %d (got %v)", tn.name, leak, got)
				}
			}
			// At least the hub should appear (proves the query worked).
			if !got[tn.ours[0]] {
				t.Errorf("%s: missing own hub %d (got %v)", tn.name, tn.ours[0], got)
			}
		})
	}
}

// TestRetrieveHTTP_EmptyQueryReturns400 pins the validation contract.
func TestRetrieveHTTP_EmptyQueryReturns400(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, q := range []string{"", "   ", "\t\n"} {
		rr := httptest.NewRecorder()
		server.handleRetrieve(rr, retrieveReq(t, RetrieveRequest{Query: q}, "tenant-A"))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("query=%q: want 400, got %d", q, rr.Code)
		}
	}
}

// TestRetrieveHTTP_MethodNotAllowed pins that GET is rejected.
func TestRetrieveHTTP_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/v1/retrieve", nil)
		req = req.WithContext(tenant.WithTenant(req.Context(), "tenant-A"))
		rr := httptest.NewRecorder()
		server.handleRetrieve(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: want 405, got %d", method, rr.Code)
		}
	}
}

// TestRetrieveHTTP_IncludeNode pins that include_node=true populates
// Metadata.Node with a hydrated NodeResponse.
func TestRetrieveHTTP_IncludeNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	retrieveSeedTenant(t, server, "tenant-A", "A")

	rr := httptest.NewRecorder()
	server.handleRetrieve(rr, retrieveReq(t, RetrieveRequest{
		Query:       "graph database",
		K:           5,
		IncludeNode: true,
	}, "tenant-A"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp RetrieveResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Documents) == 0 {
		t.Fatal("want non-empty result")
	}
	for _, d := range resp.Documents {
		if d.Metadata.Node == nil {
			t.Errorf("doc %d: Metadata.Node nil despite include_node=true", d.Metadata.NodeID)
			continue
		}
		if d.Metadata.Node.ID != d.Metadata.NodeID {
			t.Errorf("doc %d: hydrated Node.ID=%d mismatches metadata.node_id", d.Metadata.NodeID, d.Metadata.Node.ID)
		}
	}
}

// TestRetrieveHTTP_DegradedFlagForwarded pins that the hybrid-search
// degraded flag (e.g., "no-lsa-index") propagates to the response
// body and the X-GraphDB-Retrieve-Degraded header. The fixture has
// no LSA index, so this is the natural path.
func TestRetrieveHTTP_DegradedFlagForwarded(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	retrieveSeedTenant(t, server, "tenant-A", "A")

	rr := httptest.NewRecorder()
	server.handleRetrieve(rr, retrieveReq(t, RetrieveRequest{
		Query: "graph database",
		K:     5,
	}, "tenant-A"))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var resp RetrieveResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Degraded != "no-lsa-index" {
		t.Errorf("want Degraded=\"no-lsa-index\" (no LSA built in fixture), got %q", resp.Degraded)
	}
	if h := rr.Header().Get(HeaderRetrieveDegraded); h != "no-lsa-index" {
		t.Errorf("want header %q=%q, got %q", HeaderRetrieveDegraded, "no-lsa-index", h)
	}
}
