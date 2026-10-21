package graphdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestNodesUpsertCreatedAndMatched(t *testing.T) {
	for _, c := range []struct {
		name    string
		status  int
		created bool
	}{
		{"created", http.StatusCreated, true},
		{"matched", http.StatusOK, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var gotMethod, gotPath, gotQuery string
			var gotBody map[string]any
			cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.Query().Get("on_conflict")
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.WriteHeader(c.status)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": 7, "labels": []string{"Host"}, "properties": map[string]any{"ip": "10.0.0.1"}, "created": c.created,
				})
			})
			node, created, err := cl.Nodes.Upsert(context.Background(), []string{"Host"}, map[string]any{"ip": "10.0.0.1"})
			if err != nil {
				t.Fatal(err)
			}
			if gotMethod != http.MethodPost || gotPath != "/nodes" || gotQuery != "merge" {
				t.Errorf("request = %s %s on_conflict=%q", gotMethod, gotPath, gotQuery)
			}
			if gotBody["labels"] == nil || gotBody["properties"] == nil {
				t.Errorf("body = %v", gotBody)
			}
			if created != c.created {
				t.Errorf("created = %v, want %v", created, c.created)
			}
			if node == nil || node.ID != 7 || node.Labels[0] != "Host" {
				t.Errorf("node = %+v", node)
			}
		})
	}
}

func TestNodesUpsertErrors(t *testing.T) {
	respond := func(status int, body string) *Client {
		return newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		})
	}
	call := func(cl *Client) (*Node, bool, error) {
		return cl.Nodes.Upsert(context.Background(), []string{"Host"}, map[string]any{"ip": "x"})
	}

	t.Run("400 no rule", func(t *testing.T) {
		_, _, err := call(respond(400, `{"error":"Bad Request","message":"no uniqueness rule covers these labels; on_conflict=merge needs one","code":400}`))
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != 400 {
			t.Fatalf("err = %v, want *Error status 400", err)
		}
		if ae.ConflictingNodeID != 0 || ae.ConflictingNodeIDs != nil || ae.MissingLabels != nil {
			t.Errorf("conflict fields set on a 400: %+v", ae)
		}
	})
	t.Run("409 labels differ", func(t *testing.T) {
		node, created, err := call(respond(409, `{"error":"Conflict","message":"m","code":409,"conflicting_node_id":5,"missing_labels":["Asset"]}`))
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("err = %v, want ErrConflict", err)
		}
		var ae *Error
		if !errors.As(err, &ae) {
			t.Fatal("expected *Error")
		}
		if ae.ConflictingNodeID != 5 || !reflect.DeepEqual(ae.MissingLabels, []string{"Asset"}) {
			t.Errorf("error fields = %+v", ae)
		}
		if node != nil || created {
			t.Errorf("node=%v created=%v on error", node, created)
		}
	})
	t.Run("409 ambiguous", func(t *testing.T) {
		_, _, err := call(respond(409, `{"error":"Conflict","message":"m","code":409,"conflicting_node_ids":[3,4]}`))
		var ae *Error
		if !errors.Is(err, ErrConflict) || !errors.As(err, &ae) {
			t.Fatalf("err = %v", err)
		}
		if !reflect.DeepEqual(ae.ConflictingNodeIDs, []uint64{3, 4}) || ae.ConflictingNodeID != 0 {
			t.Errorf("error fields = %+v", ae)
		}
	})
}

func TestNodesUpsertNotDurable(t *testing.T) {
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":9,"labels":["Host"],"properties":{},"created":true,"message":"wal failed"}`))
	})
	node, created, err := cl.Nodes.Upsert(context.Background(), []string{"Host"}, nil)
	if !errors.Is(err, ErrNotDurable) {
		t.Fatalf("err = %v, want ErrNotDurable", err)
	}
	var nd *NotDurableError
	if !errors.As(err, &nd) || nd.ID != 9 {
		t.Errorf("NotDurableError = %+v", nd)
	}
	if node == nil || node.ID != 9 || !created {
		t.Errorf("node=%+v created=%v", node, created)
	}
}

func TestNodesBatchUpsert(t *testing.T) {
	var gotQuery string
	var gotBody struct {
		Nodes []NodeInput `json:"nodes"`
	}
	cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("on_conflict")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"nodes":[
			{"id":1,"labels":["Host"],"properties":{"ip":"a"},"created":true},
			{"id":2,"labels":["Host"],"properties":{"ip":"b"},"created":false}],
			"created":1,"matched":1,"time":"1ms","failed":1,
			"errors":[{"index":2,"error":"no uniqueness rule"}]}`))
	})
	res, err := cl.Nodes.BatchUpsert(context.Background(), []NodeInput{
		{Labels: []string{"Host"}, Properties: map[string]any{"ip": "a"}},
		{Labels: []string{"Host"}, Properties: map[string]any{"ip": "b"}},
		{Labels: []string{"Nope"}, Properties: map[string]any{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "merge" || len(gotBody.Nodes) != 3 {
		t.Errorf("query=%q sent=%d nodes", gotQuery, len(gotBody.Nodes))
	}
	if res.Created != 1 || res.Matched != 1 || res.Failed != 1 || len(res.Nodes) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if !res.Nodes[0].Created || res.Nodes[0].ID != 1 || res.Nodes[1].Created || res.Nodes[1].ID != 2 {
		t.Errorf("nodes = %+v", res.Nodes)
	}
	if len(res.Errors) != 1 || res.Errors[0].Index != 2 || res.Errors[0].Error != "no uniqueness rule" {
		t.Errorf("errors = %+v", res.Errors)
	}
}
