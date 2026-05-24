package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// postPropertyIndex is a tiny helper so each table-driven case stays
// noise-free. Mirrors the local helpers in handlers_vectors_test.go.
func postPropertyIndex(t *testing.T, server *Server, req PropertyIndexRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/property-indexes", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	server.handlePropertyIndexes(rr, r)
	return rr
}

// TestCreatePropertyIndex covers the happy path + validation gates on
// POST /property-indexes.
func TestCreatePropertyIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		request      PropertyIndexRequest
		expectStatus int
	}{
		{
			name: "Valid default string index",
			request: PropertyIndexRequest{
				PropertyKey: "name",
			},
			expectStatus: http.StatusCreated,
		},
		{
			name: "Valid int index",
			request: PropertyIndexRequest{
				PropertyKey: "age",
				ValueType:   "int",
			},
			expectStatus: http.StatusCreated,
		},
		{
			name: "Valid float index",
			request: PropertyIndexRequest{
				PropertyKey: "score",
				ValueType:   "float",
			},
			expectStatus: http.StatusCreated,
		},
		{
			name: "Valid bool index",
			request: PropertyIndexRequest{
				PropertyKey: "active",
				ValueType:   "bool",
			},
			expectStatus: http.StatusCreated,
		},
		{
			name: "Missing property_key",
			request: PropertyIndexRequest{
				ValueType: "string",
			},
			expectStatus: http.StatusBadRequest,
		},
		{
			name: "Whitespace-only property_key",
			request: PropertyIndexRequest{
				PropertyKey: "   ",
			},
			expectStatus: http.StatusBadRequest,
		},
		{
			name: "Unsupported value_type",
			request: PropertyIndexRequest{
				PropertyKey: "weird",
				ValueType:   "nonexistent",
			},
			expectStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := postPropertyIndex(t, server, tt.request)
			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}
			if rr.Code == http.StatusCreated {
				var resp PropertyIndexResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("parse response: %v", err)
				}
				if resp.PropertyKey != tt.request.PropertyKey {
					t.Errorf("property_key=%q, want %q", resp.PropertyKey, tt.request.PropertyKey)
				}
			}
		})
	}
}

// TestCreatePropertyIndex_Conflict pins that a duplicate POST returns
// 409 rather than silently succeeding or returning 200.
func TestCreatePropertyIndex_Conflict(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	req := PropertyIndexRequest{PropertyKey: "title", ValueType: "string"}
	if rr := postPropertyIndex(t, server, req); rr.Code != http.StatusCreated {
		t.Fatalf("seed create: expected 201, got %d body=%s", rr.Code, rr.Body.String())
	}

	rr := postPropertyIndex(t, server, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("duplicate create: expected %d, got %d body=%s",
			http.StatusConflict, rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "already exists") {
		t.Errorf("409 body should mention 'already exists', got %s", rr.Body.String())
	}
}

// TestListPropertyIndexes covers GET /property-indexes including stats
// reflecting nodes inserted after the index was created.
func TestListPropertyIndexes(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Seed three indexes via the storage layer to keep the test focused
	// on the HTTP listing path (not the create path, which has its own
	// coverage above).
	for _, key := range []string{"alpha", "beta", "gamma"} {
		if err := server.graph.CreatePropertyIndex(key, storage.TypeString); err != nil {
			t.Fatalf("create index %s: %v", key, err)
		}
	}

	// Insert one node with the "alpha" property so the stats endpoint
	// has at least one non-zero row to assert against.
	if _, err := server.graph.CreateNode(
		[]string{"X"},
		map[string]storage.Value{"alpha": storage.StringValue("hello")},
	); err != nil {
		t.Fatalf("create seed node: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/property-indexes", nil)
	rr := httptest.NewRecorder()
	server.handlePropertyIndexes(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp PropertyIndexListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Count != 3 || len(resp.Indexes) != 3 {
		t.Errorf("expected 3 indexes, got count=%d len=%d", resp.Count, len(resp.Indexes))
	}

	// Indexes are returned sorted (ListPropertyIndexes uses sort.Strings).
	got := []string{resp.Indexes[0].PropertyKey, resp.Indexes[1].PropertyKey, resp.Indexes[2].PropertyKey}
	want := []string{"alpha", "beta", "gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index[%d]=%q, want %q", i, got[i], want[i])
		}
	}

	// alpha should reflect the seeded node.
	for _, idx := range resp.Indexes {
		if idx.PropertyKey == "alpha" && idx.TotalNodes != 1 {
			t.Errorf("alpha.total_nodes=%d, want 1", idx.TotalNodes)
		}
	}
}

// TestGetPropertyIndex covers GET /property-indexes/{key} for both the
// hit and miss paths.
func TestGetPropertyIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreatePropertyIndex("email", storage.TypeString); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tests := []struct {
		name         string
		key          string
		expectStatus int
	}{
		{"existing", "email", http.StatusOK},
		{"missing", "nonexistent", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/property-indexes/"+tt.key, nil)
			rr := httptest.NewRecorder()
			server.handlePropertyIndex(rr, r)
			if rr.Code != tt.expectStatus {
				t.Errorf("expected %d, got %d body=%s", tt.expectStatus, rr.Code, rr.Body.String())
			}
			if rr.Code == http.StatusOK {
				var resp PropertyIndexResponse
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("parse: %v", err)
				}
				if resp.PropertyKey != tt.key {
					t.Errorf("property_key=%q, want %q", resp.PropertyKey, tt.key)
				}
			}
		})
	}
}

// TestDeletePropertyIndex covers DELETE /property-indexes/{key} for both
// the hit (204 + index actually gone) and miss (404) paths.
func TestDeletePropertyIndex(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.graph.CreatePropertyIndex("to_drop", storage.TypeString); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("delete existing", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodDelete, "/property-indexes/to_drop", nil)
		rr := httptest.NewRecorder()
		server.handlePropertyIndex(rr, r)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d body=%s", rr.Code, rr.Body.String())
		}
		if server.graph.HasPropertyIndex("to_drop") {
			t.Error("index should be gone after DELETE but is still present")
		}
	})

	t.Run("delete missing", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodDelete, "/property-indexes/never_existed", nil)
		rr := httptest.NewRecorder()
		server.handlePropertyIndex(rr, r)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

// TestPropertyIndex_MethodRouting pins that unsupported methods return
// 405 — the same shape the vector-index handlers enforce.
func TestPropertyIndex_MethodRouting(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("collection PUT not allowed", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPut, "/property-indexes", nil)
		rr := httptest.NewRecorder()
		server.handlePropertyIndexes(rr, r)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rr.Code)
		}
	})

	t.Run("single POST not allowed", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/property-indexes/foo", nil)
		rr := httptest.NewRecorder()
		server.handlePropertyIndex(rr, r)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", rr.Code)
		}
	})
}

// TestParsePropertyIndexValueType covers the input-shape mapping in
// isolation so future additions (e.g., uuid type) get a regression net.
func TestParsePropertyIndexValueType(t *testing.T) {
	tests := []struct {
		in     string
		want   storage.ValueType
		wantOk bool
	}{
		{"", storage.TypeString, true},
		{"string", storage.TypeString, true},
		{"String", storage.TypeString, true}, // case-insensitive
		{"  int  ", storage.TypeInt, true},   // trimmed
		{"integer", storage.TypeInt, true},   // alias
		{"float", storage.TypeFloat, true},
		{"double", storage.TypeFloat, true}, // alias
		{"bool", storage.TypeBool, true},
		{"boolean", storage.TypeBool, true},
		{"timestamp", storage.TypeTimestamp, true},
		{"time", storage.TypeTimestamp, true},
		{"bytes", storage.TypeBytes, true},
		{"vector", 0, false}, // not supported for property indexes
		{"uuid", 0, false},
		{"garbage", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parsePropertyIndexValueType(tt.in)
			if ok != tt.wantOk {
				t.Fatalf("ok=%v, want %v", ok, tt.wantOk)
			}
			if ok && got != tt.want {
				t.Errorf("got=%v, want %v", got, tt.want)
			}
		})
	}
}
