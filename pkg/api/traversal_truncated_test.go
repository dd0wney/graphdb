package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/query"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// A traversal that stops at the engine's depth cap answers 500 and throws
// away the rows it already found.
//
// pkg/query already returns the rows BESIDE a non-nil error that wraps
// query.ErrTraversalTruncated (see pkg/query/traversal_types.go and
// pkg/query/match_path.go) — a nil error is the positive assertion that the
// answer is complete. Before this fix, handleQuery treated every non-nil
// error alike: it answered 500 and discarded results, so the caller lost the
// answer AND the reason.
//
// The fix answers 200 with the rows that were found, and sets
// X-Traversal-Truncated: true so the caller can tell a capped answer from a
// complete one. It does not reuse X-Enumeration-Incomplete (CC14): that
// header's value is a COUNT of skipped records, and a depth-capped traversal
// has no such count to report.
//
// CONSUMER CONTRACT: CC15-truncated-query-serves-partial-result — all REST consumers (#561)
func TestTruncatedTraversalServesPartialResult(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "owner"
	// Deeper than query.MaxAllowedTraversalDepth so the bare-star pattern
	// below reaches the engine's cap with unexplored neighbours remaining.
	buildChain(t, server, tenantID, query.MaxAllowedTraversalDepth+2)

	rr := httptest.NewRecorder()
	server.handleQuery(rr, queryReqWithTenant(t,
		"MATCH (a:Chain0)-[:NEXT*]->(b) RETURN b", tenantID))

	if rr.Code != 200 {
		t.Fatalf("status %d, want 200: a traversal that stopped at an engine limit "+
			"still found real rows, and a 500 discards them. body=%s", rr.Code, rr.Body.String())
	}

	var resp QueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body %s: %v", rr.Body.String(), err)
	}
	if len(resp.Rows) == 0 {
		t.Fatalf("no rows came back, so this test cannot distinguish a truncation "+
			"signal from a broken query: body=%s", rr.Body.String())
	}

	if h := rr.Header().Get(TraversalTruncatedHeader); h != "true" {
		t.Errorf("%s = %q, want \"true\": the caller must be able to tell this answer "+
			"is incomplete", TraversalTruncatedHeader, h)
	}
}

// TestCompleteTraversalOmitsTheHeader is control 1: a header that is always
// present carries no information. Without this test, an implementation that
// set X-Traversal-Truncated on every response would pass the row above while
// telling the caller nothing.
// CONSUMER CONTRACT: CC15-truncated-query-serves-partial-result — all REST consumers (#561)
func TestCompleteTraversalOmitsTheHeader(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "owner"
	// Well inside the cap: the traversal runs out of graph, not budget.
	buildChain(t, server, tenantID, 5)

	rr := httptest.NewRecorder()
	server.handleQuery(rr, queryReqWithTenant(t,
		"MATCH (a:Chain0)-[:NEXT*]->(b) RETURN b", tenantID))

	if rr.Code != 200 {
		t.Fatalf("status %d, want 200. body=%s", rr.Code, rr.Body.String())
	}
	var resp QueryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body %s: %v", rr.Body.String(), err)
	}
	if len(resp.Rows) != 5 {
		t.Fatalf("want 5 reachable nodes, got %d (rows=%v): the fixture must be "+
			"genuinely complete for this control to mean anything", len(resp.Rows), resp.Rows)
	}
	if h := rr.Header().Get(TraversalTruncatedHeader); h != "" {
		t.Errorf("%s = %q on a complete traversal, want absent: a header that is "+
			"always present is not a signal", TraversalTruncatedHeader, h)
	}
}

// TestTraversalRealFailureStillFails is control 2: an error that is NOT
// query.ErrTraversalTruncated must still answer 500. Without this test, a fix
// that turned every query error into a 200 would pass the row above too.
//
// An explicit depth request above the engine cap is refused with
// query.ErrInvalidTraversalDepth, and pkg/query returns nil results alongside
// it (executePlanWithContext returns (nil, err) when a step itself fails,
// as opposed to noting truncation on results it still built) — a real
// failure, not a partial answer.
func TestTraversalRealFailureStillFails(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	const tenantID = "owner"
	buildChain(t, server, tenantID, 5)

	over := query.MaxAllowedTraversalDepth + 50
	rr := httptest.NewRecorder()
	server.handleQuery(rr, queryReqWithTenant(t,
		fmt.Sprintf("MATCH (a:Chain0)-[:NEXT*1..%d]->(b) RETURN b", over), tenantID))

	if rr.Code != 500 {
		t.Fatalf("status %d, want 500: this is a genuine failure (an explicit depth "+
			"above the engine cap), not a truncated answer. body=%s", rr.Code, rr.Body.String())
	}
	if h := rr.Header().Get(TraversalTruncatedHeader); h != "" {
		t.Errorf("%s = %q on a real failure, want absent", TraversalTruncatedHeader, h)
	}
}

// buildChain builds a0(:Chain0) -[:NEXT]-> n1 -> ... -> n(depth), all in
// tenantID, mirroring pkg/query's chainGraph helper (traversal_limit_honesty_test.go)
// for the equivalent REST-layer fixture. It is not exported from pkg/query
// (a different package), so it is rebuilt here.
func buildChain(t *testing.T, s *Server, tenantID string, depth int) {
	t.Helper()

	prev, err := s.graph.CreateNodeWithTenant(tenantID, []string{"Chain0"}, map[string]storage.Value{
		"name": storage.StringValue("n0"),
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	for i := 1; i <= depth; i++ {
		n, err := s.graph.CreateNodeWithTenant(tenantID, []string{"Node"}, map[string]storage.Value{
			"name": storage.StringValue(fmt.Sprintf("n%d", i)),
		})
		if err != nil {
			t.Fatalf("create n%d: %v", i, err)
		}
		if _, err := s.graph.CreateEdgeWithTenant(tenantID, prev.ID, n.ID, "NEXT", nil, 1); err != nil {
			t.Fatalf("create edge %d: %v", i, err)
		}
		prev = n
	}
}
