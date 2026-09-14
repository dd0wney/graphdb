package graphdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// C1: a POST must not be retried on a 5xx. Before the fix, isRetryable
// checked only the status, so the persistent 500 was retried twice (three
// calls total) even though the server may already have applied the write
// (M-11: a retried POST can duplicate a mutation).
func TestPostNotRetriedOn5xx(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/nodes" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.Nodes.Create(context.Background(), []string{"Person"}, map[string]any{"name": "Alice"}); err == nil {
		t.Fatal("Create: want an error from the persistent 500")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (a POST must not be retried on a 5xx)", calls)
	}
}

// C2: a GET is still retried on a 5xx. This guards the policy in C1 from
// going too wide and dropping retries for a safe, idempotent method.
func TestGetStillRetriedOn5xx(t *testing.T) {
	var calls int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"id":1,"labels":["Person"],"properties":{}}`))
	})
	if _, err := c.Nodes.Get(context.Background(), 1); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (a GET is retried on a 5xx)", calls)
	}
}

// TestIsRetryableMethodAndStatus is the table test the review asked for: it
// pins isRetryable's method/status matrix directly, rather than only through
// C1/C2's end-to-end HTTP behaviour.
func TestIsRetryableMethodAndStatus(t *testing.T) {
	methodWantsRetry := map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodPut:     true,
		http.MethodDelete:  true,
		http.MethodOptions: true,
		http.MethodPost:    false,
		http.MethodPatch:   false,
	}
	statuses := []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable}

	for method, want := range methodWantsRetry {
		for _, status := range statuses {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				if got := isRetryable(method, status); got != want {
					t.Errorf("isRetryable(%q, %d) = %v, want %v", method, status, got, want)
				}
			})
		}
	}
}

func TestBackoffClampsAtLargeAttempts(t *testing.T) {
	for _, attempt := range []int{0, 1, 5, 40, 63} {
		d := backoff(attempt)
		if d <= 0 || d > 2*time.Second {
			t.Errorf("backoff(%d) = %v, want (0, 2s]", attempt, d)
		}
	}
}
