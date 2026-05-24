package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// postNodeLabels POSTs a label-add request to /nodes/{id}/labels with
// the tenant context wired the same way withTenant would after JWT
// extraction. Mirrors postPropertyIndex from the
// feat/expose-property-indexes-and-uniqueness PR's test style.
func postNodeLabels(t *testing.T, server *Server, nodeID uint64, tenantID string, req AddNodeLabelsRequest) *httptest.ResponseRecorder {
	t.Helper()
	r := reqWithTenant(t, http.MethodPost, "/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels", req, tenantID)
	rr := httptest.NewRecorder()
	server.handleNode(rr, r)
	return rr
}

// deleteNodeLabelHTTP DELETEs /nodes/{id}/labels/{label} with the
// tenant context wired the same way withTenant would.
func deleteNodeLabelHTTP(t *testing.T, server *Server, nodeID uint64, tenantID, label string) *httptest.ResponseRecorder {
	t.Helper()
	r := reqWithTenant(t, http.MethodDelete,
		"/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels/"+label, nil, tenantID)
	rr := httptest.NewRecorder()
	server.handleNode(rr, r)
	return rr
}

// seedNode is a small helper for tests that need a node owned by a
// known tenant. Returns the node ID.
func seedNode(t *testing.T, server *Server, tenantID string, labels []string) uint64 {
	t.Helper()
	node, err := server.graph.CreateNodeWithTenant(tenantID, labels, map[string]storage.Value{
		"seed": storage.IntValue(1),
	})
	if err != nil {
		t.Fatalf("seed CreateNodeWithTenant(%q, %v): %v", tenantID, labels, err)
	}
	return node.ID
}

// TestAddNodeLabels_HappyPath covers the canonical Ulysses use case:
// backfilling a secondary label on a node that already has a primary
// one. Asserts the response shape, the storage-side label set, and
// that the tenant-scoped label index now returns the node under the
// new label (the post-condition the consumer actually relies on for
// label-filtered vector search).
func TestAddNodeLabels_HappyPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"TextEmbedding"})

	rr := postNodeLabels(t, server, nodeID, "tenant-A", AddNodeLabelsRequest{
		Labels: []string{"CharacterEmbedding"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp AddNodeLabelsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NodeID != nodeID {
		t.Errorf("node_id=%d, want %d", resp.NodeID, nodeID)
	}
	if len(resp.Added) != 1 || resp.Added[0] != "CharacterEmbedding" {
		t.Errorf("added=%v, want [CharacterEmbedding]", resp.Added)
	}
	wantLabels := map[string]bool{"TextEmbedding": true, "CharacterEmbedding": true}
	if len(resp.Labels) != 2 {
		t.Errorf("labels len=%d, want 2: %v", len(resp.Labels), resp.Labels)
	}
	for _, l := range resp.Labels {
		if !wantLabels[l] {
			t.Errorf("unexpected label %q in response", l)
		}
	}

	// Post-condition: the tenant-scoped label index should now return
	// the node under the new label. This is the post-condition that
	// matters for graphdb's HNSW label-filtered vector search — the
	// reason the consumer asked for this surface in the first place.
	got := server.graph.GetNodesByLabelForTenant("tenant-A", "CharacterEmbedding")
	if len(got) != 1 || got[0].ID != nodeID {
		t.Errorf("GetNodesByLabelForTenant: want [%d], got %d nodes", nodeID, len(got))
	}
}

// TestAddNodeLabels_Multiple covers adding several labels in one call.
func TestAddNodeLabels_Multiple(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Base"})

	rr := postNodeLabels(t, server, nodeID, "tenant-A", AddNodeLabelsRequest{
		Labels: []string{"Alpha", "Beta", "Gamma"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp AddNodeLabelsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Added) != 3 {
		t.Errorf("added len=%d, want 3: %v", len(resp.Added), resp.Added)
	}
	if len(resp.Labels) != 4 {
		t.Errorf("labels len=%d, want 4: %v", len(resp.Labels), resp.Labels)
	}
}

// TestAddNodeLabels_Idempotent pins the contract: re-adding a label
// the node already carries is a 200, not a 409, with the duplicate
// surfacing in `already_had` (not `added`). The consumer should be
// able to retry blindly.
func TestAddNodeLabels_Idempotent(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"TextEmbedding"})

	// First add — succeeds normally.
	if rr := postNodeLabels(t, server, nodeID, "tenant-A", AddNodeLabelsRequest{
		Labels: []string{"CharacterEmbedding"},
	}); rr.Code != http.StatusOK {
		t.Fatalf("first add: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Second add of the same label — must be 200 with empty `added`
	// and the existing label in `already_had`.
	rr := postNodeLabels(t, server, nodeID, "tenant-A", AddNodeLabelsRequest{
		Labels: []string{"CharacterEmbedding"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("repeat add: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp AddNodeLabelsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Added) != 0 {
		t.Errorf("repeat add.added=%v, want []", resp.Added)
	}
	if len(resp.AlreadyHad) != 1 || resp.AlreadyHad[0] != "CharacterEmbedding" {
		t.Errorf("repeat add.already_had=%v, want [CharacterEmbedding]", resp.AlreadyHad)
	}

	// Index post-condition: still exactly one node returned for the
	// label — adding twice must NOT double-insert into the index.
	got := server.graph.GetNodesByLabelForTenant("tenant-A", "CharacterEmbedding")
	if len(got) != 1 {
		t.Errorf("after idempotent re-add: want 1 indexed node, got %d", len(got))
	}
}

// TestAddNodeLabels_Mixed exercises a request where some labels are
// new and some are already present — the same response shape covers
// both cleanly.
func TestAddNodeLabels_Mixed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"A", "B"})

	rr := postNodeLabels(t, server, nodeID, "tenant-A", AddNodeLabelsRequest{
		Labels: []string{"B", "C"}, // B already present, C is new
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp AddNodeLabelsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Added) != 1 || resp.Added[0] != "C" {
		t.Errorf("added=%v, want [C]", resp.Added)
	}
	if len(resp.AlreadyHad) != 1 || resp.AlreadyHad[0] != "B" {
		t.Errorf("already_had=%v, want [B]", resp.AlreadyHad)
	}
}

// TestAddNodeLabels_NoSuchNode covers the 404 path for a node ID
// that doesn't exist in any tenant.
func TestAddNodeLabels_NoSuchNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := postNodeLabels(t, server, 99999, "tenant-A", AddNodeLabelsRequest{
		Labels: []string{"Anything"},
	})
	if rr.Code != http.StatusNotFound {
		t.Errorf("nonexistent node: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestAddNodeLabels_TenantIsolation is the existence-leak gate:
// tenant-B trying to mutate tenant-A's node's labels must get 404
// (NOT 403 or 200), AND the node's labels must be untouched in the
// owner tenant's view. The 404-with-no-side-effect contract is the
// same one A6a established for read/update/delete.
func TestAddNodeLabels_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Original"})

	rr := postNodeLabels(t, server, nodeID, "tenant-B", AddNodeLabelsRequest{
		Labels: []string{"Tampered"},
	})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant POST: want 404 (no existence leak), got %d body=%s",
			rr.Code, rr.Body.String())
	}

	// Side-effect check: the original labels must be untouched in
	// tenant-A's view.
	got, err := server.graph.GetNodeForTenant(nodeID, "tenant-A")
	if err != nil {
		t.Fatalf("readback as owner: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "Original" {
		t.Errorf("cross-tenant add leaked through: labels=%v, want [Original]", got.Labels)
	}

	// Index post-condition: tenant-B's index map should NOT have grown
	// to include the attempted label. Belt-and-braces — a previous
	// fix-it implementation could have polluted the per-tenant index
	// even on the rejected path.
	if got := server.graph.GetNodesByLabelForTenant("tenant-B", "Tampered"); len(got) != 0 {
		t.Errorf("tenant-B index leak: GetNodesByLabelForTenant returned %d nodes, want 0", len(got))
	}
}

// TestAddNodeLabels_ValidationErrors covers the 400 cases on the
// add path: missing body, empty labels slice, invalid label format,
// over-long label.
func TestAddNodeLabels_ValidationErrors(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Base"})

	tests := []struct {
		name string
		body any
	}{
		{"empty labels array", AddNodeLabelsRequest{Labels: []string{}}},
		{"empty string label", AddNodeLabelsRequest{Labels: []string{""}}},
		{"invalid characters", AddNodeLabelsRequest{Labels: []string{"has space"}}},
		{"label with hyphen", AddNodeLabelsRequest{Labels: []string{"with-hyphen"}}},
		{"label too long", AddNodeLabelsRequest{Labels: []string{strings.Repeat("a", 51)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := reqWithTenant(t, http.MethodPost,
				"/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels", tt.body, "tenant-A")
			rr := httptest.NewRecorder()
			server.handleNode(rr, r)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("%s: want 400, got %d body=%s", tt.name, rr.Code, rr.Body.String())
			}
		})
	}
}

// TestAddNodeLabels_MalformedBody covers the 400 for non-JSON bodies.
func TestAddNodeLabels_MalformedBody(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Base"})

	req := httptest.NewRequest(http.MethodPost,
		"/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels",
		bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleNode(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("malformed body: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRemoveNodeLabel_HappyPath covers DELETE
// /nodes/{id}/labels/{label} — the canonical remove-one-label flow.
func TestRemoveNodeLabel_HappyPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"TextEmbedding", "CharacterEmbedding"})

	rr := deleteNodeLabelHTTP(t, server, nodeID, "tenant-A", "CharacterEmbedding")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Node should still exist with only the primary label.
	got, err := server.graph.GetNodeForTenant(nodeID, "tenant-A")
	if err != nil {
		t.Fatalf("GetNodeForTenant: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "TextEmbedding" {
		t.Errorf("post-delete labels=%v, want [TextEmbedding]", got.Labels)
	}

	// Index post-condition: the tenant-scoped label index for the
	// removed label must no longer return this node.
	if got := server.graph.GetNodesByLabelForTenant("tenant-A", "CharacterEmbedding"); len(got) != 0 {
		t.Errorf("post-delete index: want 0 nodes for removed label, got %d", len(got))
	}
}

// TestRemoveNodeLabel_NoSuchLabel pins the contract: removing a label
// the node doesn't carry is a 404, not a no-op 204. The consumer asked
// for something specific; we surface the absence.
func TestRemoveNodeLabel_NoSuchLabel(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"TextEmbedding"})

	rr := deleteNodeLabelHTTP(t, server, nodeID, "tenant-A", "NotPresent")
	if rr.Code != http.StatusNotFound {
		t.Errorf("missing label: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
	// Body should distinguish "label not on node" from "node not found"
	// so the consumer can branch on it without reading the path again.
	if !strings.Contains(rr.Body.String(), "Label not present") {
		t.Errorf("404 body should mention 'Label not present', got: %s", rr.Body.String())
	}
}

// TestRemoveNodeLabel_NoSuchNode covers the 404 path for an ID that
// doesn't exist in any tenant.
func TestRemoveNodeLabel_NoSuchNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	rr := deleteNodeLabelHTTP(t, server, 99999, "tenant-A", "Anything")
	if rr.Code != http.StatusNotFound {
		t.Errorf("nonexistent node: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestRemoveNodeLabel_LastLabel pins the invariant: a node must
// always carry at least one label (matches validator min=1 on create).
// Removing the last label is a 400 — the consumer can choose to
// delete the node instead.
func TestRemoveNodeLabel_LastLabel(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Only"})

	rr := deleteNodeLabelHTTP(t, server, nodeID, "tenant-A", "Only")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("last-label remove: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}

	// Side-effect check: the node still carries its only label.
	got, err := server.graph.GetNodeForTenant(nodeID, "tenant-A")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "Only" {
		t.Errorf("rejected last-label delete leaked through: labels=%v", got.Labels)
	}
}

// TestRemoveNodeLabel_TenantIsolation mirrors the add path's tenant
// gate: cross-tenant DELETE attempts return 404 and the owner's
// labels remain untouched.
func TestRemoveNodeLabel_TenantIsolation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Primary", "Secondary"})

	rr := deleteNodeLabelHTTP(t, server, nodeID, "tenant-B", "Secondary")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant DELETE: want 404, got %d body=%s", rr.Code, rr.Body.String())
	}

	got, err := server.graph.GetNodeForTenant(nodeID, "tenant-A")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if len(got.Labels) != 2 {
		t.Errorf("cross-tenant delete leaked through: labels=%v, want 2 entries", got.Labels)
	}
}

// TestNodeLabels_MethodRouting pins 405 on unsupported methods so the
// routing contract is testable independent of handler logic. Mirrors
// the property-index test's MethodRouting case.
func TestNodeLabels_MethodRouting(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Base"})

	t.Run("collection GET not allowed", func(t *testing.T) {
		// /nodes/{id}/labels accepts only POST. GET returns 405 so the
		// caller doesn't accidentally rely on listing labels (which is
		// already covered by GET /nodes/{id} — labels live on the node
		// response).
		r := reqWithTenant(t, http.MethodGet,
			"/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels", nil, "tenant-A")
		rr := httptest.NewRecorder()
		server.handleNode(rr, r)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET /labels: want 405, got %d", rr.Code)
		}
	})

	t.Run("collection PUT not allowed", func(t *testing.T) {
		r := reqWithTenant(t, http.MethodPut,
			"/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels", nil, "tenant-A")
		rr := httptest.NewRecorder()
		server.handleNode(rr, r)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("PUT /labels: want 405, got %d", rr.Code)
		}
	})

	t.Run("single POST not allowed", func(t *testing.T) {
		// /nodes/{id}/labels/{label} accepts only DELETE — POST to a
		// specific label doesn't make sense (the collection POST
		// handles add).
		r := reqWithTenant(t, http.MethodPost,
			"/nodes/"+strconv.FormatUint(nodeID, 10)+"/labels/X", nil, "tenant-A")
		rr := httptest.NewRecorder()
		server.handleNode(rr, r)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST /labels/X: want 405, got %d", rr.Code)
		}
	})
}

// TestNodeLabels_DoesNotInterceptPlainNodePath is a regression pin
// against the dispatchNodeLabelSubpath check: a plain GET /nodes/{id}
// must still route to getNode, not to the label dispatcher. Without
// the strings.Contains(path, "/labels") guard on the dispatcher this
// would still work, but adding a regression test makes the contract
// explicit so a future refactor can't accidentally swallow the path.
func TestNodeLabels_DoesNotInterceptPlainNodePath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Base"})

	r := reqWithTenant(t, http.MethodGet,
		"/nodes/"+strconv.FormatUint(nodeID, 10), nil, "tenant-A")
	rr := httptest.NewRecorder()
	server.handleNode(rr, r)
	if rr.Code != http.StatusOK {
		t.Errorf("plain GET /nodes/{id}: want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestAddNodeLabels_TooManyLabels confirms the validator's
// per-request MaxLabels=10 cap applies to the add path too — a
// caller can't bypass the create-time bound by post-create patching.
func TestAddNodeLabels_TooManyLabels(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	nodeID := seedNode(t, server, "tenant-A", []string{"Base"})

	// Send 11 labels in one request — the validator caps the per-call
	// label list at MaxLabels=10. The request must 400, NOT
	// silently truncate.
	many := make([]string, 11)
	for i := range many {
		many[i] = "L" + strconv.Itoa(i)
	}
	rr := postNodeLabels(t, server, nodeID, "tenant-A", AddNodeLabelsRequest{Labels: many})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("11 labels in one call: want 400, got %d body=%s", rr.Code, rr.Body.String())
	}
}
