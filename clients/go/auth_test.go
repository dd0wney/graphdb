package graphdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLoginThenRefreshOn401(t *testing.T) {
	var logins, refreshes, protectedCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			logins++
			_, _ = w.Write([]byte(`{"access_token":"t1","refresh_token":"r1"}`))
		case "/auth/refresh":
			refreshes++
			_, _ = w.Write([]byte(`{"access_token":"t2"}`))
		case "/nodes/1":
			protectedCalls++
			if r.Header.Get("Authorization") == "Bearer t1" {
				w.WriteHeader(http.StatusUnauthorized) // stale token
				return
			}
			_, _ = w.Write([]byte(`{"id":1}`))
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL, WithLogin("u", "p"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.t.request(context.Background(), http.MethodGet, "/nodes/1", nil, nil); err != nil {
		t.Fatalf("request: %v", err)
	}
	if logins != 1 || refreshes != 1 {
		t.Errorf("logins=%d refreshes=%d, want 1/1", logins, refreshes)
	}
	if protectedCalls != 2 {
		t.Errorf("protectedCalls=%d, want 2 (initial 401 + retry)", protectedCalls)
	}
}

// stale401Server 401s any protected request bearing staleToken and accepts the
// token minted by /auth/refresh. Counters are atomic: handlers run concurrently.
func stale401Server(t *testing.T, staleToken, freshToken string, refreshes *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/refresh":
			refreshes.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"` + freshToken + `"}`))
		default:
			if r.Header.Get("Authorization") == "Bearer "+staleToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":1}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The client must be safe for concurrent use: refresh() rewrites the token
// while other goroutines read it for auth headers. Run under -race.
func TestConcurrentRequestsDuringRefreshAreRaceFree(t *testing.T) {
	var refreshes atomic.Int64
	srv := stale401Server(t, "t1", "t2", &refreshes)

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "t1", refreshToken: "r1", maxRetries: 0}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
				t.Errorf("request: %v", err)
			}
		}()
	}
	wg.Wait()
}

// N goroutines that all hit 401 on the same stale token must produce exactly
// one refresh call: refresh tokens are commonly single-use server-side, so a
// refresh stampede would invalidate the session.
func TestConcurrent401sCoalesceToOneRefresh(t *testing.T) {
	var refreshes atomic.Int64
	srv := stale401Server(t, "t1", "t2", &refreshes)

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "t1", refreshToken: "r1", maxRetries: 0}

	// Hold all workers at a barrier so they read the stale token together.
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
				t.Errorf("request: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := refreshes.Load(); got != 1 {
		t.Errorf("refreshes = %d, want 1 (concurrent 401s must coalesce)", got)
	}
}

// The auth endpoints receive credentials in the body; the stale Bearer token
// must not ride along in the Authorization header (needless exposure, and
// gateways that validate Bearers globally would reject the refresh itself).
func TestAuthEndpointsReceiveNoAuthorizationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login", "/auth/refresh":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("%s got Authorization %q, want none", r.URL.Path, got)
			}
			_, _ = w.Write([]byte(`{"access_token":"t2","refresh_token":"r2"}`))
		default:
			if r.Header.Get("Authorization") == "Bearer stale" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":1}`))
		}
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(),
		token: "stale", refreshToken: "r1", username: "u", password: "p", maxRetries: 0}
	if _, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("request: %v", err)
	}
}

// When refresh fails and there is no username/password fallback, the refresh
// failure itself must surface — not be swallowed into a second stale-token 401.
func TestRefreshFailureWithoutLoginFallbackSurfaces(t *testing.T) {
	var protectedCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/refresh" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"refresh store down"}`))
			return
		}
		protectedCalls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tr := &transport{baseURL: srv.URL, http: srv.Client(), token: "t1", refreshToken: "r1", maxRetries: 2}
	_, err := tr.request(context.Background(), http.MethodGet, "/x", nil, nil)
	var ae *Error
	if !errors.As(err, &ae) || ae.Path != "/auth/refresh" {
		t.Fatalf("err = %v, want the /auth/refresh *Error to surface", err)
	}
	if protectedCalls != 1 {
		t.Errorf("protectedCalls = %d, want 1 (no pointless stale-token retry)", protectedCalls)
	}
}
