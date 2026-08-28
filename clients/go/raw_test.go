package graphdb

import (
	"context"
	"net/http"
	"testing"
)

func TestRaw(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hybrid-search" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	res, err := c.Raw(context.Background(), http.MethodPost, "/hybrid-search", map[string]any{"q": "x"})
	if err != nil || res.Status != 200 {
		t.Fatalf("raw: %v %+v", err, res)
	}
	if string(res.Body) != `{"ok":true}` {
		t.Errorf("body = %s", res.Body)
	}
}

func TestRawReportsActualStatus(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	})
	res, err := c.Raw(context.Background(), http.MethodPost, "/nodes", map[string]any{})
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if res.Status != http.StatusCreated {
		t.Errorf("Status = %d, want 201 (must report the real status, not assume 200)", res.Status)
	}
}
