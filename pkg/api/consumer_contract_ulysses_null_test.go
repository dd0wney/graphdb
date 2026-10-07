package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// wireNode spells the key Ulysses reads, independent of NodeResponse's tags.
type wireNode struct {
	Properties map[string]any `json:"properties"`
}

func decodeWireNode(t *testing.T, rr *httptest.ResponseRecorder) wireNode {
	t.Helper()
	var n wireNode
	if err := json.Unmarshal(rr.Body.Bytes(), &n); err != nil {
		t.Fatalf("decode node: %v body=%s", err, rr.Body.String())
	}
	return n
}

// CONSUMER CONTRACT: CC23-put-null-removes-property — ulysses (this PR)
//
// A JSON null in PUT /nodes/{id} or PUT /edges/{id} removes that key (JSON
// Merge Patch, RFC 7396) and leaves the others. Ulysses clears an optional
// field by sending null; before this, PUT merged, a null was stored as a
// value, and no REST request could remove a property.
func TestUlysses_PutNullRemovesNodeProperty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	n, err := server.graph.CreateNodeWithTenant(ulyssesTenant, []string{"Character"}, map[string]storage.Value{
		"description": storage.StringValue("tall"),
		"name":        storage.StringValue("Leopold"),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	path := fmt.Sprintf("/nodes/%d", n.ID)

	rr := httptest.NewRecorder()
	server.handleNode(rr, reqWithTenant(t, http.MethodPut, path, map[string]any{
		"properties": map[string]any{"description": nil, "age": "38"},
	}, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT %s: %d %s", path, rr.Code, rr.Body.String())
	}
	assertClearedNode(t, "PUT response", decodeWireNode(t, rr))

	rr = httptest.NewRecorder()
	server.handleNode(rr, reqWithTenant(t, http.MethodGet, path, nil, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, rr.Code, rr.Body.String())
	}
	assertClearedNode(t, "GET", decodeWireNode(t, rr))
}

func assertClearedNode(t *testing.T, where string, n wireNode) {
	t.Helper()
	if v, ok := n.Properties["description"]; ok {
		t.Errorf("%s: description = %#v, want the key removed", where, v)
	}
	if n.Properties["name"] != "Leopold" || n.Properties["age"] != "38" {
		t.Errorf("%s: properties = %#v, want name=Leopold age=38 kept", where, n.Properties)
	}
}

// CONSUMER CONTRACT: CC23-put-null-removes-property — ulysses (this PR)
func TestUlysses_PutNullRemovesEdgeProperty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	a, b := ulyssesNode(t, server), ulyssesNode(t, server)
	id := ulyssesEdge(t, server, a, b, "HAS_FACT", map[string]storage.Value{
		"note": storage.StringValue("x"),
		"keep": storage.StringValue("y"),
	})

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/edges/%d", id),
		map[string]any{"properties": map[string]any{"note": nil}}, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /edges/%d: %d %s", id, rr.Code, rr.Body.String())
	}

	edges := listEdges(t, server, fmt.Sprintf("from=%d&to=%d&type=HAS_FACT", a, b))
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	if v, ok := edges[0].Properties["note"]; ok {
		t.Errorf("note = %#v after PUT null, want the key removed", v)
	}
	if edges[0].Properties["keep"] != "y" {
		t.Errorf("properties = %#v, want keep=y kept", edges[0].Properties)
	}
}

// CONSUMER CONTRACT: CC24-create-null-stores-null — ulysses (this PR)
//
// A JSON null on create is stored as a null value and read back as null; it
// does not drop the key. Ulysses creates documents with metadata null and
// reads a null back as null (a missing key as {}). This is the deliberate
// difference from CC23, where null on PUT removes the key.
func TestUlysses_CreateNullStoresNull(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	server.handleNodes(rr, reqWithTenant(t, http.MethodPost, "/nodes", map[string]any{
		"labels":     []string{"Document"},
		"properties": map[string]any{"metadata": nil, "title": "t"},
	}, ulyssesTenant))
	if rr.Code != http.StatusCreated {
		t.Fatalf("POST /nodes: %d %s", rr.Code, rr.Body.String())
	}
	n := decodeWireNode(t, rr)
	if v, ok := n.Properties["metadata"]; !ok || v != nil {
		t.Errorf("metadata = %#v (present=%v), want present and null", v, ok)
	}
}
