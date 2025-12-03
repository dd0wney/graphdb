package graphql

import (
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestSimpleQueryComplexity tests complexity calculation for simple queries
func TestSimpleQueryComplexity(t *testing.T) {
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

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// Simple query: single node by ID, 2 fields
	// Expected complexity: 1 * 2 = 2
	query := `{ person(id: "1") { id properties } }`

	result := ExecuteWithComplexity(schema, query, 100, nil)

	if result.HasErrors() {
		t.Fatalf("Expected simple query to succeed, got errors: %v", result.Errors)
	}
}

// TestListQueryComplexity tests complexity calculation for list queries
func TestListQueryComplexity(t *testing.T) {
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

	for i := 0; i < 10; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:     100,
		ListMultiplier:    10, // Default list cost
		DefaultListLimit:  100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// List query with limit: persons(limit: 5) with 1 field
	// Expected complexity: 5 * 1 = 5
	query := `{ persons(limit: 5) { id } }`

	result := ExecuteWithComplexity(schema, query, 100, nil)

	if result.HasErrors() {
		t.Fatalf("Expected list query with limit to succeed, got errors: %v", result.Errors)
	}
}

// TestComplexQueryRejected tests that expensive queries are rejected
func TestComplexQueryRejected(t *testing.T) {
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

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    10, // Very low limit
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// List query without limit: persons with 2 fields
	// Expected complexity: 100 * 2 = 200 (exceeds limit of 10)
	query := `{ persons { id properties } }`

	result := ExecuteWithComplexity(schema, query, 10, nil)

	if !result.HasErrors() {
		t.Fatal("Expected complex query to be rejected, but it succeeded")
	}

	errorMsg := result.Errors[0].Message
	if !strings.Contains(strings.ToLower(errorMsg), "complexity") {
		t.Errorf("Expected error message to mention complexity, got: %s", errorMsg)
	}
}

// TestNestedQueryComplexity tests complexity calculation for nested queries
func TestNestedQueryComplexity(t *testing.T) {
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

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    1000,
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// List query with limit: persons(limit: 2) { id }
	// Expected complexity: 2 * 1 = 2
	query := `{ persons(limit: 2) { id } }`

	result := ExecuteWithComplexity(schema, query, 1000, nil)

	if result.HasErrors() {
		t.Fatalf("Expected nested query within limit to succeed, got errors: %v", result.Errors)
	}
}

// TestComplexityWithFiltering tests that filtering doesn't reduce calculated complexity
func TestComplexityWithFiltering(t *testing.T) {
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

	for i := 0; i < 50; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(int64(i)),
		})
	}

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    200,
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// Query with filter and limit: persons(where: {age: {gt: 40}}, limit: 10)
	// Complexity should be based on limit, not filter result
	// Expected complexity: 10 * 1 = 10
	query := `query($where: WhereInput, $limit: Int) { persons(where: $where, limit: $limit) { id } }`

	result := ExecuteWithComplexity(schema, query, 200, map[string]any{
		"where": map[string]any{
			"age": map[string]any{
				"gt": 40,
			},
		},
		"limit": 10,
	})

	if result.HasErrors() {
		t.Fatalf("Expected filtered query to succeed, got errors: %v", result.Errors)
	}
}

// TestComplexityAtExactLimit tests queries at exact complexity limit
func TestComplexityAtExactLimit(t *testing.T) {
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

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    10, // Exact limit
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// Query with complexity exactly 10: persons(limit: 10) { id }
	// Complexity: 10 * 1 = 10
	query := `{ persons(limit: 10) { id } }`

	result := ExecuteWithComplexity(schema, query, 10, nil)

	if result.HasErrors() {
		t.Fatalf("Expected query at exact complexity limit to succeed, got errors: %v", result.Errors)
	}
}

// TestComplexityConfigValidation tests complexity configuration validation
func TestComplexityConfigValidation(t *testing.T) {
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

	// Test: zero max complexity (invalid)
	_, err = GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity: 0,
	})
	if err == nil {
		t.Error("Expected error with max complexity 0, got nil")
	}

	// Test: negative max complexity (invalid)
	_, err = GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity: -1,
	})
	if err == nil {
		t.Error("Expected error with negative max complexity, got nil")
	}

	// Test: valid config
	_, err = GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    1000,
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Errorf("Expected valid config to succeed, got error: %v", err)
	}
}

// TestMultipleQueriesComplexity tests complexity for multiple queries in one request
func TestMultipleQueriesComplexity(t *testing.T) {
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

	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    100,
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// Multiple queries - complexity should be sum of both
	// query1: 5 * 1 = 5, query2: 3 * 1 = 3, total = 8
	query := `{
		query1: persons(limit: 5) { id }
		query2: persons(limit: 3) { id }
	}`

	result := ExecuteWithComplexity(schema, query, 100, nil)

	if result.HasErrors() {
		t.Fatalf("Expected multiple queries within limit to succeed, got errors: %v", result.Errors)
	}
}

// TestIntrospectionQueryComplexity tests that introspection queries have low complexity
func TestIntrospectionQueryComplexity(t *testing.T) {
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

	schema, err := GenerateSchemaWithComplexity(gs, &ComplexityConfig{
		MaxComplexity:    50, // Low limit
		ListMultiplier:   10,
		DefaultListLimit: 100,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithComplexity() error = %v", err)
	}

	// Introspection query should have special low complexity
	query := `{ __schema { types { name } } }`

	result := ExecuteWithComplexity(schema, query, 50, nil)

	if result.HasErrors() {
		t.Fatalf("Expected introspection query to succeed, got errors: %v", result.Errors)
	}
}
