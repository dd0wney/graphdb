package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// /health/ready is what a load balancer or orchestrator reads. Its storage
// check used to call GetStatistics and return nil, so a server whose WAL a
// failed fsync had poisoned (every later write reaching memory and not the
// disk) still reported ready.
func TestHealthReady_PoisonedWALIsNotReady(t *testing.T) {
	server, faults, cleanup := setupTestServerWithFaultFS(t)
	defer cleanup()

	ready := func() int {
		rr := httptest.NewRecorder()
		server.healthChecker.ReadinessHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		return rr.Code
	}

	mustCreateNodeForWALTest(t, server, "before")
	if code := ready(); code != http.StatusOK {
		t.Fatalf("/health/ready before the fault = %d, want 200", code)
	}

	faults.FailSync(vfstest.Once)
	if _, err := server.graph.CreateNode([]string{"Person"}, nil); err == nil {
		t.Fatal("create under a sync fault succeeded; the fault did not reach the WAL")
	}
	if !faults.Fired() {
		t.Fatal("the sync fault never fired; this test proves nothing")
	}

	if code := ready(); code != http.StatusServiceUnavailable {
		t.Fatalf("/health/ready with a poisoned WAL = %d, want 503", code)
	}
}
