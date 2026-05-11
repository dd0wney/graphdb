package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateNode_ClaimUniquenessOnSingleLabelClaim pins H4.4: REST POST
// /nodes must enforce the same B-lite uniqueness on single-label :Claim
// nodes that the GraphQL `createNode` resolver enforces. Before the fix,
// a REST caller could create two :Claim nodes with the same `for_task`,
// silently bypassing the at-most-one-active-claim-per-task invariant
// that gate work-claim/coord rely on.
func TestCreateNode_ClaimUniquenessOnSingleLabelClaim(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	post := func(t *testing.T, body NodeRequest) *httptest.ResponseRecorder {
		t.Helper()
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(buf))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.handleNodes(rr, req)
		return rr
	}

	t.Run("two single-label :Claim with same for_task — second is 409", func(t *testing.T) {
		first := post(t, NodeRequest{
			Labels:     []string{"Claim"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-A"},
		})
		if first.Code != http.StatusCreated {
			t.Fatalf("first claim should succeed, got %d body=%s", first.Code, first.Body.String())
		}

		second := post(t, NodeRequest{
			Labels:     []string{"Claim"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-A"},
		})
		if second.Code != http.StatusConflict {
			t.Fatalf("duplicate claim should return 409, got %d body=%s", second.Code, second.Body.String())
		}
		// Error message should mention "unique constraint violation" so
		// REST callers can distinguish this from generic 409s and parse
		// the conflict's owning node id if they want to coordinate.
		if !strings.Contains(second.Body.String(), "unique constraint violation") {
			t.Errorf("409 body should mention unique constraint violation, got %s", second.Body.String())
		}
	})

	t.Run("two single-label :Claim with different for_task — both succeed", func(t *testing.T) {
		a := post(t, NodeRequest{
			Labels:     []string{"Claim"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-B"},
		})
		if a.Code != http.StatusCreated {
			t.Fatalf("first claim should succeed, got %d body=%s", a.Code, a.Body.String())
		}
		b := post(t, NodeRequest{
			Labels:     []string{"Claim"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-C"},
		})
		if b.Code != http.StatusCreated {
			t.Errorf("distinct-for_task claim should succeed, got %d body=%s", b.Code, b.Body.String())
		}
	})

	t.Run("single-label :Claim without for_task — 400", func(t *testing.T) {
		rr := post(t, NodeRequest{
			Labels:     []string{"Claim"},
			Properties: map[string]any{"comment": "missing for_task"},
		})
		if rr.Code != http.StatusBadRequest {
			t.Errorf("missing for_task should return 400, got %d body=%s", rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "for_task") {
			t.Errorf("400 body should name the missing property, got %s", rr.Body.String())
		}
	})

	t.Run("multi-label [Claim,Other] — uniqueness NOT applied, both succeed", func(t *testing.T) {
		// The single-label gate is intentional. Multi-label nodes
		// retain freedom to add secondary labels without inheriting
		// uniqueness semantics — matching the GraphQL resolver's
		// pkg/graphql/mutations_resolvers.go:78 behavior.
		a := post(t, NodeRequest{
			Labels:     []string{"Claim", "Tracer"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-multi"},
		})
		if a.Code != http.StatusCreated {
			t.Fatalf("first multi-label claim should succeed, got %d body=%s", a.Code, a.Body.String())
		}
		b := post(t, NodeRequest{
			Labels:     []string{"Claim", "Tracer"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-multi"},
		})
		if b.Code != http.StatusCreated {
			t.Errorf("multi-label nodes should NOT be uniqueness-gated, got %d body=%s", b.Code, b.Body.String())
		}
	})

	t.Run("non-Claim single label — no uniqueness check (regression)", func(t *testing.T) {
		// Spot-check: arbitrary single-label nodes with the same
		// for_task value as an existing :Claim should both succeed
		// because the label gate is on "Claim", not on the property.
		a := post(t, NodeRequest{
			Labels:     []string{"Task"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-D"},
		})
		if a.Code != http.StatusCreated {
			t.Fatalf("first :Task should succeed, got %d body=%s", a.Code, a.Body.String())
		}
		b := post(t, NodeRequest{
			Labels:     []string{"Task"},
			Properties: map[string]any{"for_task": "graphdb:H4.4-test-D"},
		})
		if b.Code != http.StatusCreated {
			t.Errorf(":Task should not be uniqueness-gated even with same for_task, got %d body=%s", b.Code, b.Body.String())
		}
	})
}
