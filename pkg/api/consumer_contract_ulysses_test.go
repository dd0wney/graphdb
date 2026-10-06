package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Consumer contracts CC17-CC22 pin the REST behaviours the Ulysses desktop app
// (dd0wney/ulysses, src-tauri/src/graphdb/) depends on. Ulysses stores writer
// data in graphdb ("graph-first, file-backed" story facts), and its own tests
// run against the bundled binary in src-tauri/tests/graphdb_contract_it.rs.
// These are PRE-EMPTIVE guards: each passes against current main and was
// teeth-proven by a temporary break recorded in the PR.

const ulyssesTenant = "default"

// wireQueryResponse and wireEdge spell the JSON keys Ulysses reads, independent
// of the server's own struct tags. Decoding into QueryResponse or EdgeResponse
// would round-trip a renamed tag and hide the break. Request bodies are
// map[string]any with literal keys for the same reason: encoding a
// QueryRequest or BatchNodeRequest would rename the key on both sides at once.
type wireQueryResponse struct {
	Rows []map[string]any `json:"rows"`
}

type wireEdge struct {
	Properties map[string]any `json:"properties"`
}

func ulyssesQuery(t *testing.T, s *Server, q string, params map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleQuery(rr, reqWithTenant(t, http.MethodPost, "/query",
		map[string]any{"query": q, "parameters": params}, ulyssesTenant))
	return rr
}

// CONSUMER CONTRACT: CC17-query-parameters-bind — ulysses (this PR)
//
// Ulysses binds every string value as a /query parameter. The query-text
// sanitizer rejects "file:" and "data:" even inside a literal and collapses
// whitespace inside literals, so a writer's text inlined into the query would
// be refused or changed. A bound value is substituted after parsing and must
// come back byte-for-byte.
func TestUlysses_QueryParametersBindVerbatim(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	value := "profile: file:x\n  two  spaces data:y"
	if rr := ulyssesQuery(t, server, "CREATE (n:UlyssesProbe {key: $k, value: $v})",
		map[string]any{"k": "p1", "v": value}); rr.Code != http.StatusOK {
		t.Fatalf("create with parameters: %d %s", rr.Code, rr.Body.String())
	}

	rr := ulyssesQuery(t, server, "MATCH (n:UlyssesProbe) WHERE n.key = $k RETURN n.value as v",
		map[string]any{"k": "p1"})
	if rr.Code != http.StatusOK {
		t.Fatalf("read with parameters: %d %s", rr.Code, rr.Body.String())
	}
	var resp wireQueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("want 1 row, got %d: %s", len(resp.Rows), rr.Body.String())
	}
	if got := resp.Rows[0]["v"]; got != value {
		t.Errorf("bound value came back as %#v, want %#v", got, value)
	}
}

// Control for CC17: the same value INLINED in the query text is refused. Without
// this, CC17 would also pass on a server whose sanitizer accepted everything,
// and it would not show that binding is what protects the value.
// CONSUMER CONTRACT: CC17-query-parameters-bind — ulysses (this PR)
func TestUlysses_InlinedValueMeetsTheSanitizer(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr := ulyssesQuery(t, server, "CREATE (n:UlyssesProbe {value: 'profile: file:x'})", nil)
	if rr.Code == http.StatusOK {
		t.Fatalf("an inlined 'file:' literal was accepted (%d); CC17's premise no longer holds", rr.Code)
	}
}

// wireBatchResponse spells the JSON keys Ulysses reads, independent of the
// server's own struct tags. Decoding into BatchNodeResponse would round-trip a
// renamed tag and hide the break.
type wireBatchResponse struct {
	Created int `json:"created"`
	Failed  int `json:"failed"`
	Errors  []struct {
		Index int `json:"index"`
	} `json:"errors"`
}

func ulyssesNode(t *testing.T, s *Server) uint64 {
	t.Helper()
	n, err := s.graph.CreateNodeWithTenant(ulyssesTenant, []string{"Character"}, map[string]storage.Value{})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	return n.ID
}

func ulyssesEdge(t *testing.T, s *Server, from, to uint64, typ string, props map[string]storage.Value) uint64 {
	t.Helper()
	e, err := s.graph.CreateEdgeWithTenant(ulyssesTenant, from, to, typ, props, 1)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
	return e.ID
}

func listEdges(t *testing.T, s *Server, query string) []wireEdge {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleEdges(rr, reqWithTenant(t, http.MethodGet, "/edges?"+query, nil, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /edges?%s: %d %s", query, rr.Code, rr.Body.String())
	}
	var edges []wireEdge
	if err := json.Unmarshal(rr.Body.Bytes(), &edges); err != nil {
		t.Fatalf("decode edges: %v body=%s", err, rr.Body.String())
	}
	return edges
}

// CONSUMER CONTRACT: CC18-edges-list-by-endpoint-type — ulysses (this PR)
//
// Ulysses lists a node's fact edges with GET /edges?from=&type= and the edges
// between two nodes with ?from=&to=&type=, and reads each edge's properties.
func TestUlysses_EdgesListByEndpointAndType(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	a, b, c := ulyssesNode(t, server), ulyssesNode(t, server), ulyssesNode(t, server)
	ulyssesEdge(t, server, a, b, "HAS_FACT", map[string]storage.Value{"k": storage.StringValue("1")})
	ulyssesEdge(t, server, a, c, "HAS_FACT", nil)
	ulyssesEdge(t, server, a, b, "OTHER", nil)

	fromA := listEdges(t, server, fmt.Sprintf("from=%d&type=HAS_FACT", a))
	if len(fromA) != 2 {
		t.Fatalf("from=a&type=HAS_FACT: want 2 edges, got %d", len(fromA))
	}
	between := listEdges(t, server, fmt.Sprintf("from=%d&to=%d&type=HAS_FACT", a, b))
	if len(between) != 1 {
		t.Fatalf("from=a&to=b&type=HAS_FACT: want 1 edge, got %d", len(between))
	}
	if got := between[0].Properties["k"]; got != "1" {
		t.Errorf("edge property k = %#v, want \"1\"", got)
	}
}

// CONSUMER CONTRACT: CC19-edge-update-merges-properties — ulysses (this PR)
//
// Ulysses updates one property of a fact edge with PUT /edges/{id} and relies
// on the other properties staying.
func TestUlysses_EdgeUpdateMergesProperties(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	a, b := ulyssesNode(t, server), ulyssesNode(t, server)
	id := ulyssesEdge(t, server, a, b, "HAS_FACT", map[string]storage.Value{
		"a": storage.StringValue("1"),
		"b": storage.StringValue("2"),
	})

	rr := httptest.NewRecorder()
	server.handleEdge(rr, reqWithTenant(t, http.MethodPut, fmt.Sprintf("/edges/%d", id),
		map[string]any{"properties": map[string]any{"b": "3"}}, ulyssesTenant))
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /edges/%d: %d %s", id, rr.Code, rr.Body.String())
	}

	edges := listEdges(t, server, fmt.Sprintf("from=%d&to=%d&type=HAS_FACT", a, b))
	if len(edges) != 1 {
		t.Fatalf("want 1 edge after update, got %d", len(edges))
	}
	if edges[0].Properties["a"] != "1" || edges[0].Properties["b"] != "3" {
		t.Errorf("properties after update = %#v, want a=1 b=3", edges[0].Properties)
	}
}

// CONSUMER CONTRACT: CC20-batch-object-shape — ulysses (this PR)
//
// Ulysses sends {"nodes": [...]} and {"edges": [...]} to the batch endpoints and
// reads created, failed, and errors[].index (the REQUEST position) to report a
// partial batch.
func TestUlysses_BatchObjectShapeReportsPartialFailure(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := httptest.NewRecorder()
	server.handleBatchNodes(rr, reqWithTenant(t, http.MethodPost, "/nodes/batch", map[string]any{
		"nodes": []map[string]any{
			{"labels": []string{"TextEmbedding"}, "properties": map[string]any{"source": "s1"}},
			{"labels": []string{}, "properties": map[string]any{"source": "bad"}}, // invalid: no labels
			{"labels": []string{"TextEmbedding"}, "properties": map[string]any{"source": "s2"}},
		},
	}, ulyssesTenant))
	if rr.Code != http.StatusCreated {
		t.Fatalf("batch nodes: %d %s", rr.Code, rr.Body.String())
	}
	var nodes wireBatchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if nodes.Created != 2 || nodes.Failed != 1 || len(nodes.Errors) != 1 || nodes.Errors[0].Index != 1 {
		t.Fatalf("batch nodes = created %d failed %d errors %+v, want 2, 1, [index 1]",
			nodes.Created, nodes.Failed, nodes.Errors)
	}

	a, b := ulyssesNode(t, server), ulyssesNode(t, server)
	rr = httptest.NewRecorder()
	server.handleBatchEdges(rr, reqWithTenant(t, http.MethodPost, "/edges/batch", map[string]any{
		"edges": []map[string]any{
			{"from_node_id": a, "to_node_id": b, "type": "KNOWS", "weight": 1},
			{"from_node_id": a, "to_node_id": 999999, "type": "KNOWS", "weight": 1}, // invalid: no such node
		},
	}, ulyssesTenant))
	if rr.Code != http.StatusCreated {
		t.Fatalf("batch edges: %d %s", rr.Code, rr.Body.String())
	}
	var edges wireBatchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &edges); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if edges.Created != 1 || edges.Failed != 1 || len(edges.Errors) != 1 || edges.Errors[0].Index != 1 {
		t.Fatalf("batch edges = created %d failed %d errors %+v, want 1, 1, [index 1]",
			edges.Created, edges.Failed, edges.Errors)
	}
}
