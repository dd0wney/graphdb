package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestEdgesCreate(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/edges" {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["type"] != "KNOWS" {
			t.Errorf("type = %v", body["type"])
		}
		_, _ = w.Write([]byte(`{"id":9,"from_node_id":1,"to_node_id":2,"type":"KNOWS","weight":0.5}`))
	})
	e, err := c.Edges.Create(context.Background(), 1, 2, "KNOWS", EdgeCreateOptions{Weight: 0.5})
	if err != nil || e.ID != 9 || e.Weight != 0.5 {
		t.Fatalf("create: %v %+v", err, e)
	}
}

func TestEdgesUpdateOmitsWeightWhenNil(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["weight"]; ok {
			t.Errorf("weight must be omitted when nil, body=%v", body)
		}
		_, _ = w.Write([]byte(`{"id":9,"type":"KNOWS"}`))
	})
	if _, err := c.Edges.Update(context.Background(), 9, EdgeUpdateOptions{Properties: map[string]any{"since": 2020}}); err != nil {
		t.Fatalf("update: %v", err)
	}
}
