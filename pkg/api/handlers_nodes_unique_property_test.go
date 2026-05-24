package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postNode is a focused helper for the unique_property suite — small
// enough to keep tests readable.
func postNode(t *testing.T, server *Server, body NodeRequest) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/nodes", bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleNodes(rr, r)
	return rr
}

// TestCreateNode_UniqueProperty_HappyPath pins that an explicit
// unique_property on POST /nodes routes through
// CreateNodeWithUniquePropertyForTenant for any single-label node — not
// just :Claim. The second create with the same value for the unique
// property must return 409 with the canonical message; a node with a
// different value must succeed.
func TestCreateNode_UniqueProperty_HappyPath(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	first := postNode(t, server, NodeRequest{
		Labels:         []string{"Document"},
		Properties:     map[string]any{"doc_id": "doc-1", "title": "First"},
		UniqueProperty: "doc_id",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first should succeed, got %d body=%s", first.Code, first.Body.String())
	}

	dup := postNode(t, server, NodeRequest{
		Labels:         []string{"Document"},
		Properties:     map[string]any{"doc_id": "doc-1", "title": "Duplicate"},
		UniqueProperty: "doc_id",
	})
	if dup.Code != http.StatusConflict {
		t.Errorf("duplicate doc_id should return 409, got %d body=%s", dup.Code, dup.Body.String())
	}
	if !strings.Contains(dup.Body.String(), "unique constraint violation") {
		t.Errorf("409 body should mention 'unique constraint violation', got %s", dup.Body.String())
	}

	other := postNode(t, server, NodeRequest{
		Labels:         []string{"Document"},
		Properties:     map[string]any{"doc_id": "doc-2", "title": "Different"},
		UniqueProperty: "doc_id",
	})
	if other.Code != http.StatusCreated {
		t.Errorf("distinct doc_id should succeed, got %d body=%s", other.Code, other.Body.String())
	}
}

// TestCreateNode_UniqueProperty_ValidationGates pins the input-shape
// guards. The handler must reject zero-label, multi-label, and
// missing-property requests at 400 — not silently bypass and write a
// non-unique node.
func TestCreateNode_UniqueProperty_ValidationGates(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name string
		body NodeRequest
		want int
		hint string // substring that should appear in the 400 body
	}{
		{
			// Note: zero-label requests are rejected by the upstream
			// node-shape validator (Labels: must be at least 1) before
			// reaching the unique_property branch. The 400 surface is
			// the right outcome; hint is broadened to just "Labels".
			name: "no labels",
			body: NodeRequest{
				Labels:         []string{},
				Properties:     map[string]any{"slug": "x"},
				UniqueProperty: "slug",
			},
			want: http.StatusBadRequest,
			hint: "Labels",
		},
		{
			name: "multi-label",
			body: NodeRequest{
				Labels:         []string{"A", "B"},
				Properties:     map[string]any{"slug": "x"},
				UniqueProperty: "slug",
			},
			want: http.StatusBadRequest,
			hint: "exactly one label",
		},
		{
			name: "unique_property missing from properties",
			body: NodeRequest{
				Labels:         []string{"Article"},
				Properties:     map[string]any{"title": "x"},
				UniqueProperty: "slug",
			},
			want: http.StatusBadRequest,
			hint: "slug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := postNode(t, server, tt.body)
			if rr.Code != tt.want {
				t.Fatalf("expected %d, got %d body=%s", tt.want, rr.Code, rr.Body.String())
			}
			if tt.hint != "" && !strings.Contains(rr.Body.String(), tt.hint) {
				t.Errorf("body should mention %q, got %s", tt.hint, rr.Body.String())
			}
		})
	}
}

// TestCreateNode_UniqueProperty_WinsOverClaimFallback pins the
// precedence rule: when unique_property is set AND the labels include
// "Claim", the explicit unique_property is used as the uniqueness key
// (NOT the hardcoded "for_task"). This is the explicit-wins contract
// called out in the handler's switch comment.
func TestCreateNode_UniqueProperty_WinsOverClaimFallback(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Two :Claim nodes with the SAME explicit unique_property but
	// DIFFERENT for_task — under the explicit-wins rule, the second
	// must 409 (because unique_property=external_id collides) even
	// though for_task differs. The hardcoded path would have allowed
	// both since for_task differs.
	first := postNode(t, server, NodeRequest{
		Labels: []string{"Claim"},
		Properties: map[string]any{
			"external_id": "extern-1",
			"for_task":    "task-A",
		},
		UniqueProperty: "external_id",
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first :Claim should succeed, got %d body=%s", first.Code, first.Body.String())
	}

	dup := postNode(t, server, NodeRequest{
		Labels: []string{"Claim"},
		Properties: map[string]any{
			"external_id": "extern-1", // same external_id
			"for_task":    "task-B",   // but different for_task
		},
		UniqueProperty: "external_id",
	})
	if dup.Code != http.StatusConflict {
		t.Errorf("explicit unique_property should win — expected 409, got %d body=%s",
			dup.Code, dup.Body.String())
	}
}

// TestCreateNode_NoUniqueProperty_PreservesDefaultBehavior is a
// regression guard for the no-op path: a request without
// unique_property must behave exactly like before — vanilla create for
// non-Claim labels, hardcoded Claim uniqueness for single-label
// :Claim.
func TestCreateNode_NoUniqueProperty_PreservesDefaultBehavior(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Vanilla path: two Person nodes with identical "email" both
	// succeed because no uniqueness was requested.
	a := postNode(t, server, NodeRequest{
		Labels:     []string{"Person"},
		Properties: map[string]any{"email": "a@example.com"},
	})
	if a.Code != http.StatusCreated {
		t.Fatalf("first Person should succeed, got %d body=%s", a.Code, a.Body.String())
	}
	b := postNode(t, server, NodeRequest{
		Labels:     []string{"Person"},
		Properties: map[string]any{"email": "a@example.com"},
	})
	if b.Code != http.StatusCreated {
		t.Errorf("second Person without unique_property should succeed, got %d body=%s",
			b.Code, b.Body.String())
	}

	// Hardcoded :Claim fallback still applies when unique_property is
	// unset.
	c1 := postNode(t, server, NodeRequest{
		Labels:     []string{"Claim"},
		Properties: map[string]any{"for_task": "task-preserved"},
	})
	if c1.Code != http.StatusCreated {
		t.Fatalf("first :Claim should succeed, got %d body=%s", c1.Code, c1.Body.String())
	}
	c2 := postNode(t, server, NodeRequest{
		Labels:     []string{"Claim"},
		Properties: map[string]any{"for_task": "task-preserved"},
	})
	if c2.Code != http.StatusConflict {
		t.Errorf("duplicate :Claim should still 409 under hardcoded fallback, got %d body=%s",
			c2.Code, c2.Body.String())
	}
}

// TestBatchCreateNode_UniqueProperty pins that POST /nodes/batch honours
// unique_property per-item. Duplicates within the batch are skipped
// (partial-success contract: response only reports the survivors via
// `created`), and explicit unique_property continues to work alongside
// the hardcoded :Claim fallback.
func TestBatchCreateNode_UniqueProperty(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	body := BatchNodeRequest{
		Nodes: []NodeRequest{
			{
				Labels:         []string{"Doc"},
				Properties:     map[string]any{"id": "1", "title": "alpha"},
				UniqueProperty: "id",
			},
			{
				Labels:         []string{"Doc"},
				Properties:     map[string]any{"id": "1", "title": "duplicate"}, // collides with #1
				UniqueProperty: "id",
			},
			{
				Labels:         []string{"Doc"},
				Properties:     map[string]any{"id": "2", "title": "beta"},
				UniqueProperty: "id",
			},
			{
				// invalid: unique_property set but missing from properties
				Labels:         []string{"Doc"},
				Properties:     map[string]any{"title": "no-id"},
				UniqueProperty: "id",
			},
			{
				// invalid: unique_property with multi-label
				Labels:         []string{"Doc", "Tracer"},
				Properties:     map[string]any{"id": "3"},
				UniqueProperty: "id",
			},
		},
	}

	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/nodes/batch", bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handleBatchNodes(rr, r)

	if rr.Code != http.StatusCreated {
		t.Fatalf("batch should 201 (partial success), got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp BatchNodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Created != 2 {
		t.Errorf("expected 2 successful creates (id=1 + id=2), got %d", resp.Created)
	}
}
