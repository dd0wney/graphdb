package graphdb

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestFromResponseConflictingNodeID(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   uint64
	}{
		{"409 with the field", 409, `{"error":"Conflict","message":"m","code":409,"conflicting_node_id":42}`, 42},
		{"409 without the field", 409, `{"error":"Conflict","message":"m","code":409}`, 0},
		{"409 with a non-JSON body", 409, `nope`, 0},
		{"non-409 ignores the field", 400, `{"error":"x","conflicting_node_id":42}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := fromResponse(c.status, []byte(c.body), "POST", "/nodes")
			var ae *Error
			if !errors.As(err, &ae) {
				t.Fatal("expected *Error")
			}
			if ae.ConflictingNodeID != c.want {
				t.Errorf("ConflictingNodeID = %d, want %d", ae.ConflictingNodeID, c.want)
			}
			if c.status == 409 && !errors.Is(err, ErrConflict) {
				t.Error("errors.Is(err, ErrConflict) = false")
			}
		})
	}
}

func TestNodesCreateConflictCarriesHoldingNodeID(t *testing.T) {
	for _, c := range []struct {
		name string
		body string
		want uint64
	}{
		{"with conflicting_node_id", `{"error":"Conflict","message":"held","code":409,"conflicting_node_id":9}`, 9},
		{"without conflicting_node_id", `{"error":"Conflict","message":"held","code":409}`, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(c.body))
			})
			_, err := cl.Nodes.Create(context.Background(), []string{"Widget"}, map[string]any{"key": "k"})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("err = %v, want ErrConflict", err)
			}
			var ae *Error
			if !errors.As(err, &ae) {
				t.Fatal("expected *Error")
			}
			if ae.ConflictingNodeID != c.want {
				t.Errorf("ConflictingNodeID = %d, want %d", ae.ConflictingNodeID, c.want)
			}
		})
	}
}
