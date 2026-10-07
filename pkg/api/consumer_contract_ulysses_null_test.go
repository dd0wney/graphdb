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

// mergePatchPut is a PUT that declares RFC 7396's media type, with a charset
// parameter so the media-type parse is exercised rather than a string match.
func mergePatchPut(t *testing.T, path string, body any) *http.Request {
	t.Helper()
	req := reqWithTenant(t, http.MethodPut, path, body, ulyssesTenant)
	req.Header.Set("Content-Type", "application/merge-patch+json; charset=utf-8")
	return req
}

// CONSUMER CONTRACT: CC23-put-null-removes-property — ulysses (#634, opt-in since this PR)
//
// A JSON null in a PUT /nodes/{id} or PUT /edges/{id} that declares
// Content-Type: application/merge-patch+json removes that key (JSON Merge
// Patch, RFC 7396) and leaves the others. Ulysses clears an optional field by
// sending null. Plain application/json keeps storing null (CC25): changing
// that default would break the 1.x stability promise.
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
	server.handleNode(rr, mergePatchPut(t, path, map[string]any{
		"properties": map[string]any{"description": nil, "age": "38"},
	}))
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

// CONSUMER CONTRACT: CC23-put-null-removes-property — ulysses (#634, opt-in since this PR)
func TestUlysses_PutNullRemovesEdgeProperty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	a, b := ulyssesNode(t, server), ulyssesNode(t, server)
	id := ulyssesEdge(t, server, a, b, "HAS_FACT", map[string]storage.Value{
		"note": storage.StringValue("x"),
		"keep": storage.StringValue("y"),
	})

	rr := httptest.NewRecorder()
	server.handleEdge(rr, mergePatchPut(t, fmt.Sprintf("/edges/%d", id),
		map[string]any{"properties": map[string]any{"note": nil}}))
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

// CONSUMER CONTRACT: CC25-put-null-plain-json-stores-null — stability policy (this PR)
//
// A PUT /nodes/{id} or PUT /edges/{id} sent as plain application/json stores a
// null as a value, as v1.4.0 did. STABILITY_POLICY.md makes a change of result
// for an unchanged request a breaking change, so the RFC 7396 removal is
// opt-in through its media type (CC23) for the whole 1.x line.
func TestPut_PlainJSONNullStoresNull(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	n, err := server.graph.CreateNodeWithTenant(ulyssesTenant, []string{"Character"}, map[string]storage.Value{
		"description": storage.StringValue("tall"),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	a, b := ulyssesNode(t, server), ulyssesNode(t, server)
	edgeID := ulyssesEdge(t, server, a, b, "HAS_FACT", map[string]storage.Value{"note": storage.StringValue("x")})

	rr := httptest.NewRecorder()
	server.handleNode(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/nodes/%d", n.ID),
		map[string]any{"properties": map[string]any{"description": nil}}, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT node: %d %s", rr.Code, rr.Body.String())
	}
	if v, ok := decodeWireNode(t, rr).Properties["description"]; !ok || v != nil {
		t.Errorf("node description = %#v (present=%v), want present and null", v, ok)
	}

	rr = httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/edges/%d", edgeID),
		map[string]any{"properties": map[string]any{"note": nil}}, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT edge: %d %s", rr.Code, rr.Body.String())
	}
	edges := listEdges(t, server, fmt.Sprintf("from=%d&to=%d&type=HAS_FACT", a, b))
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d", len(edges))
	}
	if v, ok := edges[0].Properties["note"]; !ok || v != nil {
		t.Errorf("edge note = %#v (present=%v), want present and null", v, ok)
	}
}
