package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestSortNodesByPropertyAscending tests sorting nodes in ascending order
func TestSortNodesByPropertyAscending(t *testing.T) {
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

	// Create Person nodes with different ages
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(20),
	})

	schema, err := GenerateSchemaWithSorting(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithSorting() error = %v", err)
	}

	query := `
		{
			persons(orderBy: {field: "age", direction: "ASC"}) {
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

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	if len(persons) != 3 {
		t.Fatalf("Expected 3 persons, got %d", len(persons))
	}

	// Verify sorted order (age: 20, 25, 30)
	props := persons[0].(map[string]any)["properties"].(string)
	if !contains(props, "Bob") || !contains(props, "20") {
		t.Errorf("Expected first person to be Bob (age 20), got %s", props)
	}

	props = persons[2].(map[string]any)["properties"].(string)
	if !contains(props, "Alice") || !contains(props, "30") {
		t.Errorf("Expected last person to be Alice (age 30), got %s", props)
	}
}

// TestSortNodesByPropertyDescending tests sorting nodes in descending order
func TestSortNodesByPropertyDescending(t *testing.T) {
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

	// Create Person nodes with different names
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	schema, err := GenerateSchemaWithSorting(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithSorting() error = %v", err)
	}

	query := `
		{
			persons(orderBy: {field: "name", direction: "DESC"}) {
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

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	if len(persons) != 3 {
		t.Fatalf("Expected 3 persons, got %d", len(persons))
	}

	// Verify sorted order (name: Charlie, Bob, Alice)
	props := persons[0].(map[string]any)["properties"].(string)
	if !contains(props, "Charlie") {
		t.Errorf("Expected first person to be Charlie, got %s", props)
	}

	props = persons[2].(map[string]any)["properties"].(string)
	if !contains(props, "Alice") {
		t.Errorf("Expected last person to be Alice, got %s", props)
	}
}

// TestSortWithPagination tests combining sorting with offset pagination
func TestSortWithPagination(t *testing.T) {
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

	// Create 5 Person nodes with different scores
	for i := 1; i <= 5; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name":  storage.StringValue("Person" + string(rune('0'+i))),
			"score": storage.IntValue(int64(i * 10)),
		})
	}

	schema, err := GenerateSchemaWithSorting(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithSorting() error = %v", err)
	}

	// Get top 3 scores (sorted DESC)
	query := `
		{
			persons(orderBy: {field: "score", direction: "DESC"}, limit: 3) {
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

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	if len(persons) != 3 {
		t.Errorf("Expected 3 persons with limit, got %d", len(persons))
	}

	// Verify first is highest score (50)
	props := persons[0].(map[string]any)["properties"].(string)
	if !contains(props, "50") {
		t.Errorf("Expected first person to have score 50, got %s", props)
	}
}

// TestSortWithCursorPagination tests combining sorting with cursor pagination
func TestSortWithCursorPagination(t *testing.T) {
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

	// Create 10 Person nodes with different scores
	for i := 1; i <= 10; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name":  storage.StringValue("Person" + string(rune('0'+i))),
			"score": storage.IntValue(int64(i * 10)),
		})
	}

	schema, err := GenerateSchemaWithSorting(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithSorting() error = %v", err)
	}

	// Get top 5 scores with cursor pagination
	query := `
		{
			personsConnection(orderBy: {field: "score", direction: "DESC"}, first: 5) {
				edges {
					node {
						properties
					}
				}
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

	data := result.Data.(map[string]any)
	connection := data["personsConnection"].(map[string]any)
	edges := connection["edges"].([]any)

	if len(edges) != 5 {
		t.Errorf("Expected 5 edges, got %d", len(edges))
	}

	// Verify first is highest score (100)
	firstEdge := edges[0].(map[string]any)
	node := firstEdge["node"].(map[string]any)
	props := node["properties"].(string)
	if !contains(props, "100") {
		t.Errorf("Expected first node to have score 100, got %s", props)
	}
}

// TestSortEdgesByWeight tests sorting edges by weight
func TestSortEdgesByWeight(t *testing.T) {
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

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	// Create edges with different weights
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 3.0)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 2.0)

	schema, err := GenerateSchemaWithSorting(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithSorting() error = %v", err)
	}

	query := `
		{
			edges(orderBy: {field: "weight", direction: "ASC"}) {
				weight
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

	data := result.Data.(map[string]any)
	edges := data["edges"].([]any)

	if len(edges) != 3 {
		t.Fatalf("Expected 3 edges, got %d", len(edges))
	}

	// Verify sorted order (weight: 1.0, 2.0, 3.0)
	weight1 := edges[0].(map[string]any)["weight"].(float64)
	weight3 := edges[2].(map[string]any)["weight"].(float64)

	if weight1 != 1.0 {
		t.Errorf("Expected first edge weight to be 1.0, got %f", weight1)
	}
	if weight3 != 3.0 {
		t.Errorf("Expected last edge weight to be 3.0, got %f", weight3)
	}
}

// TestSortWithoutOrderBy tests that queries without orderBy work normally
func TestSortWithoutOrderBy(t *testing.T) {
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

	// Create nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	schema, err := GenerateSchemaWithSorting(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithSorting() error = %v", err)
	}

	// Query without orderBy (should work normally)
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

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	if len(persons) != 2 {
		t.Errorf("Expected 2 persons, got %d", len(persons))
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
