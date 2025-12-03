package graphql

import (
	"encoding/json"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestExecuteQuerySingleNode tests querying a single node by ID
func TestExecuteQuerySingleNode(t *testing.T) {
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
	node, _ := gs.CreateNode(
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

	// Execute query
	query := `{
		person(id: "1") {
			id
			labels
			properties
		}
	}`

	result := ExecuteQuery(query, schema)
	if result.HasErrors() {
		t.Fatalf("Query execution failed: %v", result.Errors)
	}

	// Verify result contains the node data
	data := result.Data.(map[string]any)
	person := data["person"]
	if person == nil {
		t.Fatal("Query result missing 'person' field")
	}

	personData := person.(map[string]any)
	if personData["id"] == nil {
		t.Error("Person data missing 'id' field")
	}

	// Verify ID matches
	idStr := personData["id"].(string)
	if idStr != "1" {
		t.Errorf("Person id = %s, want 1", idStr)
	}

	_ = node // Use node to avoid unused variable error
}

// TestExecuteQueryMultipleNodes tests querying all nodes with a label
func TestExecuteQueryMultipleNodes(t *testing.T) {
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

	// Create multiple nodes
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Alice")},
	)

	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Bob")},
	)

	gs.CreateNode(
		[]string{"Company"},
		map[string]storage.Value{"name": storage.StringValue("TechCorp")},
	)

	// Generate schema
	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Execute query for all persons
	query := `{
		persons {
			id
			labels
		}
	}`

	result := ExecuteQuery(query, schema)
	if result.HasErrors() {
		t.Fatalf("Query execution failed: %v", result.Errors)
	}

	// Verify result contains multiple persons
	data := result.Data.(map[string]any)
	persons := data["persons"]
	if persons == nil {
		t.Fatal("Query result missing 'persons' field")
	}

	personsList := persons.([]any)
	if len(personsList) != 2 {
		t.Errorf("Expected 2 persons, got %d", len(personsList))
	}
}

// TestExecuteQueryWithProperties tests querying node properties
func TestExecuteQueryWithProperties(t *testing.T) {
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

	// Create node with properties
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{
			"name":  storage.StringValue("Alice"),
			"age":   storage.IntValue(30),
			"email": storage.StringValue("alice@example.com"),
		},
	)

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Query with properties
	query := `{
		person(id: "1") {
			id
			labels
			properties
		}
	}`

	result := ExecuteQuery(query, schema)
	if result.HasErrors() {
		t.Fatalf("Query execution failed: %v", result.Errors)
	}

	// Verify properties are returned
	data := result.Data.(map[string]any)
	person := data["person"].(map[string]any)
	properties := person["properties"]

	if properties == nil {
		t.Fatal("Person data missing 'properties' field")
	}

	// Properties should be a JSON string
	propsStr := properties.(string)
	if propsStr == "" {
		t.Error("Properties string is empty")
	}

	// Verify it's valid JSON
	var propsMap map[string]any
	if err := json.Unmarshal([]byte(propsStr), &propsMap); err != nil {
		t.Errorf("Properties not valid JSON: %v", err)
	}
}

// TestExecuteQueryNotFound tests querying a non-existent node
func TestExecuteQueryNotFound(t *testing.T) {
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

	// Create one node
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Alice")},
	)

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Query for non-existent node
	query := `{
		person(id: "999") {
			id
			labels
		}
	}`

	result := ExecuteQuery(query, schema)

	// Should not error, but return null
	if result.HasErrors() {
		t.Logf("Query has errors (expected): %v", result.Errors)
	}

	// Result should be null or empty
	data := result.Data.(map[string]any)
	person := data["person"]
	if person != nil {
		t.Errorf("Expected null for non-existent node, got %v", person)
	}
}

// TestExecuteQueryInvalidSyntax tests handling of invalid GraphQL syntax
func TestExecuteQueryInvalidSyntax(t *testing.T) {
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

	// Invalid query syntax
	query := `{
		person(id: "1"
	}`

	result := ExecuteQuery(query, schema)

	// Should have errors
	if !result.HasErrors() {
		t.Error("Expected errors for invalid syntax, got none")
	}
}

// TestExecuteQueryEmptyDatabase tests querying an empty database
func TestExecuteQueryEmptyDatabase(t *testing.T) {
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

	// Generate schema with no data
	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Try to query - should not crash
	query := `{
		__schema {
			queryType {
				name
			}
		}
	}`

	result := ExecuteQuery(query, schema)
	if result.HasErrors() {
		t.Fatalf("Query execution failed: %v", result.Errors)
	}

	// Should return schema introspection data
	data := result.Data.(map[string]any)
	schemaData := data["__schema"]
	if schemaData == nil {
		t.Error("Schema introspection failed")
	}
}
