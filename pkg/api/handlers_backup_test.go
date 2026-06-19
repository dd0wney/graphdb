package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// backupReq issues a request to handleBackup wrapped in requireAdmin. The
// caller's role is one of auth.RoleAdmin / auth.RoleViewer (plain strings).
// We use distinct literal usernames to avoid conflicts between sub-tests.
func backupReq(t *testing.T, server *Server, role, username, method string) *httptest.ResponseRecorder {
	t.Helper()
	user, err := server.userStore.CreateUser(username, "Password123!", role)
	if err != nil {
		t.Fatal(err)
	}
	token, err := server.jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, "/admin/backup", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.requireAdmin(server.handleBackup)(rr, req)
	return rr
}

// TestHandleBackup_AdminGetsGzip verifies that an admin POST returns 200 with
// a non-empty gzip body and the expected download headers.
func TestHandleBackup_AdminGetsGzip(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr := backupReq(t, server, "admin", "admin-backup-user", http.MethodPost)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("Content-Type = %q, want application/gzip", ct)
	}
	if cd := rr.Header().Get("Content-Disposition"); cd == "" {
		t.Error("missing Content-Disposition")
	}
	if rr.Body.Len() == 0 {
		t.Error("empty archive body")
	}
}

// TestHandleBackup_NonAdminForbidden verifies that a viewer (non-admin) POST
// is rejected with 403 before the handler body runs.
func TestHandleBackup_NonAdminForbidden(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr := backupReq(t, server, "viewer", "viewer-backup-user", http.MethodPost)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rr.Code)
	}
}

// TestHandleBackup_MethodNotAllowed verifies that a non-POST verb from an
// admin is rejected with 405.
func TestHandleBackup_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()
	rr := backupReq(t, server, "admin", "admin-backup-user2", http.MethodGet)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
