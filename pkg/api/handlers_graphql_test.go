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

// TestGraphQL_ComplexityValidation tests that complex queries are rejected
func TestGraphQL_ComplexityValidation(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some test data and regenerate schema to include the new labels
	for i := 0; i < 5; i++ {
		server.graph.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	// Regenerate schema to pick up new labels
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schema/regenerate", nil)
	rr := httptest.NewRecorder()
	server.handleSchemaRegenerate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to regenerate schema: %s", rr.Body.String())
	}

	tests := []struct {
		name        string
		query       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Health query passes",
			query:       `{ health }`,
			expectError: false,
		},
		{
			name:        "Simple node query passes",
			query:       `{ persons { id labels } }`,
			expectError: false,
		},
		{
			name: "Complex nested query rejected",
			// This creates a very complex query that exceeds the default complexity limit
			// Each list field multiplies complexity, so deeply nested edges blow up fast
			query: `{
				persons {
					id
					outgoingEdges {
						id
						fromNodeId
						toNodeId
					}
				}
			}`,
			expectError: true, // Complexity: 100 * 100 * 3 = 30000 > 5000 default limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := map[string]any{
				"query": tt.query,
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleGraphQL(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
			}

			var response map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			hasErrors := response["errors"] != nil
			if tt.expectError && !hasErrors {
				t.Error("Expected complexity error but got none")
			}
			if !tt.expectError && hasErrors {
				errors := response["errors"]
				t.Errorf("Expected no error but got: %v", errors)
			}
		})
	}
}

// TestGraphQL_MethodNotAllowed tests that non-POST methods are rejected
func TestGraphQL_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	methods := []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/graphql", nil)
			rr := httptest.NewRecorder()

			server.handleGraphQL(rr, req)

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d for %s, got %d",
					http.StatusMethodNotAllowed, method, rr.Code)
			}
		})
	}
}

// TestGraphQL_InvalidRequest tests handling of invalid request bodies
func TestGraphQL_InvalidRequest(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		body       string
		expectCode int
	}{
		{
			name:       "Invalid JSON",
			body:       `{"query": "{ health }"`,
			expectCode: http.StatusBadRequest,
		},
		{
			name:       "Empty body",
			body:       "",
			expectCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			server.handleGraphQL(rr, req)

			if rr.Code != tt.expectCode {
				t.Errorf("Expected status %d, got %d", tt.expectCode, rr.Code)
			}
		})
	}
}

// TestGraphQL_LimitsEnforced tests that result limits are enforced
func TestGraphQL_LimitsEnforced(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create more nodes than the default limit
	for i := 0; i < 150; i++ {
		server.graph.CreateNode([]string{"Item"}, map[string]storage.Value{
			"name": storage.StringValue("Item"),
		})
	}

	// Regenerate schema to pick up the new "Item" label
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schema/regenerate", nil)
	rr := httptest.NewRecorder()
	server.handleSchemaRegenerate(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("Failed to regenerate schema: %s", rr.Body.String())
	}

	// Query without explicit limit - should use default (100)
	reqBody := map[string]any{
		"query": `{ items { id } }`,
	}
	body, _ := json.Marshal(reqBody)

	gqlReq := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	gqlReq.Header.Set("Content-Type", "application/json")

	gqlRR := httptest.NewRecorder()
	server.handleGraphQL(gqlRR, gqlReq)

	if gqlRR.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, gqlRR.Code, gqlRR.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(gqlRR.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("Response missing data field: %v", response)
	}

	items, ok := data["items"].([]any)
	if !ok {
		t.Fatalf("Response missing items field: %v", data)
	}

	// Default limit should cap results at 100
	if len(items) > 100 {
		t.Errorf("Expected max 100 items (default limit), got %d", len(items))
	}

	t.Logf("✓ Limits enforced: returned %d items (max 100)", len(items))
}

// TestSchemaRegenerate tests the admin schema regeneration endpoint
func TestSchemaRegenerate(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		method       string
		expectStatus int
	}{
		{
			name:         "POST regenerates schema",
			method:       http.MethodPost,
			expectStatus: http.StatusOK,
		},
		{
			name:         "GET not allowed",
			method:       http.MethodGet,
			expectStatus: http.StatusMethodNotAllowed,
		},
		{
			name:         "PUT not allowed",
			method:       http.MethodPut,
			expectStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/v1/schema/regenerate", nil)
			rr := httptest.NewRecorder()

			server.handleSchemaRegenerate(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			if tt.expectStatus == http.StatusOK {
				var response map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}

				if response["status"] != "success" {
					t.Errorf("Expected status 'success', got %v", response["status"])
				}

				t.Logf("✓ Schema regenerated: %v", response["message"])
			}
		})
	}
}

// TestSchemaRegenerate_NewLabels tests that new labels appear after regeneration
func TestSchemaRegenerate_NewLabels(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a node with a new label
	_, err := server.graph.CreateNode([]string{"NewLabelType"}, map[string]storage.Value{
		"name": storage.StringValue("Test"),
	})
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Regenerate schema
	req := httptest.NewRequest(http.MethodPost, "/api/v1/schema/regenerate", nil)
	rr := httptest.NewRecorder()
	server.handleSchemaRegenerate(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Schema regeneration failed: %s", rr.Body.String())
	}

	// Query the new label type
	reqBody := map[string]any{
		"query": `{ newlabeltypes { id labels } }`,
	}
	body, _ := json.Marshal(reqBody)

	req = httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	server.handleGraphQL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GraphQL query failed: %s", rr.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have data without errors
	if response["errors"] != nil {
		t.Errorf("Expected no errors querying new label, got: %v", response["errors"])
	}

	t.Logf("✓ New label type 'NewLabelType' accessible after schema regeneration")
}

// TestOpenAPISpec tests the OpenAPI specification endpoint
func TestOpenAPISpec(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name         string
		path         string
		accept       string
		expectStatus int
		expectType   string
	}{
		{
			name:         "YAML endpoint",
			path:         "/api/docs/openapi.yaml",
			accept:       "",
			expectStatus: http.StatusOK,
			expectType:   "application/x-yaml",
		},
		{
			name:         "JSON endpoint",
			path:         "/api/docs/openapi.json",
			accept:       "",
			expectStatus: http.StatusOK,
			expectType:   "application/json",
		},
		{
			name:         "Accept JSON header",
			path:         "/api/docs/openapi.yaml",
			accept:       "application/json",
			expectStatus: http.StatusOK,
			expectType:   "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.accept != "" {
				req.Header.Set("Accept", tt.accept)
			}

			rr := httptest.NewRecorder()
			server.handleOpenAPISpec(rr, req)

			// May return 404 if spec file not found in test environment
			if rr.Code == http.StatusNotFound {
				t.Skip("OpenAPI spec file not found in test environment")
			}

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s",
					tt.expectStatus, rr.Code, rr.Body.String())
			}

			contentType := rr.Header().Get("Content-Type")
			if !strings.Contains(contentType, tt.expectType) {
				t.Errorf("Expected Content-Type %s, got %s", tt.expectType, contentType)
			}

			t.Logf("✓ OpenAPI spec served as %s", contentType)
		})
	}
}

// TestOpenAPISpec_MethodNotAllowed tests that non-GET methods are rejected
func TestOpenAPISpec_MethodNotAllowed(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/docs/openapi.yaml", nil)
			rr := httptest.NewRecorder()

			server.handleOpenAPISpec(rr, req)

			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("Expected status %d for %s, got %d",
					http.StatusMethodNotAllowed, method, rr.Code)
			}
		})
	}
}
