package graphql

import (
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestDefaultLimitApplied tests that default limit is applied when no limit specified
func TestDefaultLimitApplied(t *testing.T) {
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

	// Create 200 nodes (more than default limit of 100)
	for i := 0; i < 200; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
			"id":   storage.IntValue(int64(i)),
		})
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 100,
		MaxLimit:     10000,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Query without specifying limit - should apply default of 100
	query := `
		{
			persons {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	persons := data["persons"].([]interface{})

	// Should return only 100 results (default limit)
	if len(persons) != 100 {
		t.Errorf("Expected default limit of 100 to be applied, got %d results", len(persons))
	}
}

// TestMaxLimitEnforced tests that max limit cannot be exceeded
func TestMaxLimitEnforced(t *testing.T) {
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

	// Create 100 nodes
	for i := 0; i < 100; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 25,
		MaxLimit:     50, // Set max limit to 50
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Try to query with limit exceeding max (100 > 50)
	query := `
		query($limit: Int) {
			persons(limit: $limit) {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]interface{}{
			"limit": 100, // Request 100, should be capped at 50
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	persons := data["persons"].([]interface{})

	// Should cap at max limit of 50
	if len(persons) != 50 {
		t.Errorf("Expected max limit of 50 to be enforced, got %d results", len(persons))
	}
}

// TestExplicitLimitWithinMax tests that explicit limit within max is respected
func TestExplicitLimitWithinMax(t *testing.T) {
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

	// Create 100 nodes
	for i := 0; i < 100; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 100,
		MaxLimit:     1000,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Query with explicit limit of 25 (within max)
	query := `
		query($limit: Int) {
			persons(limit: $limit) {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]interface{}{
			"limit": 25,
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	persons := data["persons"].([]interface{})

	// Should respect explicit limit of 25
	if len(persons) != 25 {
		t.Errorf("Expected explicit limit of 25, got %d results", len(persons))
	}
}

// TestLimitsOnEdgeQueries tests limits apply to edge queries
func TestLimitsOnEdgeQueries(t *testing.T) {
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

	// Create nodes and 150 edges
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	for i := 0; i < 150; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{}, 1.0)
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 50,
		MaxLimit:     10000,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Query edges without limit - should apply default
	query := `
		{
			edges {
				type
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	edges := data["edges"].([]interface{})

	// Should apply default limit of 50
	if len(edges) != 50 {
		t.Errorf("Expected default limit of 50 on edges, got %d results", len(edges))
	}
}

// TestLimitsWithFiltering tests limits work correctly with filtering
func TestLimitsWithFiltering(t *testing.T) {
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

	// Create 200 nodes, 100 with age > 30
	for i := 0; i < 200; i++ {
		age := int64(20)
		if i >= 100 {
			age = 35
		}
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
			"age":  storage.IntValue(age),
		})
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 50,
		MaxLimit:     10000,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Query with filter - should still apply default limit
	query := `
		query($where: WhereInput) {
			persons(where: $where) {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]interface{}{
			"where": map[string]interface{}{
				"age": map[string]interface{}{
					"gt": 30,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	persons := data["persons"].([]interface{})

	// Should apply default limit of 50 even with filtering
	if len(persons) != 50 {
		t.Errorf("Expected default limit of 50 with filtering, got %d results", len(persons))
	}
}

// TestZeroLimitReturnsEmpty tests that limit of 0 returns empty results
func TestZeroLimitReturnsEmpty(t *testing.T) {
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

	// Create some nodes
	for i := 0; i < 50; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 100,
		MaxLimit:     10000,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Query with limit = 0
	query := `
		query($limit: Int) {
			persons(limit: $limit) {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]interface{}{
			"limit": 0,
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	persons := data["persons"].([]interface{})

	// Should return empty array
	if len(persons) != 0 {
		t.Errorf("Expected 0 results with limit=0, got %d results", len(persons))
	}
}

// TestNegativeLimitTreatedAsUnlimited tests negative limit behavior
func TestNegativeLimitTreatedAsDefault(t *testing.T) {
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

	// Create 150 nodes
	for i := 0; i < 150; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 100,
		MaxLimit:     10000,
	})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits() error = %v", err)
	}

	// Query with negative limit - should treat as no limit specified (use default)
	query := `
		query($limit: Int) {
			persons(limit: $limit) {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]interface{}{
			"limit": -1,
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]interface{})
	persons := data["persons"].([]interface{})

	// Should apply default limit
	if len(persons) != 100 {
		t.Errorf("Expected default limit of 100 with negative limit, got %d results", len(persons))
	}
}

// TestLimitConfigValidation tests that invalid config is rejected
func TestLimitConfigValidation(t *testing.T) {
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

	// Test: default limit > max limit (invalid)
	_, err = GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 1000,
		MaxLimit:     100,
	})

	if err == nil {
		t.Error("Expected error when default limit > max limit, got nil")
	}

	if err != nil && !strings.Contains(err.Error(), "default limit") {
		t.Errorf("Expected error message about default limit, got: %v", err)
	}

	// Test: zero max limit (invalid)
	_, err = GenerateSchemaWithLimits(gs, &LimitConfig{
		DefaultLimit: 100,
		MaxLimit:     0,
	})

	if err == nil {
		t.Error("Expected error when max limit is 0, got nil")
	}
}
