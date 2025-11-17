package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestSchemaGeneration tests generating GraphQL schema from storage
func TestSchemaGeneration(t *testing.T) {
	// Create test storage with nodes and edges
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

	// Add test nodes with different labels
	node1, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{
			"name": storage.StringValue("Alice"),
			"age":  storage.IntValue(30),
		},
	)

	node2, _ := gs.CreateNode(
		[]string{"Company"},
		map[string]storage.Value{
			"name":    storage.StringValue("TechCorp"),
			"founded": storage.IntValue(2010),
			"revenue": storage.FloatValue(1000000.50),
			"active":  storage.BoolValue(true),
		},
	)

	// Add test edge
	gs.CreateEdge(node1.ID, node2.ID, "WORKS_AT", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)

	// Generate GraphQL schema
	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Verify schema contains Query type
	queryType := schema.QueryType()
	if queryType == nil {
		t.Error("Schema missing Query type")
	}
}

// TestSchemaNodeTypes tests that node labels become GraphQL types
func TestSchemaNodeTypes(t *testing.T) {
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

	// Add node with Person label
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{
			"name": storage.StringValue("Alice"),
			"age":  storage.IntValue(30),
		},
	)

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Verify Person type exists
	personType := schema.TypeMap()["Person"]
	if personType == nil {
		t.Error("Schema missing Person type")
	}
}

// TestSchemaQueryFields tests that query fields are generated for each label
func TestSchemaQueryFields(t *testing.T) {
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

	// Add nodes with different labels
	gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Alice")},
	)

	gs.CreateNode(
		[]string{"Company"},
		map[string]storage.Value{"name": storage.StringValue("TechCorp")},
	)

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	queryType := schema.QueryType()
	if queryType == nil {
		t.Fatal("Schema missing Query type")
	}

	// Verify query fields exist for each label
	fields := queryType.Fields()

	// Should have: person(id: ID!), persons, company(id: ID!), companys
	// Note: Using simple pluralization (label + "s") for now
	expectedFields := []string{"person", "persons", "company", "companys"}
	for _, fieldName := range expectedFields {
		if fields[fieldName] == nil {
			t.Errorf("Query type missing field: %s", fieldName)
		}
	}
}

// TestSchemaEdgeTypes tests that edge types are included in node types
func TestSchemaEdgeTypes(t *testing.T) {
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

	// Add nodes and edge
	node1, _ := gs.CreateNode(
		[]string{"Person"},
		map[string]storage.Value{"name": storage.StringValue("Alice")},
	)

	node2, _ := gs.CreateNode(
		[]string{"Company"},
		map[string]storage.Value{"name": storage.StringValue("TechCorp")},
	)

	gs.CreateEdge(node1.ID, node2.ID, "WORKS_AT", map[string]storage.Value{}, 1.0)

	schema, err := GenerateSchema(gs)
	if err != nil {
		t.Fatalf("GenerateSchema() error = %v", err)
	}

	// Verify Person type has WORKS_AT field for traversal
	personType := schema.TypeMap()["Person"]
	if personType == nil {
		t.Fatal("Schema missing Person type")
	}

	// Person should have a field for outgoing WORKS_AT edges
	// (This will be implemented as a "worksAt" field returning [Company])
}
