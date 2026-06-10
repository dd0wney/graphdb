package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// probeStatus runs a request bearing token through requireAuth wrapping a
// 200 probe and returns the status code.
func probeStatus(server *Server, token string) int {
	probe := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	mux := http.NewServeMux()
	mux.HandleFunc("/_probe", server.requireAuth(probe))

	req := httptest.NewRequest(http.MethodGet, "/_probe", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr.Code
}

// TestRequireAuth_RevocationViaGeneration pins security audit finding M-7:
// bumping a user's token-generation counter (explicit revoke, password
// change, or role change) must invalidate every outstanding token on the
// next request — without rotating the global JWT secret.
//
// RED against pre-fix code: there is no generation field/check, so the
// token stays valid after revocation.
func TestRequireAuth_RevocationViaGeneration(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	user, err := server.userStore.CreateUser("revoke-me", "StrongPass123!", "editor")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	mint := func() string {
		tok, err := server.jwtManager.GenerateTokenWithGeneration(user.ID, user.Username, user.Role, "", user.TokenGeneration)
		if err != nil {
			t.Fatalf("mint token (gen %d): %v", user.TokenGeneration, err)
		}
		return tok
	}

	cases := []struct {
		name   string
		revoke func()
	}{
		{"explicit revoke", func() { _ = server.userStore.RevokeUserTokens(user.ID) }},
		{"password change", func() { _ = server.userStore.ChangePassword(user.ID, "EvenStronger456!") }},
		{"role change", func() { _ = server.userStore.UpdateUserRole(user.ID, "viewer") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Fresh user state per subtest is awkward with a shared user, so
			// re-read the live generation and mint a current token first.
			liveUser, _ := server.userStore.GetUserByID(user.ID)
			user.TokenGeneration = liveUser.TokenGeneration
			token := mint()

			if code := probeStatus(server, token); code != http.StatusOK {
				t.Fatalf("current token: want 200, got %d", code)
			}

			tc.revoke()

			if code := probeStatus(server, token); code != http.StatusUnauthorized {
				t.Errorf("after %s: want 401 (token revoked), got %d", tc.name, code)
			}

			// A token minted at the NEW generation must work again.
			liveUser, _ = server.userStore.GetUserByID(user.ID)
			user.TokenGeneration = liveUser.TokenGeneration
			if code := probeStatus(server, mint()); code != http.StatusOK {
				t.Errorf("re-minted token at new generation: want 200, got %d", code)
			}
		})
	}
}

// TestRequireAuth_LegacyGenZeroTokenValid pins backward compatibility: a
// token with no generation claim (legacy / offline mint-token, generation
// 0) is accepted for a user whose counter is still 0.
func TestRequireAuth_LegacyGenZeroTokenValid(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	user, err := server.userStore.CreateUser("legacy-user", "StrongPass123!", "viewer")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// GenerateTokenWithTenant omits the gen claim (generation 0).
	token, err := server.jwtManager.GenerateTokenWithTenant(user.ID, user.Username, user.Role, "")
	if err != nil {
		t.Fatalf("mint legacy token: %v", err)
	}
	if code := probeStatus(server, token); code != http.StatusOK {
		t.Errorf("legacy gen-0 token for fresh user: want 200, got %d", code)
	}
}
