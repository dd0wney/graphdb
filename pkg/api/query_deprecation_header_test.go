package api

import (
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/query"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// A plain DELETE of a node with relationships still works in 1.x
// (docs/STABILITY_POLICY.md), but /query must tell the caller it is
// deprecated, the same way it tells a caller a traversal was truncated: a
// header that is absent when there is nothing to report.
func TestQuery_PlainDeleteOfConnectedNode_SetsDeprecationHeader(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"plain DELETE detaches", "MATCH (a:Del) DELETE a", query.NoticePlainDeleteDetach},
		{"DETACH DELETE", "MATCH (a:Del) DETACH DELETE a", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, cleanup := setupTestServer(t)
			defer cleanup()

			const tenantID = "owner"
			a, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Del"}, map[string]storage.Value{})
			if err != nil {
				t.Fatalf("create a: %v", err)
			}
			b, err := server.graph.CreateNodeWithTenant(tenantID, []string{"Other"}, map[string]storage.Value{})
			if err != nil {
				t.Fatalf("create b: %v", err)
			}
			if _, err := server.graph.CreateEdgeWithTenant(tenantID, a.ID, b.ID, "R", map[string]storage.Value{}, 1); err != nil {
				t.Fatalf("create edge: %v", err)
			}

			rr := httptest.NewRecorder()
			server.handleQuery(rr, queryReqWithTenant(t, tc.query, tenantID))
			if rr.Code != 200 {
				t.Fatalf("status %d, want 200: body=%s", rr.Code, rr.Body.String())
			}
			if got := rr.Header().Get(CypherDeprecationHeader); got != tc.want {
				t.Fatalf("%s = %q, want %q", CypherDeprecationHeader, got, tc.want)
			}
			if _, err := server.graph.GetNodeForTenant(a.ID, tenantID); err == nil {
				t.Fatal("the node was not deleted")
			}
		})
	}
}
