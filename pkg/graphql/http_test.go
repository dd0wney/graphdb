package graphql

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestGraphQLHTTPHandler tests the HTTP handler for GraphQL queries
func TestGraphQLHTTPHandler(t *testing.T) {
	// Setup storage with test data
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create test node
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{
			"name": storage.StringValue("Alice"),
			"age":  storage.IntValue(30),
		},
	)

	// Generate schema
	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Create HTTP handler
	handler := NewGraphQLHandler(schema)

	// Create test request
	queryReq := GraphQLRequest{
		Query: `{
			person(id: "1") {
				id
				labels
			}
		}`,
	}

	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Execute request
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Verify response
	if rr.Code != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	// Parse response
	var response GraphQLResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify no errors
	if len(response.Errors) > 0 {
		t.Errorf("Response has errors: %v", response.Errors)
	}

	// Verify data exists
	if response.Data == nil {
		t.Error("Response data is nil")
	}
}

// TestGraphQLHTTPHandlerWithVariables tests queries with variables
func TestGraphQLHTTPHandlerWithVariables(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Bob")},
	)

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	handler := NewGraphQLHandler(schema)

	// Query with variables
	queryReq := GraphQLRequest{
		Query: `query GetPerson($id: ID!) {
			person(id: $id) {
				id
				labels
			}
		}`,
		Variables: map[string]any{
			"id": "1",
		},
	}

	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	var response GraphQLResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Errors) > 0 {
		t.Errorf("Response has errors: %v", response.Errors)
	}
}

// TestGraphQLHTTPHandlerInvalidJSON tests handling of invalid JSON
func TestGraphQLHTTPHandlerInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	handler := NewGraphQLHandler(schema)

	// Invalid JSON
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should return 400 Bad Request
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Handler returned wrong status code for invalid JSON: got %v want %v", rr.Code, http.StatusBadRequest)
	}
}

// TestGraphQLHTTPHandlerMethodNotAllowed tests non-POST methods
func TestGraphQLHTTPHandlerMethodNotAllowed(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	handler := NewGraphQLHandler(schema)

	// Try GET request
	req := httptest.NewRequest("GET", "/graphql", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should return 405 Method Not Allowed
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Handler returned wrong status code for GET: got %v want %v", rr.Code, http.StatusMethodNotAllowed)
	}
}

// TestGraphQLHTTPHandlerCORS tests CORS headers
func TestGraphQLHTTPHandlerCORS(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	handler := NewGraphQLHandler(schema)

	queryReq := GraphQLRequest{
		Query: `{ health }`,
	}

	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Verify CORS headers are set
	if rr.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("CORS header 'Access-Control-Allow-Origin' not set")
	}

	if rr.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("CORS header 'Access-Control-Allow-Methods' not set")
	}
}

// TestGraphQLHTTPHandlerQueryErrors tests GraphQL query errors
func TestGraphQLHTTPHandlerQueryErrors(t *testing.T) {
	tmpDir := t.TempDir()
	config := storage.StorageConfig{
		DataDir:        tmpDir,
		BulkImportMode: true,
	}

	gs, err := storage.NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	handler := NewGraphQLHandler(schema)

	// Invalid query syntax
	queryReq := GraphQLRequest{
		Query: `{
			person(id: "1"
		}`,
	}

	body, _ := json.Marshal(queryReq)
	req := httptest.NewRequest("POST", "/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Should return 200 but with errors in response
	if rr.Code != http.StatusOK {
		t.Errorf("Handler returned wrong status code: got %v want %v", rr.Code, http.StatusOK)
	}

	var response GraphQLResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have errors
	if len(response.Errors) == 0 {
		t.Error("Expected query errors, got none")
	}
}
