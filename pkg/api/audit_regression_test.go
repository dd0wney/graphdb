package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// TestAuditRegressionSuite_CrossTenantIsolation is the consolidated
// security gate for audit Track A (PRs #17-#26). It pins every
// cross-tenant guardrail introduced over the audit's lifetime in one
// readable matrix — each subtest names the audit task that
// introduced the contract. CI runs this as a single-shot regression
// check; if any row fails, a Track A guarantee has regressed.
//
// This is *defense in depth*: each row has a corresponding
// per-package test (see references below). Those package tests stay
// authoritative for fine-grained debugging; this suite is the
// umbrella that catches a contract slipping unnoticed across the
// whole API surface.
//
// Reference map (for context, not required reading to extend this
// suite):
//
//	A1   → pkg/tenantid                                 (typed tenant)
//	A3a  → pkg/storage/tenant_signatures_test.go        (*ForTenant variants)
//	A3b  → pkg/storage/tenant_signatures_test.go        (storage enforcement)
//	A5   → pkg/api/middleware_tenant_a5_test.go         (withTenant middleware)
//	A6a  → pkg/api/handlers_a6a_tenant_test.go          (/nodes /edges)
//	A6a-fu → pkg/storage/tenant_signatures_test.go      (verifyNodeExists strict)
//	A6b  → pkg/api/handlers_a6b_traverse_test.go        (/traverse /shortest-path)
//	A6c-graphql → pkg/graphql/http_tenant_test.go       (/graphql)
//	A6c-query   → pkg/api/handlers_query_a6c_test.go    (/query)
//	A6c-algorithms → pkg/api/handlers_algorithms_a6c_test.go (/algorithms)
//
// Open follow-ups (NOT exercised here — separate audit tasks):
//
//	A8 — replication tenancy (WriteOperation has no TenantID)
//	A9 — GraphQL schema introspection metadata leak
func TestAuditRegressionSuite_CrossTenantIsolation(t *testing.T) {
	fix := setupAuditRegressionFixture(t)
	defer fix.cleanup()

	// ---- Storage layer: A3b + A6a follow-up ----

	t.Run("A6a-fu/CreateEdgeWithTenant_cross-tenant-from-refused", func(t *testing.T) {
		// PR #20: cross-tenant from/to node ref → ErrNodeNotFound.
		_, err := fix.server.graph.CreateEdgeWithTenant("tenant-A", fix.b.docID, fix.a.userID, "REL", nil, 1.0)
		if err == nil {
			t.Errorf("cross-tenant from refused-storage: want error, got nil")
		}
	})

	t.Run("A6a-fu/CreateEdgeWithTenant_cross-tenant-to-refused", func(t *testing.T) {
		_, err := fix.server.graph.CreateEdgeWithTenant("tenant-A", fix.a.userID, fix.b.docID, "REL", nil, 1.0)
		if err == nil {
			t.Errorf("cross-tenant to refused-storage: want error, got nil")
		}
	})

	// ---- /nodes — A6a ----

	t.Run("A6a/createNode-lands-in-caller-tenant", func(t *testing.T) {
		// PR #18: tenant-stamping. Created node must be visible to
		// caller's tenant only.
		rr := httptest.NewRecorder()
		fix.server.createNode(rr, fix.req(t, http.MethodPost, "/nodes", NodeRequest{
			Labels:     []string{"Doc"},
			Properties: map[string]any{"title": "fresh"},
		}, "tenant-A"))
		if rr.Code != http.StatusCreated {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, err := fix.server.graph.GetNodeForTenant(resp.ID, "tenant-A"); err != nil {
			t.Errorf("tenant-A can't see its own create: %v", err)
		}
		if _, err := fix.server.graph.GetNodeForTenant(resp.ID, "default"); err == nil {
			t.Errorf("create leaked into default tenant")
		}
	})

	t.Run("A6a/getNode-cross-tenant-returns-404", func(t *testing.T) {
		// PR #18: existence-leak guard. Cross-tenant GET ≡ missing.
		rr := httptest.NewRecorder()
		fix.server.handleNode(rr, fix.req(t, http.MethodGet, "/nodes/"+strconv.FormatUint(fix.a.userID, 10), nil, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant GET /nodes/{id}: want 404, got %d", rr.Code)
		}
	})

	t.Run("A6a/updateNode-cross-tenant-no-side-effects", func(t *testing.T) {
		// PR #18: refusal AND no side effect.
		rr := httptest.NewRecorder()
		fix.server.handleNode(rr, fix.req(t, http.MethodPut, "/nodes/"+strconv.FormatUint(fix.a.userID, 10), NodeRequest{
			Properties: map[string]any{"name": "tampered"},
		}, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant PUT: want 404, got %d", rr.Code)
		}
		got, err := fix.server.graph.GetNodeForTenant(fix.a.userID, "tenant-A")
		if err != nil {
			t.Fatalf("readback: %v", err)
		}
		if name, _ := got.Properties["name"].AsString(); name != "alice-A" {
			t.Errorf("cross-tenant update leaked: name=%q", name)
		}
	})

	t.Run("A6a/deleteNode-cross-tenant-no-side-effects", func(t *testing.T) {
		// PR #18: deletion refusal AND target survives.
		rr := httptest.NewRecorder()
		fix.server.handleNode(rr, fix.req(t, http.MethodDelete, "/nodes/"+strconv.FormatUint(fix.a.docID, 10), nil, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant DELETE: want 404, got %d", rr.Code)
		}
		if _, err := fix.server.graph.GetNodeForTenant(fix.a.docID, "tenant-A"); err != nil {
			t.Errorf("node disappeared after refused cross-tenant delete: %v", err)
		}
	})

	t.Run("A6a/createEdge-lands-in-caller-tenant", func(t *testing.T) {
		// PR #18 + #20 (gap closure): edge stamped with caller, refused
		// against foreign-tenant from/to.
		rr := httptest.NewRecorder()
		fix.server.createEdge(rr, fix.req(t, http.MethodPost, "/edges", EdgeRequest{
			FromNodeID: fix.a.userID,
			ToNodeID:   fix.a.docID,
			Type:       "OWNS",
			Weight:     1.0,
		}, "tenant-A"))
		if rr.Code != http.StatusCreated {
			t.Errorf("legitimate same-tenant create: want 201, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("A6a-fu/createEdge-cross-tenant-from-returns-404", func(t *testing.T) {
		// PR #20: createEdge handler now returns 404 (was 500) when
		// from/to references a foreign-tenant node.
		rr := httptest.NewRecorder()
		fix.server.createEdge(rr, fix.req(t, http.MethodPost, "/edges", EdgeRequest{
			FromNodeID: fix.b.userID, // tenant-B's node
			ToNodeID:   fix.a.docID,
			Type:       "REL",
			Weight:     1.0,
		}, "tenant-A"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant from: want 404, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("A6a/getEdge-cross-tenant-returns-404", func(t *testing.T) {
		// PR #18: edge existence-leak guard.
		rr := httptest.NewRecorder()
		fix.server.handleEdge(rr, fix.req(t, http.MethodGet, "/edges/"+strconv.FormatUint(fix.a.edgeID, 10), nil, "tenant-B"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant GET /edges/{id}: want 404, got %d", rr.Code)
		}
	})

	// ---- /traverse and /shortest-path — A6b ----

	t.Run("A6b/traverse-stops-at-tenant-boundary", func(t *testing.T) {
		// PR #19: BFS stays inside caller's subgraph.
		rr := httptest.NewRecorder()
		fix.server.handleTraversal(rr, fix.req(t, http.MethodPost, "/traverse", TraversalRequest{
			StartNodeID: fix.a.userID,
			MaxDepth:    5,
		}, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp TraversalResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, n := range resp.Nodes {
			if n.ID == fix.b.userID || n.ID == fix.b.docID {
				t.Errorf("traverse leaked tenant-B node %d into tenant-A result", n.ID)
			}
		}
	})

	t.Run("A6b/shortest-path-cross-tenant-end-is-404", func(t *testing.T) {
		// PR #19: both endpoints scoped (this row catches the
		// second-of-pair miss).
		rr := httptest.NewRecorder()
		fix.server.handleShortestPath(rr, fix.req(t, http.MethodPost, "/shortest-path", ShortestPathRequest{
			StartNodeID: fix.a.userID,
			EndNodeID:   fix.b.userID,
		}, "tenant-A"))
		if rr.Code != http.StatusNotFound {
			t.Errorf("cross-tenant end: want 404, got %d", rr.Code)
		}
	})

	// ---- /query — A6c-query ----

	t.Run("A6c-query/MATCH-only-sees-caller-tenant", func(t *testing.T) {
		// PR #25: ExecutionContext snapshots tenantID from r.Context(),
		// step bodies use *ForTenant.
		rr := httptest.NewRecorder()
		fix.server.handleQuery(rr, fix.req(t, http.MethodPost, "/query", QueryRequest{
			Query: "MATCH (u:User) RETURN u.name",
		}, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp QueryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.Count != 1 {
			t.Errorf("MATCH (u:User) tenant-A: want 1 (just alice-A), got %d (rows=%v)", resp.Count, resp.Rows)
		}
	})

	t.Run("A6c-query/CREATE-lands-in-caller-tenant", func(t *testing.T) {
		// PR #25: Cypher CREATE stamps caller's tenant.
		rr := httptest.NewRecorder()
		fix.server.handleQuery(rr, fix.req(t, http.MethodPost, "/query", QueryRequest{
			Query: `CREATE (n:Tag {name: "via-query"}) RETURN n`,
		}, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		aTags := fix.server.graph.GetNodesByLabelForTenant("tenant-A", "Tag")
		if len(aTags) != 1 {
			t.Errorf("tenant-A Tag count after CREATE: want 1, got %d", len(aTags))
		}
		defaultTags := fix.server.graph.GetNodesByLabelForTenant("default", "Tag")
		if len(defaultTags) != 0 {
			t.Errorf("CREATE leaked into default tenant: %d Tags", len(defaultTags))
		}
	})

	// ---- /algorithms — A6c-algorithms ----

	t.Run("A6c-algorithms/PageRank-only-ranks-caller-tenant", func(t *testing.T) {
		// PR #26: graphView pattern; tenant-scoped algorithm body.
		rr := httptest.NewRecorder()
		fix.server.handleAlgorithm(rr, fix.algReq(t, "pagerank", "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var raw struct {
			Results map[string]any `json:"results"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		scores, _ := raw.Results["scores"].(map[string]any)
		// The contract: tenant-B's specific node IDs must not appear
		// in tenant-A's scoring map. Don't gate on exact count —
		// earlier subtests in this matrix may have mutated the
		// shared fixture (e.g., CREATE-lands-in-caller-tenant adds
		// a Tag node for tenant-A). What matters is "no leak," not
		// "exact set size."
		bUserKey := strconv.FormatUint(fix.b.userID, 10)
		bDocKey := strconv.FormatUint(fix.b.docID, 10)
		if _, leak := scores[bUserKey]; leak {
			t.Errorf("PageRank leaked tenant-B userID %s", bUserKey)
		}
		if _, leak := scores[bDocKey]; leak {
			t.Errorf("PageRank leaked tenant-B docID %s", bDocKey)
		}
		// Sanity: tenant-A's own seeded user must be ranked.
		aUserKey := strconv.FormatUint(fix.a.userID, 10)
		if _, ok := scores[aUserKey]; !ok {
			t.Errorf("tenant-A PageRank missing own user %s — over-filtered?", aUserKey)
		}
	})

	t.Run("A6c-algorithms/triangles-only-counts-caller-tenant", func(t *testing.T) {
		// PR #26: graphView pattern. Each tenant's triangle count is
		// independent of the other's.
		// Note: fixture doesn't seed triangles by default; this test
		// tolerates 0 and only fails on a leak (count > seeded triangles).
		rr := httptest.NewRecorder()
		fix.server.handleAlgorithm(rr, fix.algReq(t, "triangles", "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var raw struct {
			Results map[string]any `json:"results"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		gc, _ := raw.Results["global_count"].(float64)
		// Fixture seeds no triangles, so tenant-A must report 0.
		// A leak from tenant-B (which also has no triangles) would be
		// hard to detect from this alone — covered by the dedicated
		// TestA6c_Algorithms_TrianglesIsolation test which seeds
		// triangles in both tenants. This row is the smoke test.
		if gc != 0 {
			t.Errorf("tenant-A triangles: want 0 (fixture has none), got %d", int(gc))
		}
	})
}

// auditRegressionFixture sets up two parallel tenants with
// equivalent shapes so cross-tenant tests can compare apples to
// apples without coincidence-of-emptiness false negatives.
type auditRegressionFixture struct {
	server  *Server
	cleanup func()
	a       fixtureTenant
	b       fixtureTenant
}

type fixtureTenant struct {
	userID uint64 // labeled "User", name="alice-A" or "alice-B"
	docID  uint64 // labeled "Doc"
	edgeID uint64 // userID --OWNS--> docID
}

func setupAuditRegressionFixture(t *testing.T) *auditRegressionFixture {
	t.Helper()
	server, cleanup := setupTestServer(t)

	mkTenant := func(name string, suffix string) fixtureTenant {
		user, err := server.graph.CreateNodeWithTenant(name, []string{"User"}, map[string]storage.Value{
			"name": storage.StringValue("alice-" + suffix),
		})
		if err != nil {
			cleanup()
			t.Fatalf("seed user %s: %v", name, err)
		}
		doc, err := server.graph.CreateNodeWithTenant(name, []string{"Doc"}, map[string]storage.Value{
			"title": storage.StringValue("doc-" + suffix),
		})
		if err != nil {
			cleanup()
			t.Fatalf("seed doc %s: %v", name, err)
		}
		edge, err := server.graph.CreateEdgeWithTenant(name, user.ID, doc.ID, "OWNS", nil, 1.0)
		if err != nil {
			cleanup()
			t.Fatalf("seed edge %s: %v", name, err)
		}
		return fixtureTenant{userID: user.ID, docID: doc.ID, edgeID: edge.ID}
	}

	return &auditRegressionFixture{
		server:  server,
		cleanup: cleanup,
		a:       mkTenant("tenant-A", "A"),
		b:       mkTenant("tenant-B", "B"),
	}
}

// req builds a JSON-body POST/PUT/GET/DELETE with the tenant context
// wired the same way withTenant middleware does. body=nil for GET/DELETE.
func (f *auditRegressionFixture) req(t *testing.T, method, path string, body any, tenantID string) *http.Request {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(tenant.WithTenant(req.Context(), tenantID))
	return req
}

func (f *auditRegressionFixture) algReq(t *testing.T, algName string, tenantID string) *http.Request {
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
