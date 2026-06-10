package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// TestDeleteTenant_FailsWhenLSASnapshotCleanupFails pins security audit
// finding M-2: if the on-disk LSA snapshot (which holds the deleted
// tenant's full indexed content) cannot be removed, the delete must fail
// so the operator retries — not return 200 leaving an orphaned file
// (a right-to-erasure / data-remanence gap).
//
// The failure is injected by placing a NON-EMPTY directory where the
// tenant's `.lsa` file would be: os.Remove on a non-empty directory
// errors (ENOTEMPTY), and that error is not os.IsNotExist, so
// DeleteLSASnapshot surfaces it.
//
// RED against pre-fix code: the error is logged and the handler returns 200.
func TestDeleteTenant_FailsWhenLSASnapshotCleanupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Remove non-empty-dir semantics differ on Windows")
	}
	server, cleanup := setupTestServer(t)
	defer cleanup()

	server.tenantStore = tenant.NewTenantStore()
	const id = "doomed"
	if err := server.tenantStore.Create(&tenant.Tenant{ID: id, Name: id, Status: tenant.TenantStatusActive}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Make DeleteLSASnapshot fail: its target path is a non-empty directory.
	lsaPath := filepath.Join(server.dataDir, "lsa", id+".lsa")
	if err := os.MkdirAll(lsaPath, 0o700); err != nil {
		t.Fatalf("mkdir lsa snapshot path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lsaPath, "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	adminTok := mintTestToken(t, server, auth.RoleAdmin, "root-admin", "")
	mux := http.NewServeMux()
	server.registerRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("DELETE with un-removable LSA snapshot = %d, want 500 (must not silently orphan the file)", rr.Code)
	}
}
