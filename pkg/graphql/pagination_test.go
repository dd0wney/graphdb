package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestNodeQueryPagination tests pagination for node queries
func TestNodeQueryPagination(t *testing.T) {
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

	// Create 10 Person nodes
	for i := 1; i <= 10; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person" + string(rune('0'+i))),
			"id":   storage.IntValue(int64(i)),
		})
	}

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Test limit only
	query := `
		{
			persons(limit: 5) {
				id
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

	if len(persons) != 5 {
		t.Errorf("Expected 5 persons with limit, got %d", len(persons))
	}
}

// TestNodeQueryPaginationWithOffset tests pagination with offset
func TestNodeQueryPaginationWithOffset(t *testing.T) {
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

	// Create 10 Person nodes
	for i := 1; i <= 10; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person" + string(rune('0'+i))),
			"id":   storage.IntValue(int64(i)),
		})
	}

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Test offset and limit
	query := `
		{
			persons(limit: 3, offset: 5) {
				id
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

	if len(persons) != 3 {
		t.Errorf("Expected 3 persons with offset and limit, got %d", len(persons))
	}
}

// TestNodeQueryPaginationDefaultBehavior tests pagination without arguments
func TestNodeQueryPaginationDefaultBehavior(t *testing.T) {
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

	// Create 5 Person nodes
	for i := 1; i <= 5; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person" + string(rune('0'+i))),
		})
	}

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Test without pagination arguments (should return all)
	query := `
		{
			persons {
				id
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

	if len(persons) != 5 {
		t.Errorf("Expected all 5 persons without pagination args, got %d", len(persons))
	}
}

// TestEdgeQueryPagination tests pagination for edge queries
func TestEdgeQueryPagination(t *testing.T) {
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

	// Create 10 edges
	for i := 1; i <= 10; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
			"strength": storage.IntValue(int64(i)),
		}, float64(i))
	}

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Test limit only
	query := `
		{
			edges(limit: 5) {
				id
				type
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

	data := result.Data.(map[string]interface{})
	edges := data["edges"].([]interface{})

	if len(edges) != 5 {
		t.Errorf("Expected 5 edges with limit, got %d", len(edges))
	}
}

// TestEdgeQueryPaginationWithOffset tests pagination for edges with offset
func TestEdgeQueryPaginationWithOffset(t *testing.T) {
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

	// Create 10 edges
	for i := 1; i <= 10; i++ {
		gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
			"strength": storage.IntValue(int64(i)),
		}, float64(i))
	}

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Test offset and limit
	query := `
		{
			edges(limit: 3, offset: 7) {
				id
				type
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

	data := result.Data.(map[string]interface{})
	edges := data["edges"].([]interface{})

	// With 10 edges total, offset 7 and limit 3 should return 3 edges (edges 8, 9, 10)
	if len(edges) != 3 {
		t.Errorf("Expected 3 edges with offset 7 and limit 3, got %d", len(edges))
	}
}

// TestPaginationEdgeCases tests edge cases for pagination
func TestPaginationEdgeCases(t *testing.T) {
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

	// Create 5 Person nodes
	for i := 1; i <= 5; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person" + string(rune('0'+i))),
		})
	}

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	tests := []struct {
		name          string
		query         string
		expectedCount int
	}{
		{
			name: "offset beyond total",
			query: `
				{
					persons(offset: 10) {
						id
					}
				}
			`,
			expectedCount: 0,
		},
		{
			name: "limit larger than total",
			query: `
				{
					persons(limit: 100) {
						id
					}
				}
			`,
			expectedCount: 5,
		},
		{
			name: "limit 0",
			query: `
				{
					persons(limit: 0) {
						id
					}
				}
			`,
			expectedCount: 0,
		},
		{
			name: "offset + limit exceeds total",
			query: `
				{
					persons(offset: 3, limit: 5) {
						id
					}
				}
			`,
			expectedCount: 2, // Only 2 items remain after offset 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := graphql.Do(graphql.Params{
				Schema:        schema,
				RequestString: tt.query,
			})

			if result.HasErrors() {
				t.Fatalf("GraphQL query failed: %v", result.Errors)
			}

			data := result.Data.(map[string]interface{})
			persons := data["persons"].([]interface{})

			if len(persons) != tt.expectedCount {
				t.Errorf("Expected %d persons, got %d", tt.expectedCount, len(persons))
			}
		})
	}
}
