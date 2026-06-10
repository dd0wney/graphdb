package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/auth"
)

const testSecret = "test-only-secret-at-least-32-chars-long!!"

// #226: a minted token must validate under the same secret and carry the
// requested identity/role/tenant.
func TestMintToken_RoundTrips(t *testing.T) {
	token, err := mintToken(testSecret, "u-1", "alice", auth.RoleEditor, "tenant-x", time.Hour)
	if err != nil {
		t.Fatalf("mintToken: %v", err)
	}

	mgr, err := auth.NewJWTManager(testSecret, time.Hour, auth.DefaultRefreshTokenDuration)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	claims, err := mgr.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateToken rejected a freshly minted token: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("username = %q, want alice", claims.Username)
	}
	if claims.Role != auth.RoleEditor {
		t.Errorf("role = %q, want editor", claims.Role)
	}
	if claims.TenantID != "tenant-x" {
		t.Errorf("tenant = %q, want tenant-x", claims.TenantID)
	}
}

// TestMintTokenCommand_DefaultsToLeastPrivilege pins security audit
// finding M-6: invoking mint-token without --role must NOT produce an
// admin token. Before the fix the --role default was auth.RoleAdmin, so
// any runbook copying the example minted admin credentials silently.
//
// RED against pre-fix code: the minted token decodes with role "admin".
func TestMintTokenCommand_DefaultsToLeastPrivilege(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)

	// Capture stdout (the command prints the bare token there).
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	handleMintTokenCommand([]string{"--username", "svc-account"}) // no --role
	_ = w.Close()
	os.Stdout = old

	out, _ := io.ReadAll(r)
	token := strings.TrimSpace(string(out))
	if token == "" {
		t.Fatal("mint-token produced no token on stdout")
	}

	mgr, err := auth.NewJWTManager(testSecret, time.Hour, auth.DefaultRefreshTokenDuration)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}
	claims, err := mgr.ValidateToken(context.Background(), token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if claims.Role == auth.RoleAdmin {
		t.Error("mint-token without --role minted an ADMIN token (M-6)")
	}
	if claims.Role != auth.RoleViewer {
		t.Errorf("default minted role = %q, want %q", claims.Role, auth.RoleViewer)
	}
}

func TestMintToken_Errors(t *testing.T) {
	cases := []struct {
		name                           string
		secret, id, user, role, tenant string
	}{
		{"empty secret", "", "u", "u", auth.RoleAdmin, ""},
		{"short secret", "too-short", "u", "u", auth.RoleAdmin, ""},
		{"invalid role", testSecret, "u", "u", "superuser", ""},
		{"empty username", testSecret, "u", "", auth.RoleAdmin, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := mintToken(tc.secret, tc.id, tc.user, tc.role, tc.tenant, time.Hour); err == nil {
				t.Errorf("mintToken(%s) = nil error, want failure", tc.name)
			}
		})
	}
}

// #226: login posts to /auth/login and extracts the access token.
func TestLogin_ExtractsToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" || r.Method != http.MethodPost {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "admin" || body["password"] != "pw" {
			http.Error(w, "bad creds", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token":  "ACCESS-123",
			"refresh_token": "REFRESH-456",
		})
	}))
	defer srv.Close()

	access, refresh, err := login(srv.URL, "admin", "pw")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if access != "ACCESS-123" {
		t.Errorf("access token = %q, want ACCESS-123", access)
	}
	if refresh != "REFRESH-456" {
		t.Errorf("refresh token = %q, want REFRESH-456", refresh)
	}
}

func TestLogin_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, _, err := login(srv.URL, "admin", "wrong"); err == nil {
		t.Error("login with bad credentials = nil error, want failure")
	}
}
