package graphdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestEdgesListFollowsCursor(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/edges" {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
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
	got, err := c.Edges.ListAll(context.Background(), ListEdgesOptions{Type: "KNOWS", PageSize: 2})
	if err != nil {
		t.Fatalf("listall: %v", err)
	}
	var ids []uint64
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	if fmt.Sprint(ids) != "[1 2 3]" {
		t.Errorf("ids = %v, want [1 2 3]", ids)
	}
}

func TestEdgesListSendsLimitAndTypeParams(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "2" || q.Get("type") != "KNOWS" {
			t.Errorf("query = %s, want limit=2&type=KNOWS", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"id":1}]`))
	})
	if _, err := c.Edges.ListAll(context.Background(), ListEdgesOptions{Type: "KNOWS", PageSize: 2}); err != nil {
		t.Fatalf("listall: %v", err)
	}
}

// Breaking out of the range must stop pagination: no further page fetches,
// and no panic from the iterator calling yield after it returned false.
func TestEdgesListEarlyBreakStopsPagination(t *testing.T) {
	var pages int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		w.Header().Set("X-Next-Cursor", fmt.Sprintf("c%d", pages))
		_, _ = w.Write([]byte(`[{"id":1},{"id":2}]`))
	})
	var seen int
	for _, err := range c.Edges.List(context.Background(), ListEdgesOptions{}) {
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		seen++
		if seen == 1 {
			break
		}
	}
	if seen != 1 || pages != 1 {
		t.Errorf("seen=%d pages=%d, want 1/1 (break must stop fetching)", seen, pages)
	}
}

func TestEdgesListErrorMidPagination(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Header().Set("X-Next-Cursor", "c1")
			_, _ = w.Write([]byte(`[{"id":1}]`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"page 2 exploded"}`))
	})
	_, err := c.Edges.ListAll(context.Background(), ListEdgesOptions{})
	if !errors.Is(err, ErrServer) {
		t.Fatalf("err = %v, want ErrServer from page 2", err)
	}
}
