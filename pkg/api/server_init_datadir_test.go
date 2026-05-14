package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestNewServerWithDataDir_PersistsAuthUnderDataDir pins the contract
// the 2026-05-14 fix restored: auth artifacts (users.json, apikeys.json,
// .graphdb_admin_password) must land under the daemon's --data path,
// not at CWD. Before the fix, cmd/server/main.go called the
// dataDir-less NewServer which defaulted to "./data/server", silently
// splitting auth state from graph state and invalidating every
// previously-issued X-API-Key on every restart.
//
// This test guards two contracts:
//  1. NewServerWithDataDir honours its dataDir argument — auth files
//     appear under <dataDir>/auth and nowhere else.
//  2. Auth state round-trips via the dataDir: a key minted by one
//     server instance validates against a fresh server instance
//     pointed at the same dataDir.
func TestNewServerWithDataDir_PersistsAuthUnderDataDir(t *testing.T) {
	tmp := t.TempDir()
	graphPath := filepath.Join(tmp, "graph")
	dataDir := filepath.Join(tmp, "datadir")

	graph, err := storage.NewGraphStorage(graphPath)
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	t.Cleanup(func() { _ = graph.Close() })

	srv1, err := NewServerWithDataDir(graph, 0, dataDir)
	if err != nil {
		t.Fatalf("NewServerWithDataDir #1: %v", err)
	}

	// Bootstrap admin should have created a user under <dataDir>/auth.
	authDir := filepath.Join(dataDir, "auth")
	if _, err := os.Stat(filepath.Join(authDir, "users.json")); err != nil {
		t.Fatalf("users.json missing under dataDir: %v (expected %s)", err, authDir)
	}

	// Mint a key directly via the store so the test is agnostic to the
	// HTTP handler wiring (which is a separate concern).
	users := srv1.userStore.ListUsers()
	if len(users) == 0 {
		t.Fatal("no admin user created on bootstrap")
	}
	adminID := users[0].ID
	_, keyString, err := srv1.apiKeyStore.CreateKeyWithEnv(adminID, "test-persistence", []string{"read", "write"}, 0, "test")
	if err != nil {
		t.Fatalf("CreateKeyWithEnv: %v", err)
	}
	if err := srv1.SaveAuthData(); err != nil {
		t.Fatalf("SaveAuthData: %v", err)
	}

	// Contract #1: apikeys.json appears under <dataDir>/auth, not CWD.
	if _, err := os.Stat(filepath.Join(authDir, "apikeys.json")); err != nil {
		t.Fatalf("apikeys.json missing under dataDir: %v", err)
	}
	if _, err := os.Stat("./data/server/auth/apikeys.json"); err == nil {
		t.Error("apikeys.json leaked to ./data/server/auth — dataDir not honoured")
	}

	// Contract #2: a fresh server pointed at the same dataDir
	// recognises the minted key.
	srv2, err := NewServerWithDataDir(graph, 0, dataDir)
	if err != nil {
		t.Fatalf("NewServerWithDataDir #2: %v", err)
	}
	if _, err := srv2.apiKeyStore.ValidateKeyForEnv(keyString, "test"); err != nil {
		t.Errorf("key minted on srv1 fails to validate on srv2: %v — HMAC secret not round-tripped via dataDir", err)
	}
}
