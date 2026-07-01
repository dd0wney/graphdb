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
