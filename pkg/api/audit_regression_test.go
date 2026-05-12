package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/auth"
	"github.com/dd0wney/cluso-graphdb/pkg/masking"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
	"github.com/dd0wney/cluso-graphdb/pkg/wal/apply"
)

// TestAuditRegressionSuite_CrossTenantIsolation is the consolidated
// security gate for audit Track A (PRs #17-#26), F2 retrieval
// (PR #31), A9 GraphQL introspection (PR #36-#39), A8 replication
// tenancy (PRs #40+), and F3 Compliance API (PRs #111/#114/#122).
// It pins every cross-tenant guardrail introduced
// across audit + feature work in one readable matrix — each subtest
// names the task that introduced the contract. CI runs this as a
// single-shot regression check; if any row fails, a guarantee has
// regressed.
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
//	A6c-graphql → pkg/graphql/http_tenant_test.go       (/graphql resolver scope)
//	A6c-query   → pkg/api/handlers_query_a6c_test.go    (/query)
//	A6c-algorithms → pkg/api/handlers_algorithms_a6c_test.go (/algorithms)
//	A8   → pkg/wal/apply/apply_test.go                  (apply path fail-closed tenant gate; lifted 2026-05-12 by A8.1 Option B from pkg/replication)
//	A8.2 → (closed by A8.1 deletion — replica binary no longer exists)
//	A9   → pkg/api/handlers_graphql_introspection_a9_test.go (/graphql introspection)
//	F2   → pkg/api/handlers_retrieve_test.go            (/v1/retrieve)
//	F3   → pkg/api/handlers_compliance_test.go          (/v1/compliance/audit-log)
//	F3   → pkg/api/handlers_nodes_masking_test.go       (read-path masking, REST)
//	F3   → pkg/api/handlers_graphql_masking_test.go     (read-path masking, GraphQL)
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

	// ---- F2 graph-augmented retrieval — PR #31 ----

	t.Run("F2/retrieve-only-returns-caller-tenant", func(t *testing.T) {
		// PR #31: /v1/retrieve composes hybrid search + tenant-scoped
		// BFS expansion. Both tenants have a User node named
		// "alice-{A,B}" and a Doc node titled "doc-{A,B}". A query
		// for "alice" matches both tenants at the FTS level — but
		// each caller must see only its own results.
		//
		// This row is the umbrella gate. The dedicated
		// TestRetrieveHTTP_TenantIsolation in handlers_retrieve_test.go
		// stays authoritative for fine-grained debugging.
		rr := httptest.NewRecorder()
		fix.server.handleRetrieve(rr, fix.req(t, http.MethodPost, "/v1/retrieve", RetrieveRequest{
			Query:   "alice",
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
		// Tenant-B's IDs must not appear in tenant-A's result.
		for _, doc := range resp.Documents {
			if doc.Metadata.NodeID == fix.b.userID || doc.Metadata.NodeID == fix.b.docID {
				t.Errorf("retrieve leaked tenant-B node %d into tenant-A result", doc.Metadata.NodeID)
			}
			// Source.Path is the load-bearing graph signal — pin it
			// here too. Empty path or path not ending at NodeID =
			// the F2 spike §2 Q6 contract regressed.
			if len(doc.Metadata.Source.Path) == 0 {
				t.Errorf("doc %d: empty Source.Path (graph signal missing)", doc.Metadata.NodeID)
				continue
			}
			if last := doc.Metadata.Source.Path[len(doc.Metadata.Source.Path)-1]; last != doc.Metadata.NodeID {
				t.Errorf("doc %d: Source.Path must end at NodeID, got %v", doc.Metadata.NodeID, doc.Metadata.Source.Path)
			}
		}
		// Sanity: at least one doc returned (proves the query worked).
		if len(resp.Documents) == 0 {
			t.Errorf("tenant-A: expected ≥1 document for query 'alice', got 0 — fixture or search broken")
		}
	})

	// ---- /graphql introspection — A9 #4 ----

	t.Run("A9/graphql-introspection-cant-see-other-tenant-labels", func(t *testing.T) {
		// PR #36-#39: per-tenant GraphQL schema (built lazily from
		// gs.GetLabelsForTenant) closes the introspection metadata
		// leak. Pre-fix, GenerateSchema(gs) used GetAllLabels() and a
		// single shared schema, so any /graphql caller could
		// enumerate every tenant's label set via __schema.
		//
		// The fixture's shared "User" / "Doc" labels appear in both
		// tenants' schemas — that's correct, those aren't a leak.
		// Seed an *exclusive* label per tenant inside this subtest so
		// the assertion is unambiguous: tenant-A's introspection must
		// reveal "ASecret" but never "BSecret".
		if _, err := fix.server.graph.CreateNodeWithTenant("tenant-A", []string{"ASecret"}, nil); err != nil {
			t.Fatalf("seed A: %v", err)
		}
		if _, err := fix.server.graph.CreateNodeWithTenant("tenant-B", []string{"BSecret"}, nil); err != nil {
			t.Fatalf("seed B: %v", err)
		}

		rr := httptest.NewRecorder()
		fix.server.handleGraphQL(rr, fix.req(t, http.MethodPost, "/graphql", map[string]string{
			"query": `{ __schema { types { name } } }`,
		}, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		typeNames := extractIntrospectedTypeNames(t, rr.Body.Bytes())
		if !containsString(typeNames, "ASecret") {
			t.Errorf("tenant-A introspection missing own type 'ASecret' (got: %v)", typeNames)
		}
		if containsString(typeNames, "BSecret") {
			t.Errorf("tenant-A introspection LEAKED tenant-B type 'BSecret' (A9 regression; got: %v)", typeNames)
		}
	})

	// ---- F3 compliance API — PRs #111 (audit-log), #114 (masking REST), #122 (masking GraphQL) ----

	t.Run("F3/audit-log-only-returns-caller-tenant-events", func(t *testing.T) {
		// PR #111: GET /v1/compliance/audit-log defaults to the
		// caller's tenant (withTenant resolution). Non-admin callers
		// cannot widen with ?tenant=* (handler-internal admin gate).
		//
		// Per-package authoritative tests:
		//   pkg/api/handlers_compliance_test.go::TestComplianceAuditLog_TenantIsolation
		//   pkg/api/handlers_compliance_test.go::TestComplianceAuditLog_NonAdminCannotWidenCrossTenant
		//
		// This row pins the umbrella contract: seed events for both
		// tenants directly into the in-memory logger, call the
		// handler as tenant-A (no admin claims), assert the result
		// contains only tenant-A's event.
		if err := fix.server.inMemoryAuditLogger.Log(&audit.Event{
			TenantID: "tenant-A", UserID: "u-a", Username: "alice",
			Action: audit.ActionRead, ResourceType: audit.ResourceNode, Status: audit.StatusSuccess,
		}); err != nil {
			t.Fatalf("seed tenant-A audit event: %v", err)
		}
		if err := fix.server.inMemoryAuditLogger.Log(&audit.Event{
			TenantID: "tenant-B", UserID: "u-b", Username: "bob",
			Action: audit.ActionRead, ResourceType: audit.ResourceNode, Status: audit.StatusSuccess,
		}); err != nil {
			t.Fatalf("seed tenant-B audit event: %v", err)
		}

		rr := httptest.NewRecorder()
		fix.server.handleComplianceAuditLog(rr, fix.req(t, http.MethodGet, "/v1/compliance/audit-log", nil, "tenant-A"))
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["tenant"] != "tenant-A" {
			t.Errorf("tenant: want tenant-A, got %v", resp["tenant"])
		}
		events, _ := resp["events"].([]any)
		for i, ev := range events {
			m, _ := ev.(map[string]any)
			if m["tenant_id"] != "tenant-A" {
				t.Errorf("event[%d] tenant_id: want tenant-A, got %v (audit-log leaked across tenants)", i, m["tenant_id"])
			}
		}
	})

	t.Run("F3/masking-policy-GET-cross-tenant-non-admin-403", func(t *testing.T) {
		// PR #114: GET /v1/compliance/masking-policy/{tenant} for a
		// non-admin caller targeting a different tenant returns 403.
		// Admin or self-tenant return 200; cross-tenant non-admin is
		// the rejected case.
		//
		// Per-package authoritative test:
		//   pkg/api/handlers_nodes_masking_test.go::TestMasking_PolicyFollowsTenant
		fix.server.maskingPolicyStore.Set("tenant-B", &masking.Policy{
			Properties: map[string]masking.MaskingStrategy{"email": masking.StrategyRedact},
		})

		req := fix.req(t, http.MethodGet, "/v1/compliance/masking-policy/tenant-B", nil, "tenant-A")
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, &auth.Claims{
			UserID: "u-a", Username: "alice", Role: auth.RoleViewer,
		}))
		rr := httptest.NewRecorder()
		fix.server.handleComplianceMaskingPolicy(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("non-admin tenant-A reading tenant-B's policy: want 403, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("F3/masking-policy-POST-non-admin-403", func(t *testing.T) {
		// PR #114: POST /v1/compliance/masking-policy is admin-only.
		// A non-admin caller (regardless of target tenant) receives
		// 403 "Admin role required" before any tenant-resolution
		// runs. This row pins that the admin gate fires before the
		// X-Tenant-ID override path could be abused.
		body := maskingPolicyRequest{
			Properties: map[string]masking.MaskingStrategy{"ssn": masking.StrategyFull},
		}
		req := fix.req(t, http.MethodPost, "/v1/compliance/masking-policy", body, "tenant-A")
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, &auth.Claims{
			UserID: "u-a", Username: "alice", Role: auth.RoleViewer,
		}))
		rr := httptest.NewRecorder()
		fix.server.handleComplianceMaskingPolicy(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("non-admin POST masking-policy: want 403, got %d body=%s", rr.Code, rr.Body.String())
		}
	})

	// ---- apply path fail-closed tenant gate — A8 ----

	t.Run("A8/apply-write-preserves-tenant", func(t *testing.T) {
		// PR #40+ (A8 spike), lifted 2026-05-12 by A8.1 Option B:
		// a WriteOperation flowing through the apply path lands only
		// in the originating tenant, and never silently routes to
		// "default". The per-package authoritative test is
		// pkg/wal/apply/apply_test.go (fail-closed semantics on
		// empty TenantID + tenant-flow-through with a mock
		// executor); this row is the umbrella that pins the
		// observable end-to-end contract against a real
		// *storage.GraphStorage.
		//
		// Unique label "A8WireSentinel" — separate from the
		// fixture's shared "User"/"Doc" labels — so the assertion
		// is exact (1 in tenant-A, 0 in default).
		op := apply.WriteOperation{
			Type:     "create_node",
			TenantID: "tenant-A",
			Labels:   []string{"A8WireSentinel"},
		}
		// Capture the error: a future row extension (e.g.,
		// create_edge with stale node IDs) would surface the actual
		// failure here rather than a confusing "got 0" further down.
		if err := apply.ApplyWriteOperation(applyAdapter{gs: fix.server.graph}, op); err != nil {
			t.Fatalf("ApplyWriteOperation: %v", err)
		}

		inA := fix.server.graph.GetNodesByLabelForTenant("tenant-A", "A8WireSentinel")
		if len(inA) != 1 {
			t.Errorf("tenant-A A8WireSentinel: want 1, got %d", len(inA))
		}
		inDefault := fix.server.graph.GetNodesByLabelForTenant("default", "A8WireSentinel")
		if len(inDefault) != 0 {
			t.Errorf("apply leaked into default tenant: %d node(s) with A8WireSentinel", len(inDefault))
		}
	})
}

// applyAdapter wraps *storage.GraphStorage to satisfy the
// apply.WriteExecutor interface for the A8 audit row.
//
// Test-scoped on purpose. Properties are passed nil because the audit
// row uses no properties; a real adapter would need a
// map[string]interface{} → map[string]storage.Value conversion.
type applyAdapter struct {
	gs storage.Storage
}

func (a applyAdapter) CreateNodeWithTenant(tenantID string, labels []string, _ map[string]interface{}) (uint64, error) {
	n, err := a.gs.CreateNodeWithTenant(tenantID, labels, nil)
	if err != nil {
		return 0, err
	}
	return n.ID, nil
}

func (a applyAdapter) CreateEdgeWithTenant(tenantID string, from, to uint64, edgeType string, _ map[string]interface{}, weight float64) (uint64, error) {
	e, err := a.gs.CreateEdgeWithTenant(tenantID, from, to, edgeType, nil, weight)
	if err != nil {
		return 0, err
	}
	return e.ID, nil
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

	fix := &auditRegressionFixture{
		server:  server,
		cleanup: cleanup,
		a:       mkTenant("tenant-A", "A"),
		b:       mkTenant("tenant-B", "B"),
	}

	// Audit F2 #5: build per-tenant FTS indexes so the
	// /v1/retrieve subtest below has searchable seeds. Purely
	// additive — doesn't affect the storage / handler / query /
	// algorithms subtests above (they don't touch the search index).
	if err := server.searchIndexes.IndexForTenant("tenant-A", []string{"User", "Doc"}, []string{"name", "title"}); err != nil {
		cleanup()
		t.Fatalf("FTS index tenant-A: %v", err)
	}
	if err := server.searchIndexes.IndexForTenant("tenant-B", []string{"User", "Doc"}, []string{"name", "title"}); err != nil {
		cleanup()
		t.Fatalf("FTS index tenant-B: %v", err)
	}

	return fix
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
