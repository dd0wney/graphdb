package graphdb

import (
	"context"
	"net/http"
	"net/http/httptest"
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
