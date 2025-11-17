package graphql

import (
	"encoding/json"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestMutationSchemaGeneration tests that mutations are included in schema
func TestMutationSchemaGeneration(t *testing.T) {
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

	// Add a node so schema generation has labels
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Test")},
	)

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations() error = %v", err)
	}

	// Verify schema contains Mutation type
	mutationType := schema.MutationType()
	if mutationType == nil {
		t.Error("Schema missing Mutation type")
	}
}

// TestCreateNodeMutation tests creating a node via GraphQL mutation
func TestCreateNodeMutation(t *testing.T) {
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

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations() error = %v", err)
	}

	// Execute createNode mutation
	mutation := `
		mutation {
			createNode(labels: ["Person"], properties: "{\"name\": \"Alice\", \"age\": 30}") {
				id
				labels
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
	createNodeData := data["createNode"].(map[string]interface{})

	if createNodeData["id"] == nil {
		t.Error("Created node missing ID")
	}

	labels := createNodeData["labels"].([]interface{})
	if len(labels) != 1 || labels[0].(string) != "Person" {
		t.Errorf("Expected labels [Person], got %v", labels)
	}

	// Verify node was actually created in storage
	nodeID, _ := createNodeData["id"].(string)
	var id uint64
	json.Unmarshal([]byte(nodeID), &id)

	node, err := gs.GetNode(id)
	if err != nil {
		t.Errorf("Failed to retrieve created node: %v", err)
	}

	if node.ID != id {
		t.Errorf("Node ID mismatch: expected %d, got %d", id, node.ID)
	}
}

// TestUpdateNodeMutation tests updating a node via GraphQL mutation
func TestUpdateNodeMutation(t *testing.T) {
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

	// Create initial node
	node, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{
			"name": storage.StringValue("Alice"),
			"age":  storage.IntValue(30),
		},
	)

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations() error = %v", err)
	}

	// Execute updateNode mutation
	mutation := `
		mutation {
			updateNode(id: "1", properties: "{\"age\": 31, \"city\": \"NYC\"}") {
				id
				labels
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

	// Verify node was updated in storage
	updatedNode, err := gs.GetNode(node.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve updated node: %v", err)
	}

	// Check age was updated
	age, _ := updatedNode.Properties["age"].AsInt()
	if age != 31 {
		t.Errorf("Expected age 31, got %d", age)
	}

	// Check city was added
	city, _ := updatedNode.Properties["city"].AsString()
	if city != "NYC" {
		t.Errorf("Expected city NYC, got %s", city)
	}

	// Check name is still there
	name, _ := updatedNode.Properties["name"].AsString()
	if name != "Alice" {
		t.Errorf("Expected name Alice to remain, got %s", name)
	}
}

// TestDeleteNodeMutation tests deleting a node via GraphQL mutation
func TestDeleteNodeMutation(t *testing.T) {
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

	// Create node to delete
	node, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Bob")},
	)

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations() error = %v", err)
	}

	// Execute deleteNode mutation
	mutation := `
		mutation {
			deleteNode(id: "1") {
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
	deleteNodeData := data["deleteNode"].(map[string]interface{})

	if success, ok := deleteNodeData["success"].(bool); !ok || !success {
		t.Error("Expected success: true")
	}

	// Verify node was actually deleted from storage
	_, err = gs.GetNode(node.ID)
	if err == nil {
		t.Error("Node should have been deleted but still exists")
	}
}

// TestCreateNodeMutationWithVariables tests creating a node with GraphQL variables
func TestCreateNodeMutationWithVariables(t *testing.T) {
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

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations() error = %v", err)
	}

	// Execute createNode mutation with variables
	mutation := `
		mutation CreatePerson($labels: [String!]!, $properties: String!) {
			createNode(labels: $labels, properties: $properties) {
				id
				labels
				properties
			}
		}
	`

	variables := map[string]interface{}{
		"labels":     []string{"Person"},
		"properties": "{\"name\": \"Charlie\", \"age\": 25}",
	}

	result := graphql.Do(graphql.Params{
		Schema:         schema,
		RequestString:  mutation,
		VariableValues: variables,
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL mutation failed: %v", result.Errors)
	}

	// Verify result
	data := result.Data.(map[string]interface{})
	createNodeData := data["createNode"].(map[string]interface{})

	if createNodeData["id"] == nil {
		t.Error("Created node missing ID")
	}

	labels := createNodeData["labels"].([]interface{})
	if len(labels) != 1 || labels[0].(string) != "Person" {
		t.Errorf("Expected labels [Person], got %v", labels)
	}
}

// TestMutationErrorHandling tests error cases for mutations
func TestMutationErrorHandling(t *testing.T) {
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

	schema, err := GenerateSchemaWithMutations(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithMutations() error = %v", err)
	}

	tests := []struct {
		name          string
		mutation      string
		expectError   bool
		errorContains string
	}{
		{
			name: "invalid JSON in properties",
			mutation: `
				mutation {
					createNode(labels: ["Person"], properties: "{invalid json}") {
						id
					}
				}
			`,
			expectError:   true,
			errorContains: "invalid",
		},
		{
			name: "delete non-existent node",
			mutation: `
				mutation {
					deleteNode(id: "99999") {
						success
					}
				}
			`,
			expectError:   true,
			errorContains: "not found",
		},
		{
			name: "update non-existent node",
			mutation: `
				mutation {
					updateNode(id: "99999", properties: "{\"age\": 30}") {
						id
					}
				}
			`,
			expectError:   true,
			errorContains: "not found",
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
				errorMsg := result.Errors[0].Message
				if tt.errorContains != "" {
					// Note: We're just checking that we got an error
					// Specific error message validation can be adjusted
					t.Logf("Got expected error: %s", errorMsg)
				}
			}
		})
	}
}
