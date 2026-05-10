//go:build nng
// +build nng

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/replication"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestBuildHTTPHandler_A82_NoNodesRoute pins audit A8.2 for the nng
// replica variant. The previous registration was a stub returning
// []any{} so the security exposure was zero, but the symmetric route
// was removed for consistency with cmd/graphdb-replica (where the same
// path leaked graph.GetAllNodesAcrossTenants() unauthenticated). This
// test fails if anyone re-adds /nodes without going through cmd/server's
// middleware (see docs/A8_REPLICATION_TENANCY_DESIGN.md §1.3).
func TestBuildHTTPHandler_A82_NoNodesRoute(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer graph.Close()

	replica, err := replication.NewNNGReplicaNode(replication.DefaultReplicationConfig(), graph)
	if err != nil {
		t.Fatalf("NewNNGReplicaNode: %v", err)
	}
	mux := buildHTTPHandler(replica, graph)

	cases := []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/nodes", http.StatusNotFound},
		{http.MethodPost, "/nodes", http.StatusNotFound},
		{http.MethodGet, "/nodes/", http.StatusNotFound},
		{http.MethodGet, "/nodes/123", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.method+"_"+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			mux.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Errorf("%s %s: got %d, want %d (A8.2: /nodes must not be registered on replica)",
					tc.method, tc.path, rr.Code, tc.want)
			}
		})
	}
}

func TestBuildHTTPHandler_HealthAndStatsStillRegistered(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer graph.Close()

	replica, err := replication.NewNNGReplicaNode(replication.DefaultReplicationConfig(), graph)
	if err != nil {
		t.Fatalf("NewNNGReplicaNode: %v", err)
	}
	mux := buildHTTPHandler(replica, graph)

	for _, path := range []string{"/health", "/stats", "/replication/status"} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			mux.ServeHTTP(rr, req)
			if rr.Code == http.StatusNotFound {
				t.Errorf("%s: got 404, want a registered route (regression sanity check for the buildHTTPHandler refactor)", path)
			}
		})
	}
}
