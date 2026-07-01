package graphdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, WithToken("tok"), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNodesCreateAndGet(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/nodes":
			var body struct {
				Labels     []string       `json:"labels"`
				Properties map[string]any `json:"properties"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Labels) != 1 || body.Labels[0] != "Person" {
				t.Errorf("labels = %v", body.Labels)
			}
			_, _ = w.Write([]byte(`{"id":7,"labels":["Person"],"properties":{"name":"Alice"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/nodes/7":
			_, _ = w.Write([]byte(`{"id":7,"labels":["Person"],"properties":{"name":"Alice"}}`))
		}
	})
	n, err := c.Nodes.Create(context.Background(), []string{"Person"}, map[string]any{"name": "Alice"})
	if err != nil || n.ID != 7 {
		t.Fatalf("create: %v id=%d", err, n.ID)
	}
	g, err := c.Nodes.Get(context.Background(), 7)
	if err != nil || g.Properties["name"] != "Alice" {
		t.Fatalf("get: %v", err)
	}
}

func TestNodesListFollowsCursor(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "":
			w.Header().Set("X-Next-Cursor", "c1")
			_, _ = w.Write([]byte(`[{"id":1},{"id":2}]`))
		case "c1":
			// no next cursor -> last page
			_, _ = w.Write([]byte(`[{"id":3}]`))
		default:
			t.Errorf("unexpected cursor %q", cursor)
		}
	})
	got, err := c.Nodes.ListAll(context.Background(), ListOptions{Label: "Person", PageSize: 2})
	if err != nil {
		t.Fatalf("listall: %v", err)
	}
	var ids []uint64
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	if fmt.Sprint(ids) != "[1 2 3]" {
		t.Errorf("ids = %v, want [1 2 3]", ids)
	}
}
