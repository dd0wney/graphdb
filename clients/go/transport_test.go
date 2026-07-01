package graphdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
