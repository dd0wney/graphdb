package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestCursorPaginationForward tests forward cursor-based pagination (first + after)
func TestCursorPaginationForward(t *testing.T) {
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

	schema, err := GenerateSchemaWithCursors(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithCursors() error = %v", err)
	}

	// Test first 5 items
	query := `
		{
			personsConnection(first: 5) {
				edges {
					cursor
					node {
						id
						properties
					}
				}
				pageInfo {
					hasNextPage
					hasPreviousPage
					startCursor
					endCursor
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

	data := result.Data.(map[string]interface{})
	connection := data["personsConnection"].(map[string]interface{})
	edges := connection["edges"].([]interface{})
	pageInfo := connection["pageInfo"].(map[string]interface{})

	if len(edges) != 5 {
		t.Errorf("Expected 5 edges with first: 5, got %d", len(edges))
	}

	if !pageInfo["hasNextPage"].(bool) {
		t.Error("Expected hasNextPage to be true")
	}

	if pageInfo["hasPreviousPage"].(bool) {
		t.Error("Expected hasPreviousPage to be false")
	}

	// Verify cursor exists
	firstEdge := edges[0].(map[string]interface{})
	if firstEdge["cursor"] == nil || firstEdge["cursor"] == "" {
		t.Error("Expected cursor to be present")
	}
}

// TestCursorPaginationAfter tests pagination using after cursor
func TestCursorPaginationAfter(t *testing.T) {
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

	schema, err := GenerateSchemaWithCursors(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithCursors() error = %v", err)
	}

	// First, get first 3 items to get endCursor
	query1 := `
		{
			personsConnection(first: 3) {
				pageInfo {
					endCursor
				}
			}
		}
	`

	result1 := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query1,
	})

	if result1.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result1.Errors)
	}

	data1 := result1.Data.(map[string]interface{})
	connection1 := data1["personsConnection"].(map[string]interface{})
	pageInfo1 := connection1["pageInfo"].(map[string]interface{})
	endCursor := pageInfo1["endCursor"].(string)

	// Now use after with the endCursor to get next items
	query2 := `
		{
			personsConnection(first: 3, after: "` + endCursor + `") {
				edges {
					node {
						id
					}
				}
				pageInfo {
					hasNextPage
					hasPreviousPage
				}
			}
		}
	`

	result2 := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query2,
	})

	if result2.HasErrors() {
		t.Fatalf("GraphQL query with after failed: %v", result2.Errors)
	}

	data2 := result2.Data.(map[string]interface{})
	connection2 := data2["personsConnection"].(map[string]interface{})
	edges2 := connection2["edges"].([]interface{})
	pageInfo2 := connection2["pageInfo"].(map[string]interface{})

	if len(edges2) != 3 {
		t.Errorf("Expected 3 edges after cursor, got %d", len(edges2))
	}

	if !pageInfo2["hasNextPage"].(bool) {
		t.Error("Expected hasNextPage to be true")
	}

	if !pageInfo2["hasPreviousPage"].(bool) {
		t.Error("Expected hasPreviousPage to be true after using 'after'")
	}
}

// TestCursorPaginationBackward tests backward cursor-based pagination (last + before)
func TestCursorPaginationBackward(t *testing.T) {
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

	schema, err := GenerateSchemaWithCursors(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithCursors() error = %v", err)
	}

	// Test last 5 items
	query := `
		{
			personsConnection(last: 5) {
				edges {
					cursor
					node {
						id
					}
				}
				pageInfo {
					hasNextPage
					hasPreviousPage
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

	data := result.Data.(map[string]interface{})
	connection := data["personsConnection"].(map[string]interface{})
	edges := connection["edges"].([]interface{})
	pageInfo := connection["pageInfo"].(map[string]interface{})

	if len(edges) != 5 {
		t.Errorf("Expected 5 edges with last: 5, got %d", len(edges))
	}

	if pageInfo["hasNextPage"].(bool) {
		t.Error("Expected hasNextPage to be false for last items")
	}

	if !pageInfo["hasPreviousPage"].(bool) {
		t.Error("Expected hasPreviousPage to be true")
	}
}

// TestCursorPaginationEdgeCases tests edge cases for cursor pagination
func TestCursorPaginationEdgeCases(t *testing.T) {
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

	schema, err := GenerateSchemaWithCursors(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithCursors() error = %v", err)
	}

	tests := []struct {
		name                 string
		query                string
		expectedEdgeCount    int
		expectedHasNext      bool
		expectedHasPrevious  bool
	}{
		{
			name: "first larger than total",
			query: `
				{
					personsConnection(first: 100) {
						edges { node { id } }
						pageInfo { hasNextPage hasPreviousPage }
					}
				}
			`,
			expectedEdgeCount:   5,
			expectedHasNext:     false,
			expectedHasPrevious: false,
		},
		{
			name: "first 0",
			query: `
				{
					personsConnection(first: 0) {
						edges { node { id } }
						pageInfo { hasNextPage hasPreviousPage }
					}
				}
			`,
			expectedEdgeCount:   0,
			expectedHasNext:     true,
			expectedHasPrevious: false,
		},
		{
			name: "no arguments returns all",
			query: `
				{
					personsConnection {
						edges { node { id } }
						pageInfo { hasNextPage hasPreviousPage }
					}
				}
			`,
			expectedEdgeCount:   5,
			expectedHasNext:     false,
			expectedHasPrevious: false,
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
			connection := data["personsConnection"].(map[string]interface{})
			edges := connection["edges"].([]interface{})
			pageInfo := connection["pageInfo"].(map[string]interface{})

			if len(edges) != tt.expectedEdgeCount {
				t.Errorf("Expected %d edges, got %d", tt.expectedEdgeCount, len(edges))
			}

			if pageInfo["hasNextPage"].(bool) != tt.expectedHasNext {
				t.Errorf("Expected hasNextPage to be %v, got %v", tt.expectedHasNext, pageInfo["hasNextPage"])
			}

			if pageInfo["hasPreviousPage"].(bool) != tt.expectedHasPrevious {
				t.Errorf("Expected hasPreviousPage to be %v, got %v", tt.expectedHasPrevious, pageInfo["hasPreviousPage"])
			}
		})
	}
}

// TestEdgeCursorPagination tests cursor pagination for edges
func TestEdgeCursorPagination(t *testing.T) {
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

	schema, err := GenerateSchemaWithCursors(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithCursors() error = %v", err)
	}

	// Test first 5 edges
	query := `
		{
			edgesConnection(first: 5) {
				edges {
					cursor
					node {
						id
						type
						weight
					}
				}
				pageInfo {
					hasNextPage
					hasPreviousPage
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

	data := result.Data.(map[string]interface{})
	connection := data["edgesConnection"].(map[string]interface{})
	edges := connection["edges"].([]interface{})
	pageInfo := connection["pageInfo"].(map[string]interface{})

	if len(edges) != 5 {
		t.Errorf("Expected 5 edges with first: 5, got %d", len(edges))
	}

	if !pageInfo["hasNextPage"].(bool) {
		t.Error("Expected hasNextPage to be true")
	}

	if pageInfo["hasPreviousPage"].(bool) {
		t.Error("Expected hasPreviousPage to be false")
	}
}

// TestInvalidCursor tests error handling for invalid cursors
func TestInvalidCursor(t *testing.T) {
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
	for i := 1; i <= 5; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person" + string(rune('0'+i))),
		})
	}

	schema, err := GenerateSchemaWithCursors(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithCursors() error = %v", err)
	}

	// Test with invalid cursor
	query := `
		{
			personsConnection(first: 3, after: "invalid-cursor-string") {
				edges {
					node {
						id
					}
				}
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
	})

	if !result.HasErrors() {
		t.Fatal("Expected error for invalid cursor, got none")
	}
}
