package graphql

import (
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestShallowQueryAllowed tests that queries within depth limit are allowed
func TestShallowQueryAllowed(t *testing.T) {
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
		"age":  storage.IntValue(30),
	})

	schema, err := GenerateSchemaWithDepthLimit(gs, 5)
	if err != nil {
		t.Fatalf("GenerateSchemaWithDepthLimit() error = %v", err)
	}

	query := `{ persons { id properties } }`

	result := ExecuteWithDepthLimit(schema, query, 5, nil)

	if result.HasErrors() {
		t.Fatalf("Expected shallow query to succeed, got errors: %v", result.Errors)
	}
}

// TestDeepQueryRejected tests that queries exceeding depth limit are rejected
func TestDeepQueryRejected(t *testing.T) {
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

	// Create test data
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	schema, err := GenerateSchemaWithDepthLimit(gs, 1)
	if err != nil {
		t.Fatalf("GenerateSchemaWithDepthLimit() error = %v", err)
	}

	// Query at depth 2: persons -> properties (exceeds limit of 1)
	query := `{ persons { properties } }`

	result := ExecuteWithDepthLimit(schema, query, 1, nil)

	if !result.HasErrors() {
		t.Fatal("Expected deep query to be rejected, but it succeeded")
	}

	errorMsg := result.Errors[0].Message
	if !strings.Contains(strings.ToLower(errorMsg), "depth") {
		t.Errorf("Expected error message to mention depth, got: %s", errorMsg)
	}
}

// TestQueryAtExactDepthLimit tests queries at exact depth limit are allowed
func TestQueryAtExactDepthLimit(t *testing.T) {
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

	schema, err := GenerateSchemaWithDepthLimit(gs, 2)
	if err != nil {
		t.Fatalf("GenerateSchemaWithDepthLimit() error = %v", err)
	}

	// Query at depth 2: persons -> properties (exactly at limit)
	query := `{ persons { properties } }`

	result := ExecuteWithDepthLimit(schema, query, 2, nil)

	if result.HasErrors() {
		t.Fatalf("Expected query at exact depth limit to succeed, got errors: %v", result.Errors)
	}
}

// TestDepthLimitConfigValidation tests depth limit configuration validation
func TestDepthLimitConfigValidation(t *testing.T) {
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

	_, err = GenerateSchemaWithDepthLimit(gs, 0)
	if err == nil {
		t.Error("Expected error with depth limit 0, got nil")
	}

	_, err = GenerateSchemaWithDepthLimit(gs, -1)
	if err == nil {
		t.Error("Expected error with negative depth limit, got nil")
	}

	_, err = GenerateSchemaWithDepthLimit(gs, 10)
	if err != nil {
		t.Errorf("Expected valid depth limit to succeed, got error: %v", err)
	}
}
