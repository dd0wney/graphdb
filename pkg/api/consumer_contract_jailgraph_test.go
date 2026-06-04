package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Consumer contracts CC7–CC9 pin the REST behaviours the jailgraph consumer
// depends on (ingest → profile → audit, over the REST API only — no internal
// coupling). See docs/CONSUMER_CONTRACTS.md and
// ../jailgraph/docs/GRAPHDB_CONTRACTS_HANDOFF.md.
//
// Unlike CC1–CC6 (each written red against a real bug), these are PRE-EMPTIVE
// guards: they pass against current main. jailgraph requested them because the
// in-flight storage-hardening wave could silently change a relied-on behaviour
// and no regression would catch it. A characterization test that passes on day
// one only earns its keep if it would go RED on the realistic regression — each
// test below was teeth-proven by temporarily breaking the behaviour it pins
// (echo, pagination, neighbour expansion) and confirming failure before revert.

// CONSUMER CONTRACT: CC7-batch-partial-echo — jailgraph (#319)
//
// TestBatchNodes_PartialOutOfOrderEchoesProperties pins POST /nodes/batch as a
// partial-success path that echoes each created node's properties so a client
// can reconcile assigned IDs to a client-supplied correlation key. jailgraph
// has no server-side dedup for its labels, so the ingest worker maps the batch
// response back to its requests by an echoed `_key` property — NOT by index.
//
// The deliberately-invalid node (empty labels → validation `min=1` rejects it)
// sits in the MIDDLE of the batch: that makes partial-success meaningful and
// proves the response is reconciled by `_key`, not position. The contract is
// order-AGNOSTIC — we assert the `_key`→id mapping and the dropped key's
// absence, never positional order (the handler happens to preserve request
// order, but pinning that would contradict the "unspecified order" contract).
func TestBatchNodes_PartialOutOfOrderEchoesProperties(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"

	// Three valid nodes with distinct correlation keys, one invalid node
	// (no labels) wedged in the middle to force a partial result.
	const droppedKey = "proc:run1:DROPPED"
	validKeys := []string{"proc:run1:100", "proc:run1:200", "proc:run1:300"}
	req := BatchNodeRequest{
		Nodes: []NodeRequest{
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": validKeys[0]}},
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": validKeys[1]}},
			{Labels: []string{}, Properties: map[string]any{"_key": droppedKey}}, // invalid: no labels
			{Labels: []string{"Process"}, Properties: map[string]any{"_key": validKeys[2]}},
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

	// (a) Partial success: only the valid nodes are returned.
	if resp.Created != len(validKeys) {
		t.Errorf("Created = %d, want %d (invalid node must be skipped, not error the batch)", resp.Created, len(validKeys))
	}
	if len(resp.Nodes) != len(validKeys) {
		t.Fatalf("len(Nodes) = %d, want %d", len(resp.Nodes), len(validKeys))
	}

	// (b) Reconcile by echoed `_key` — the contract jailgraph actually uses.
	byKey := make(map[string]uint64)
	for _, n := range resp.Nodes {
		raw, ok := n.Properties["_key"]
		if !ok {
			t.Fatalf("node %d response omits the `_key` property — break the IDs→key reconciliation jailgraph relies on", n.ID)
		}
		key, ok := raw.(string)
		if !ok {
			t.Fatalf("node %d `_key` echoed as %T, want string", n.ID, raw)
		}
		if n.ID == 0 {
			t.Errorf("node for _key=%q has zero ID", key)
		}
		byKey[key] = n.ID
	}

	for _, k := range validKeys {
		if _, ok := byKey[k]; !ok {
			t.Errorf("valid _key %q absent from response — cannot reconcile its assigned ID", k)
		}
	}

	// (c) The dropped node's correlation key must NOT appear.
	if id, ok := byKey[droppedKey]; ok {
		t.Errorf("dropped node's _key %q present (id=%d) — invalid node leaked into the response", droppedKey, id)
	}
}

// CONSUMER CONTRACT: CC8-label-list-properties-paginated — jailgraph (#319)
//
// TestNodesByLabel_ReturnsPropertiesAcrossPages pins GET /nodes?label= as a
// listing that (a) includes each node's properties and (b) is followable to
// completion via the X-Next-Cursor header. jailgraph's ingest worker rebuilds
// its natural-key→ID cache across runs by listing a label and reading each
// node's `_key`; internal/profile.Collect enumerates a run's Process nodes the
// same way. A partial fetch (or a page that strips properties) silently
// duplicates shared nodes on the next run — there is no server-side dedup to
// save it.
//
// The load-bearing assertion is "every node, with its `_key` intact, on EVERY
// page" — not just the total count. We page with a small limit so completion
// genuinely depends on following the cursor.
func TestNodesByLabel_ReturnsPropertiesAcrossPages(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"
	const label = "Process"
	const total = 5
	const pageLimit = 2

	want := make(map[string]bool, total) // expected _key set
	for i := 0; i < total; i++ {
		key := fmt.Sprintf("proc:run1:%d", i)
		want[key] = true
		if _, err := server.graph.CreateNodeWithTenant(tenantID, []string{label}, map[string]storage.Value{
			"_key": storage.StringValue(key),
		}); err != nil {
			t.Fatalf("CreateNodeWithTenant: %v", err)
		}
	}

	got := make(map[string]uint64)
	cursor := ""
	// Cap iterations so a regression that never clears X-Next-Cursor fails
	// fast instead of hanging the suite. total/pageLimit pages + slack.
	const maxPages = total + 2
	for page := 0; ; page++ {
		if page >= maxPages {
			t.Fatalf("pagination did not terminate after %d pages — X-Next-Cursor never cleared", maxPages)
		}
		path := fmt.Sprintf("/nodes?label=%s&limit=%d", label, pageLimit)
		if cursor != "" {
			path += "&cursor=" + cursor
		}

		rr := httptest.NewRecorder()
		server.listNodes(rr, reqWithTenant(t, http.MethodGet, path, nil, tenantID))
		if rr.Code != http.StatusOK {
			t.Fatalf("listNodes page %d: want 200, got %d: %s", page, rr.Code, rr.Body.String())
		}

		var nodes []NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &nodes); err != nil {
			t.Fatalf("decode page %d: %v", page, err)
		}

		for _, n := range nodes {
			raw, ok := n.Properties["_key"]
			if !ok {
				t.Fatalf("node %d on page %d omits `_key` — label listing dropped properties", n.ID, page)
			}
			key, ok := raw.(string)
			if !ok {
				t.Fatalf("node %d `_key` is %T, want string", n.ID, raw)
			}
			if prev, dup := got[key]; dup {
				t.Errorf("_key %q returned twice (ids %d and %d) across pages", key, prev, n.ID)
			}
			got[key] = n.ID
		}

		cursor = rr.Header().Get(CursorHeader)
		if cursor == "" {
			break // last page
		}
	}

	if len(got) != total {
		t.Errorf("collected %d distinct nodes across pages, want %d", len(got), total)
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("_key %q never returned — pagination did not reach completion", key)
		}
	}
}

// CONSUMER CONTRACT: CC9-traverse-outgoing-depth — jailgraph (#319)
//
// TestTraverse_OutgoingNeighborsAtDepth pins POST /traverse to return the nodes
// reachable via outgoing edges within max_depth. jailgraph's Collect does a
// depth-1 traverse from each Process node to gather its Syscall/File/Binary/
// Capability/Namespace neighbours (filtering by label client-side). This
// contract is the lower-priority guard against regressing to "returns nothing /
// drops neighbours" — it does not pin edge-type/direction filtering (jailgraph
// would adapt if those were added).
//
// A star graph: center → 3 leaves. A depth-1 traverse returns the start node
// (depth 0) plus all outgoing neighbours, so we assert all three leaves are
// present rather than an exact count.
func TestTraverse_OutgoingNeighborsAtDepth(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "default"

	center, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Process"}, nil)
	if err != nil {
		t.Fatalf("create center: %v", err)
	}

	leaves := make(map[uint64]bool)
	for _, kind := range []string{"Syscall", "File", "Binary"} {
		leaf, err := server.graph.CreateNodeWithTenant(tenantID, []string{kind}, nil)
		if err != nil {
			t.Fatalf("create %s leaf: %v", kind, err)
		}
		leaves[leaf.ID] = true
		if _, err := server.graph.CreateEdgeWithTenant(tenantID, center.ID, leaf.ID, "USES", nil, 1.0); err != nil {
			t.Fatalf("create edge center→%s: %v", kind, err)
		}
	}

	rr := httptest.NewRecorder()
	server.handleTraversal(rr, reqWithTenant(t, http.MethodPost, "/traverse", TraversalRequest{
		StartNodeID: center.ID,
		MaxDepth:    1,
	}, tenantID))
	if rr.Code != http.StatusOK {
		t.Fatalf("handleTraversal: want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp TraversalResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode traversal: %v", err)
	}

	returned := make(map[uint64]bool, len(resp.Nodes))
	for _, n := range resp.Nodes {
		returned[n.ID] = true
	}
	for leafID := range leaves {
		if !returned[leafID] {
			t.Errorf("outgoing neighbour %d missing from depth-1 traverse — /traverse dropped a neighbour", leafID)
		}
	}
}
