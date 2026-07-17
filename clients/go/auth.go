package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
)

func (t *transport) usesLogin() bool {
	if t.username != "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.refreshToken != ""
}

// loginIfNeeded performs the lazy first login at most once across goroutines.
func (t *transport) loginIfNeeded(ctx context.Context) error {
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()
	if t.currentToken() != "" {
		return nil
	}
	return t.login(ctx)
}

// refreshIfStale refreshes only if usedToken (the token that just got a 401)
// is still current — a concurrent caller may have already replaced it, and
// refresh tokens are commonly single-use server-side.
func (t *transport) refreshIfStale(ctx context.Context, usedToken string) error {
	t.refreshMu.Lock()
	defer t.refreshMu.Unlock()
	if t.currentToken() != usedToken {
		return nil
	}
	return t.refresh(ctx)
}

// login exchanges username/password for tokens. Callers hold refreshMu.
func (t *transport) login(ctx context.Context) error {
	body := map[string]string{"username": t.username, "password": t.password}
	res, err := t.rawAttempt(ctx, http.MethodPost, "/auth/login", body)
	if err != nil {
		return err
	}
	var tok struct {
		Access  string `json:"access_token"`
		Refresh string `json:"refresh_token"`
	}
	if err := json.Unmarshal(res, &tok); err != nil {
		return err
	}
	t.mu.Lock()
	t.token = tok.Access
	t.refreshToken = tok.Refresh
	t.mu.Unlock()
	return nil
}

// refresh tries the refresh token, falling back to a full re-login. Callers
// hold refreshMu.
func (t *transport) refresh(ctx context.Context) error {
	t.mu.Lock()
	rt := t.refreshToken
	t.mu.Unlock()
	if rt != "" {
		res, err := t.rawAttempt(ctx, http.MethodPost, "/auth/refresh",
			map[string]string{"refresh_token": rt})
		if err == nil {
			var tok struct {
				Access string `json:"access_token"`
			}
			if json.Unmarshal(res, &tok) == nil && tok.Access != "" {
				t.mu.Lock()
				t.token = tok.Access
				t.mu.Unlock()
				return nil
			}
		}
	}
	if t.username != "" {
		return t.login(ctx)
	}
	return nil
}
