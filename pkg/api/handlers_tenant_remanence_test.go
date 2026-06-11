package api

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/auth"
	"github.com/dd0wney/graphdb/pkg/storage"
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

// TestDeleteTenant_PurgesWALRemanence pins security audit finding M-1
// (DR-1): after DELETE /api/v1/tenants/{id} returns 200, the deleted
// tenant's WAL records — its OpCreate* entries carrying full property
// data — must be gone from the on-disk WAL, not linger until the next
// snapshot+truncate at Close. The handler runs CompactWAL after the
// cascade.
//
// RED against pre-fix code: the create entry (base64 of the property
// bytes, since Value.Data marshals as base64) survives in wal.log.
func TestDeleteTenant_PurgesWALRemanence(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	server.tenantStore = tenant.NewTenantStore()
	if err := server.tenantStore.Create(&tenant.Tenant{ID: "t-rem", Name: "t-rem", Status: tenant.TenantStatusActive}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	const marker = "wal-remanence-PII-marker-9f3e6a1c7d250b48"
	if _, err := server.graph.CreateNodeWithTenant("t-rem", []string{"Doc"}, map[string]storage.Value{
		"secret": storage.StringValue(marker),
	}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	walMarker := base64.StdEncoding.EncodeToString([]byte(marker))
	walPath := filepath.Join(server.dataDir, "wal", "wal.log")
	if raw, err := os.ReadFile(walPath); err != nil || !strings.Contains(string(raw), walMarker) {
		t.Fatalf("test premise broken: marker not in WAL pre-delete (err=%v)", err)
	}

	adminTok := mintTestToken(t, server, auth.RoleAdmin, "root-admin", "")
	mux := http.NewServeMux()
	server.registerRoutes(mux)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t-rem", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("DELETE /api/v1/tenants/t-rem = %d, want 200 (body: %s)", rr.Code, rr.Body.String())
	}

	raw, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL post-delete: %v", err)
	}
	if strings.Contains(string(raw), walMarker) {
		t.Fatalf("deleted tenant's WAL records survive the delete (M-1 remanence)")
	}
}
