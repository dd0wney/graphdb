package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestEdgeSchemaGeneration tests that edge types are included in schema
func TestEdgeSchemaGeneration(t *testing.T) {
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

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Verify Edge type exists in schema
	edgeType := schema.TypeMap()["Edge"]
	if edgeType == nil {
		t.Error("Schema missing Edge type")
	}
}

// TestQueryEdgeByID tests querying a single edge by ID
func TestQueryEdgeByID(t *testing.T) {
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

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Query for edge by ID
	query := `
		{
			edge(id: "1") {
				id
				fromNodeId
				toNodeId
				type
				weight
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

	// Verify result
	data := result.Data.(map[string]interface{})
	edgeData := data["edge"].(map[string]interface{})

	if edgeData["type"] != "KNOWS" {
		t.Errorf("Expected type KNOWS, got %v", edgeData["type"])
	}

	if edgeData["fromNodeId"] != "1" {
		t.Errorf("Expected fromNodeId 1, got %v", edgeData["fromNodeId"])
	}

	if edgeData["toNodeId"] != "2" {
		t.Errorf("Expected toNodeId 2, got %v", edgeData["toNodeId"])
	}

	// Verify edge exists in storage
	storedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Errorf("Failed to retrieve edge from storage: %v", err)
	}
	if storedEdge.Type != "KNOWS" {
		t.Errorf("Edge type mismatch: expected KNOWS, got %s", storedEdge.Type)
	}
}

// TestQueryAllEdges tests querying all edges
func TestQueryAllEdges(t *testing.T) {
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

	// Create nodes and multiple edges
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	node3, _ := gs.CreateNode([]string{"Company"}, map[string]storage.Value{
		"name": storage.StringValue("TechCorp"),
	})

	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "WORKS_AT", nil, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Query for all edges
	query := `
		{
			edges {
				id
				type
				fromNodeId
				toNodeId
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

	// Verify result
	data := result.Data.(map[string]interface{})
	edges := data["edges"].([]interface{})

	if len(edges) != 2 {
		t.Errorf("Expected 2 edges, got %d", len(edges))
	}
}

// TestCreateEdgeMutation tests creating an edge via GraphQL mutation
func TestCreateEdgeMutation(t *testing.T) {
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

	// Create nodes first
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Execute createEdge mutation
	mutation := `
		mutation {
			createEdge(
				fromNodeId: "1",
				toNodeId: "2",
				type: "KNOWS",
				properties: "{\"since\": 2020}",
				weight: 1.0
			) {
				id
				fromNodeId
				toNodeId
				type
				weight
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: mutation,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL mutation failed: %v", result.Errors)
	}

	// Verify result
	data := result.Data.(map[string]interface{})
	edgeData := data["createEdge"].(map[string]interface{})

	if edgeData["type"] != "KNOWS" {
		t.Errorf("Expected type KNOWS, got %v", edgeData["type"])
	}

	if edgeData["id"] == nil {
		t.Error("Created edge missing ID")
	}

	// Verify edge was created in storage
	stats := gs.GetStatistics()
	if stats.EdgeCount != 1 {
		t.Errorf("Expected 1 edge in storage, got %d", stats.EdgeCount)
	}
}

// TestDeleteEdgeMutation tests deleting an edge via GraphQL mutation
func TestDeleteEdgeMutation(t *testing.T) {
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

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Execute deleteEdge mutation
	mutation := `
		mutation {
			deleteEdge(id: "1") {
				success
				id
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: mutation,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL mutation failed: %v", result.Errors)
	}

	// Verify result
	data := result.Data.(map[string]interface{})
	deleteData := data["deleteEdge"].(map[string]interface{})

	if success, ok := deleteData["success"].(bool); !ok || !success {
		t.Error("Expected success: true")
	}

	// Verify edge was deleted from storage
	_, err = gs.GetEdge(edge.ID)
	if err == nil {
		t.Error("Edge should have been deleted but still exists")
	}
}

// TestUpdateEdgeMutation tests updating an edge via GraphQL mutation
func TestUpdateEdgeMutation(t *testing.T) {
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

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Execute updateEdge mutation
	mutation := `
		mutation {
			updateEdge(id: "1", properties: "{\"since\": 2021, \"strength\": \"strong\"}", weight: 2.5) {
				id
				fromNodeId
				toNodeId
				type
				weight
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: mutation,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL mutation failed: %v", result.Errors)
	}

	// Verify result
	data := result.Data.(map[string]interface{})
	edgeData := data["updateEdge"].(map[string]interface{})

	if edgeData["id"] != "1" {
		t.Errorf("Expected id 1, got %v", edgeData["id"])
	}

	if weight, ok := edgeData["weight"].(float64); !ok || weight != 2.5 {
		t.Errorf("Expected weight 2.5, got %v", edgeData["weight"])
	}

	// Verify edge was updated in storage
	updatedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve updated edge: %v", err)
	}

	// Check weight was updated
	if updatedEdge.Weight != 2.5 {
		t.Errorf("Expected weight 2.5, got %f", updatedEdge.Weight)
	}

	// Check 'since' property was updated
	since, _ := updatedEdge.Properties["since"].AsInt()
	if since != 2021 {
		t.Errorf("Expected since 2021, got %d", since)
	}

	// Check 'strength' property was added
	strength, _ := updatedEdge.Properties["strength"].AsString()
	if strength != "strong" {
		t.Errorf("Expected strength 'strong', got %s", strength)
	}
}

// TestUpdateEdgePropertiesOnly tests updating only properties (not weight)
func TestUpdateEdgePropertiesOnly(t *testing.T) {
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

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Execute updateEdge mutation (properties only)
	mutation := `
		mutation {
			updateEdge(id: "1", properties: "{\"since\": 2022}") {
				id
				weight
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: mutation,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL mutation failed: %v", result.Errors)
	}

	// Verify edge was updated in storage
	updatedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve updated edge: %v", err)
	}

	// Check weight remained the same
	if updatedEdge.Weight != 1.0 {
		t.Errorf("Expected weight 1.0 (unchanged), got %f", updatedEdge.Weight)
	}

	// Check 'since' property was updated
	since, _ := updatedEdge.Properties["since"].AsInt()
	if since != 2022 {
		t.Errorf("Expected since 2022, got %d", since)
	}
}

// TestUpdateEdgeWeightOnly tests updating only weight (not properties)
func TestUpdateEdgeWeightOnly(t *testing.T) {
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

	// Create nodes and edge
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	edge, _ := gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Execute updateEdge mutation (weight only)
	mutation := `
		mutation {
			updateEdge(id: "1", weight: 3.5) {
				id
				weight
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: mutation,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL mutation failed: %v", result.Errors)
	}

	// Verify edge was updated in storage
	updatedEdge, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve updated edge: %v", err)
	}

	// Check weight was updated
	if updatedEdge.Weight != 3.5 {
		t.Errorf("Expected weight 3.5, got %f", updatedEdge.Weight)
	}

	// Check 'since' property remained the same
	since, _ := updatedEdge.Properties["since"].AsInt()
	if since != 2020 {
		t.Errorf("Expected since 2020 (unchanged), got %d", since)
	}
}

// TestEdgeMutationErrorHandling tests error cases for edge mutations
func TestEdgeMutationErrorHandling(t *testing.T) {
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

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	tests := []struct {
		name        string
		mutation    string
		expectError bool
	}{
		{
			name: "create edge with non-existent nodes",
			mutation: `
				mutation {
					createEdge(fromNodeId: "999", toNodeId: "1000", type: "KNOWS", properties: "{}", weight: 1.0) {
						id
					}
				}
			`,
			expectError: true,
		},
		{
			name: "delete non-existent edge",
			mutation: `
				mutation {
					deleteEdge(id: "99999") {
						success
					}
				}
			`,
			expectError: true,
		},
		{
			name: "create edge with invalid JSON properties",
			mutation: `
				mutation {
					createEdge(fromNodeId: "1", toNodeId: "2", type: "KNOWS", properties: "{invalid}", weight: 1.0) {
						id
					}
				}
			`,
			expectError: true,
		},
		{
			name: "update non-existent edge",
			mutation: `
				mutation {
					updateEdge(id: "99999", properties: "{\"test\": 1}") {
						id
					}
				}
			`,
			expectError: true,
		},
		{
			name: "update edge with invalid JSON properties",
			mutation: `
				mutation {
					updateEdge(id: "1", properties: "{invalid}") {
						id
					}
				}
			`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := graphql.Do(graphql.Params{
				Schema:        schema,
				RequestString: tt.mutation,
			})

			if tt.expectError && !result.HasErrors() {
				t.Errorf("Expected error but got none")
			}

			if tt.expectError && result.HasErrors() {
				t.Logf("Got expected error: %s", result.Errors[0].Message)
			}
		})
	}
}

// TestNodeEdgeTraversal tests getting edges from a node
func TestNodeEdgeTraversal(t *testing.T) {
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

	// Create nodes and edges
	node1, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	node2, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	node3, _ := gs.CreateNode([]string{"Company"}, map[string]storage.Value{
		"name": storage.StringValue("TechCorp"),
	})

	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.0)
	gs.CreateEdge(node1.ID, node3.ID, "WORKS_AT", nil, 1.0)

	schema, err := GenerateSchemaWithEdges(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithEdges() error = %v", err)
	}

	// Query node with its outgoing edges
	query := `
		{
			person(id: "1") {
				id
				labels
				outgoingEdges {
					id
					type
					toNodeId
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

	// Verify result
	data := result.Data.(map[string]interface{})
	personData := data["person"].(map[string]interface{})
	outgoingEdges := personData["outgoingEdges"].([]interface{})

	if len(outgoingEdges) != 2 {
		t.Errorf("Expected 2 outgoing edges, got %d", len(outgoingEdges))
	}
}
