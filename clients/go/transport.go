package graphdb

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type apiResult struct {
	status int
	data   json.RawMessage
	header http.Header
}

type transport struct {
	baseURL string
	http    *http.Client

	// immutable after New
	apiKey   string
	username string
	password string

	// mu guards the mutable token fields (refresh rewrites them while other
	// goroutines read them for auth headers).
	mu           sync.Mutex
	token        string
	refreshToken string

	// refreshMu serializes login/refresh so concurrent 401s coalesce into a
	// single token exchange. Never held while mu is held.
	refreshMu sync.Mutex

	maxRetries int
}

func (t *transport) currentToken() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.token
}

func (t *transport) authHeaders(h http.Header, token string) {
	switch {
	case token != "":
		h.Set("Authorization", "Bearer "+token)
	case t.apiKey != "":
		h.Set("X-API-Key", t.apiKey)
	}
}

// request performs a JSON request with retry on retryable statuses. On a first
// 401 it refreshes (or lazily logs in) and retries once.
func (t *transport) request(ctx context.Context, method, path string, body any, params url.Values) (*apiResult, error) {
	if t.username != "" {
		if err := t.loginIfNeeded(ctx); err != nil {
			return nil, err
		}
	}
	refreshed := false
	retries := 0
	for {
		// Snapshot the token used for this attempt so the 401 handler can
		// tell whether a concurrent caller already replaced it.
		usedToken := t.currentToken()
		resp, err := t.attempt(ctx, method, path, body, params, usedToken)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusUnauthorized && !refreshed && t.usesLogin() {
			resp.Body.Close()
			refreshed = true
			if err := t.refreshIfStale(ctx, usedToken); err != nil {
				return nil, err
			}
			continue // retry once with the new token; does NOT consume the retry budget
		}
		if resp.StatusCode >= 400 && retries < t.maxRetries && isRetryable(resp.StatusCode) {
			resp.Body.Close()
			select {
			case <-time.After(backoff(retries)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			retries++
			continue
		}
		defer resp.Body.Close()
		data, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 400 {
			return nil, fromResponse(resp.StatusCode, data, method, path)
		}
		return &apiResult{status: resp.StatusCode, data: data, header: resp.Header}, nil
	}
}

// rawAttempt performs a single unauthenticated request and returns the raw
// body. It is used only by login/refresh: no Authorization header is sent
// (credentials travel in the body, and a stale Bearer would leak to
// auth-adjacent logs or be rejected by gateways that validate it globally),
// and it must not re-enter the 401-refresh path in request().
func (t *transport) rawAttempt(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	resp, err := t.attempt(ctx, method, path, body, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 400 {
		return nil, fromResponse(resp.StatusCode, data, method, path)
	}
	return data, nil
}

func (t *transport) attempt(ctx context.Context, method, path string, body any, params url.Values, token string) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	u := strings.TrimRight(t.baseURL, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	t.authHeaders(req.Header, token)
	return t.http.Do(req)
}

func isRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func backoff(attempt int) time.Duration {
	// 100<<attempt overflows for large attempt values; the cap is reached at
	// attempt 5 anyway.
	if attempt > 4 {
		return 2 * time.Second
	}
	d := time.Duration(100<<attempt) * time.Millisecond
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}
