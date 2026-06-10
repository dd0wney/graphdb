package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubValidator accepts exactly one token string, returning fixed claims —
// standing in for the OIDC validator inside the composite chain.
type stubValidator struct {
	accept string
	claims *Claims
}

func (s *stubValidator) ValidateToken(_ context.Context, token string) (*Claims, error) {
	if token == s.accept {
		return s.claims, nil
	}
	return nil, errors.New("stub: token not accepted")
}

func (s *stubValidator) Name() string { return "stub" }

// TestUserManagementHandler_UsesConfiguredValidator pins security audit
// finding M-8 / AUTH-4: the user-management handler validated access
// tokens with the bare JWTManager, so an OIDC-provisioned admin (whose
// token only the composite validator accepts) got 401 and could not manage
// users — forcing a parallel local-admin credential store. After the fix
// the handler validates through SetTokenValidator's validator.
//
// RED against pre-fix code: there is no tokenValidator field; the handler
// always calls jwtManager.ValidateToken, which rejects the OIDC token.
func TestUserManagementHandler_UsesConfiguredValidator(t *testing.T) {
	store := NewUserStore()
	jwtManager, _ := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewUserManagementHandler(store, jwtManager)

	const oidcToken = "oidc-opaque-token"
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+oidcToken)

	// Before wiring the composite: the bare jwtManager rejects the OIDC token.
	if _, err := handler.extractAndValidateToken(req); err == nil {
		t.Fatal("jwtManager-only path unexpectedly accepted the OIDC token")
	}

	// Wire a validator that accepts the OIDC token (as the composite would).
	handler.SetTokenValidator(&stubValidator{
		accept: oidcToken,
		claims: &Claims{Username: "oidc-admin", Role: RoleAdmin},
	})

	claims, err := handler.extractAndValidateToken(req)
	if err != nil {
		t.Fatalf("composite validator path rejected the OIDC token: %v", err)
	}
	if claims.Role != RoleAdmin || claims.Username != "oidc-admin" {
		t.Errorf("got claims %+v, want admin/oidc-admin", claims)
	}
}

// TestAuthHandler_UsesConfiguredValidator pins the same AUTH-4 fix for the
// AuthHandler (/auth/register, /auth/me).
func TestAuthHandler_UsesConfiguredValidator(t *testing.T) {
	store := NewUserStore()
	jwtManager, _ := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	const oidcToken = "oidc-opaque-token"
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+oidcToken)

	if _, err := handler.extractAndValidateToken(req); err == nil {
		t.Fatal("jwtManager-only path unexpectedly accepted the OIDC token")
	}

	handler.SetTokenValidator(&stubValidator{
		accept: oidcToken,
		claims: &Claims{Username: "oidc-admin", Role: RoleAdmin},
	})

	if _, err := handler.extractAndValidateToken(req); err != nil {
		t.Fatalf("composite validator path rejected the OIDC token: %v", err)
	}
}

// TestSetTokenValidator_NilIgnored pins that a nil validator never clears
// the jwtManager default (which would 401 every request).
func TestSetTokenValidator_NilIgnored(t *testing.T) {
	store := NewUserStore()
	jwtManager, _ := NewJWTManager("test-secret-key-must-be-at-least-32-characters-long", DefaultTokenDuration, DefaultRefreshTokenDuration)
	handler := NewAuthHandler(store, jwtManager)

	handler.SetTokenValidator(nil)

	user, err := store.CreateUser("bob", "BobPass123!", RoleViewer)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, err := jwtManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	if _, err := handler.extractAndValidateToken(req); err != nil {
		t.Fatalf("nil SetTokenValidator cleared the jwtManager default: %v", err)
	}
}
