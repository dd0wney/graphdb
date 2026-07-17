package graphdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTransportRequestSuccessAndAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/nodes" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42}`))
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "tok", maxRetries: 0}
	res, err := tr.request(context.Background(), http.MethodPost, "/nodes", map[string]any{"x": 1}, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var n Node
	if err := json.Unmarshal(res.data, &n); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if n.ID != 42 {
		t.Errorf("id = %d", n.ID)
	}
}

func TestTransportMapsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), maxRetries: 0}
	_, err := tr.request(context.Background(), http.MethodGet, "/nodes/1", nil, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestTransportRetriesRetryableStatus(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), maxRetries: 2}
	if _, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("request: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one retry)", calls)
	}
}

func TestRefreshDoesNotConsumeRetryBudget(t *testing.T) {
	var protectedCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/refresh":
			_, _ = w.Write([]byte(`{"access_token":"t2"}`))
		default:
			protectedCalls++
			if protectedCalls == 1 {
				w.WriteHeader(http.StatusUnauthorized) // triggers one refresh, no budget spent
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable) // persistent retryable
		}
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "t1", refreshToken: "r1", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatal("expected error from persistent 503")
	}
	// 1 (401) + 1 initial-after-refresh + 2 retries = 4 protected calls.
	if protectedCalls != 4 {
		t.Errorf("protectedCalls = %d, want 4 (401 must not consume the 2-retry budget)", protectedCalls)
	}
}

func TestStaticToken401ReturnsErrAuthWithoutRefresh(t *testing.T) {
	var calls, refreshes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			refreshes++
			return
		}
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "static", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if calls != 1 || refreshes != 0 {
		t.Errorf("calls=%d refreshes=%d, want 1/0 (static token must not refresh or retry)", calls, refreshes)
	}
}

func TestAPIKey401ReturnsErrAuthWithoutRefresh(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("X-API-Key"); got != "k1" {
			t.Errorf("X-API-Key = %q", got)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), apiKey: "k1", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestPersistent401AfterRefreshErrorsInsteadOfLooping(t *testing.T) {
	var protectedCalls, refreshes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			refreshes++
			_, _ = w.Write([]byte(`{"access_token":"t2"}`))
			return
		}
		protectedCalls++
		w.WriteHeader(http.StatusUnauthorized) // still 401 with the new token
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "t1", refreshToken: "r1", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if protectedCalls != 2 || refreshes != 1 {
		t.Errorf("protectedCalls=%d refreshes=%d, want 2/1 (refresh once, then give up)", protectedCalls, refreshes)
	}
}

func TestRefreshFailureFallsBackToLoginWhoseErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/refresh":
			w.WriteHeader(http.StatusInternalServerError)
		case "/auth/login":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"account locked"}`))
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(),
		token: "t1", refreshToken: "r1", username: "u", password: "p", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth from the failed re-login", err)
	}
	var ae *Error
	if !errors.As(err, &ae) || ae.Path != "/auth/login" {
		t.Errorf("err = %v, want the login *Error to surface", err)
	}
}

// With only a refresh token (no username), a failed refresh is swallowed and
// the stale token is retried once; the second 401 then surfaces as ErrAuth.
// This pins intended behavior: no infinite loop, no panic.
func TestRefreshEndpointFailureWithoutLoginRetriesStaleTokenOnce(t *testing.T) {
	var protectedCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		protectedCalls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "t1", refreshToken: "r1", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
	if protectedCalls != 2 {
		t.Errorf("protectedCalls = %d, want 2 (one stale retry, then error)", protectedCalls)
	}
}

func TestContextCancelledDuringRetryBackoff(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// First backoff is 100ms; a 50ms deadline expires inside it.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "tok", maxRetries: 2}
	_, err := tr.request(ctx, http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retry after cancellation)", calls)
	}
}

func TestTransportRetries429(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "tok", maxRetries: 2}
	if _, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("request: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (429 is retryable)", calls)
	}
}
