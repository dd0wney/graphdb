package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Issue #455: handleBatchNodes / handleBatchEdges silently `continue` past
// invalid or storage-rejected items, so a caller has no way to know an item
// was dropped or why. These tests pin the additive fix: a `failed` count and
// an `errors` array (index = position in the REQUEST array, since dropped
// items are omitted from the response array entirely) — without changing
// any existing field's meaning, ordering, or the partial-success semantics
// that consumer contract CC7 (TestBatchNodes_PartialOutOfOrderEchoesProperties)
// depends on.

// TestHandleBatchNodes_PartialFailure_ReportsIndexAndError posts 3 node
// requests with the middle one invalid (empty Labels fails validator's
// `required,min=1`) and asserts the two valid nodes still come back
// (partial-success unchanged) while the response also reports exactly one
// failure at request-index 1 with a non-empty error message.
func TestHandleBatchNodes_PartialFailure_ReportsIndexAndError(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	req := BatchNodeRequest{
		Nodes: []NodeRequest{
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": "a"}},
			{Labels: []string{}, Properties: map[string]any{"_key": "bad"}}, // invalid: no labels
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": "c"}},
		},
	}

	rr := httptest.NewRecorder()
	server.handleBatchNodes(rr, reqWithTenant(t, http.MethodPost, "/nodes/batch", req, tenantID))
	if rr.Code != http.StatusCreated {
		t.Fatalf("handleBatchNodes: want 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp BatchNodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}

	if resp.Created != 2 {
		t.Errorf("Created = %d, want 2", resp.Created)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("len(Nodes) = %d, want 2", len(resp.Nodes))
	}
	if resp.Failed != 1 {
		t.Errorf("Failed = %d, want 1", resp.Failed)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(resp.Errors))
	}
	if resp.Errors[0].Index != 1 {
		t.Errorf("Errors[0].Index = %d, want 1 (position in the REQUEST array)", resp.Errors[0].Index)
	}
	if resp.Errors[0].Error == "" {
		t.Errorf("Errors[0].Error is empty, want a non-empty validation message")
	}
}

// TestHandleBatchNodes_AllValid_OmitsErrorsFromJSON posts an all-valid batch
// and asserts not just that Failed == 0, but that the `errors` key is
// genuinely ABSENT from the raw JSON — proving `omitempty` on a nil slice
// behaves as intended rather than serializing `"errors":null` or `[]`.
func TestHandleBatchNodes_AllValid_OmitsErrorsFromJSON(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	req := BatchNodeRequest{
		Nodes: []NodeRequest{
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": "a"}},
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": "b"}},
		},
	}

	rr := httptest.NewRecorder()
	server.handleBatchNodes(rr, reqWithTenant(t, http.MethodPost, "/nodes/batch", req, tenantID))
	if rr.Code != http.StatusCreated {
		t.Fatalf("handleBatchNodes: want 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp BatchNodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if resp.Failed != 0 {
		t.Errorf("Failed = %d, want 0", resp.Failed)
	}

	var raw map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, ok := raw["errors"]; ok {
		t.Errorf(`"errors" key present in JSON for an all-valid batch, want it omitted via omitempty; body=%s`, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"errors"`) {
		t.Errorf(`raw JSON body contains "errors" for an all-valid batch: %s`, rr.Body.String())
	}
}

// TestHandleBatchEdges_PartialFailure_ReportsIndexAndError mirrors the node
// case for handleBatchEdges. Two real nodes are created first so the edges
// have valid from/to IDs; the invalid item uses an empty Type (validator's
// `required,min=1` on EdgeRequest.Type) rather than a fabricated storage
// error, matching the validation-continue path in handlers_edges.go.
func TestHandleBatchEdges_PartialFailure_ReportsIndexAndError(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	n1, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Process"}, nil)
	if err != nil {
		t.Fatalf("create n1: %v", err)
	}
	n2, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Process"}, nil)
	if err != nil {
		t.Fatalf("create n2: %v", err)
	}

	req := BatchEdgeRequest{
		Edges: []EdgeRequest{
			{FromNodeID: n1.ID, ToNodeID: n2.ID, Type: "USES", Weight: 1.0},
			{FromNodeID: n1.ID, ToNodeID: n2.ID, Type: "", Weight: 1.0}, // invalid: empty Type
			{FromNodeID: n2.ID, ToNodeID: n1.ID, Type: "USES", Weight: 1.0},
		},
	}

	rr := httptest.NewRecorder()
	server.handleBatchEdges(rr, reqWithTenant(t, http.MethodPost, "/edges/batch", req, tenantID))
	if rr.Code != http.StatusCreated {
		t.Fatalf("handleBatchEdges: want 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp BatchEdgeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}

	if resp.Created != 2 {
		t.Errorf("Created = %d, want 2", resp.Created)
	}
	if len(resp.Edges) != 2 {
		t.Fatalf("len(Edges) = %d, want 2", len(resp.Edges))
	}
	if resp.Failed != 1 {
		t.Errorf("Failed = %d, want 1", resp.Failed)
	}
	if len(resp.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(resp.Errors))
	}
	if resp.Errors[0].Index != 1 {
		t.Errorf("Errors[0].Index = %d, want 1 (position in the REQUEST array)", resp.Errors[0].Index)
	}
	if resp.Errors[0].Error == "" {
		t.Errorf("Errors[0].Error is empty, want a non-empty validation message")
	}
}

// TestHandleBatchEdges_AllValid_OmitsErrorsFromJSON mirrors
// TestHandleBatchNodes_AllValid_OmitsErrorsFromJSON for edges.
func TestHandleBatchEdges_AllValid_OmitsErrorsFromJSON(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	n1, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Process"}, nil)
	if err != nil {
		t.Fatalf("create n1: %v", err)
	}
	n2, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Process"}, nil)
	if err != nil {
		t.Fatalf("create n2: %v", err)
	}

	req := BatchEdgeRequest{
		Edges: []EdgeRequest{
			{FromNodeID: n1.ID, ToNodeID: n2.ID, Type: "USES", Weight: 1.0},
			{FromNodeID: n2.ID, ToNodeID: n1.ID, Type: "USES", Weight: 1.0},
		},
	}

	rr := httptest.NewRecorder()
	server.handleBatchEdges(rr, reqWithTenant(t, http.MethodPost, "/edges/batch", req, tenantID))
	if rr.Code != http.StatusCreated {
		t.Fatalf("handleBatchEdges: want 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp BatchEdgeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if resp.Failed != 0 {
		t.Errorf("Failed = %d, want 0", resp.Failed)
	}

	var raw map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, ok := raw["errors"]; ok {
		t.Errorf(`"errors" key present in JSON for an all-valid batch, want it omitted via omitempty; body=%s`, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"errors"`) {
		t.Errorf(`raw JSON body contains "errors" for an all-valid batch: %s`, rr.Body.String())
	}
}
