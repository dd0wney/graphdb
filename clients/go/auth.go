package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
)

func (t *transport) usesLogin() bool { return t.username != "" || t.refreshToken != "" }

// login exchanges username/password for tokens.
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
	t.token = tok.Access
	t.refreshToken = tok.Refresh
	return nil
}

// refresh tries the refresh token, falling back to a full re-login.
func (t *transport) refresh(ctx context.Context) error {
	if t.refreshToken != "" {
		res, err := t.rawAttempt(ctx, http.MethodPost, "/auth/refresh",
			map[string]string{"refresh_token": t.refreshToken})
		if err == nil {
			var tok struct {
				Access string `json:"access_token"`
			}
			if json.Unmarshal(res, &tok) == nil && tok.Access != "" {
				t.token = tok.Access
				return nil
			}
		}
	}
	if t.username != "" {
		return t.login(ctx)
	}
	return nil
}
