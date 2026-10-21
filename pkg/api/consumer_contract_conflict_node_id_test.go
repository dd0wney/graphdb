package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// CONSUMER CONTRACT: CC26-uniqueness-conflict-names-holder — mapper (coord graphdb:v1.4-rest-conflict-node-id)
//
// mapper creates a node under a uniqueness rule and, on a 409, needs the ID
// of the node that already holds the key. It reads the JSON number
// "conflicting_node_id" from the 409 body instead of parsing the message
// text. The body is decoded through literal keys, not through a Go struct,
// so a rename of the field fails here. The error, message and code keys stay
// so that older clients do not break.
func TestUniquenessConflict_BodyNamesTheHoldingNode(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "widget_key", Label: "Widget", PropertyKey: "key",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}

	req := NodeRequest{Labels: []string{"Widget"}, Properties: map[string]any{"key": "k1"}}

	first := postNode(t, server, req)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201, body=%s", first.Code, first.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	firstID, ok := created["id"].(float64)
	if !ok {
		t.Fatalf(`201 body has no numeric "id": %s`, first.Body.String())
	}

	second := postNode(t, server, req)
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate create: status = %d, want 409, body=%s", second.Code, second.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}

	got, ok := body["conflicting_node_id"].(float64)
	if !ok {
		t.Fatalf(`409 body has no numeric "conflicting_node_id": %s`, second.Body.String())
	}
	if got != firstID {
		t.Errorf("conflicting_node_id = %v, want %v (the node the first create returned)", got, firstID)
	}

	// The existing keys must keep their values.
	if body["error"] != "Conflict" {
		t.Errorf(`"error" = %v, want "Conflict"`, body["error"])
	}
	if code, _ := body["code"].(float64); code != http.StatusConflict {
		t.Errorf(`"code" = %v, want 409`, body["code"])
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Errorf(`"message" is empty`)
	}
	// Tenant and rule name stay out of the body.
	for _, k := range []string{"tenant_id", "rule", "rule_name"} {
		if _, has := body[k]; has {
			t.Errorf("409 body carries %q, which must stay private", k)
		}
	}
}
