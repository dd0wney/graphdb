package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestFilterWithOR tests filtering with OR logic
func TestFilterWithOR(t *testing.T) {
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

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("David"),
		"age":  storage.IntValue(20),
	})

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	// Query: age > 30 OR age < 22
	// Should match: Charlie (35) and David (20)
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
		VariableValues: map[string]any{
			"where": map[string]any{
				"OR": []any{
					map[string]any{
						"age": map[string]any{
							"gt": 30,
						},
					},
					map[string]any{
						"age": map[string]any{
							"lt": 22,
						},
					},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Charlie (35) and David (20)
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons matching OR condition, got %d", len(persons))
	}
}

// TestFilterWithNOT tests filtering with NOT logic
func TestFilterWithNOT(t *testing.T) {
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

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(30),
	})

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	// Query: NOT (age == 30)
	// Should match: Bob (25)
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
		VariableValues: map[string]any{
			"where": map[string]any{
				"NOT": map[string]any{
					"age": map[string]any{
						"eq": 30,
					},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return only Bob (25)
	if len(persons) != 1 {
		t.Errorf("Expected 1 person not matching age 30, got %d", len(persons))
	}
}

// TestFilterWithAND tests explicit AND logic
func TestFilterWithAND(t *testing.T) {
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

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"age":    storage.IntValue(30),
		"active": storage.BoolValue(true),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Bob"),
		"age":    storage.IntValue(35),
		"active": storage.BoolValue(false),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"age":    storage.IntValue(35),
		"active": storage.BoolValue(true),
	})

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	// Query: age >= 30 AND active == true
	// Should match: Alice (30, active) and Charlie (35, active)
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
		VariableValues: map[string]any{
			"where": map[string]any{
				"AND": []any{
					map[string]any{
						"age": map[string]any{
							"gte": 30,
						},
					},
					map[string]any{
						"active": map[string]any{
							"eq": true,
						},
					},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Alice and Charlie
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons matching AND condition, got %d", len(persons))
	}
}

// TestFilterWithNestedLogic tests combining AND/OR/NOT
func TestFilterWithNestedLogic(t *testing.T) {
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

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"age":    storage.IntValue(30),
		"active": storage.BoolValue(true),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Bob"),
		"age":    storage.IntValue(25),
		"active": storage.BoolValue(false),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"age":    storage.IntValue(35),
		"active": storage.BoolValue(true),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("David"),
		"age":    storage.IntValue(20),
		"active": storage.BoolValue(true),
	})

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	// Query: (age > 30 OR age < 22) AND active == true
	// Should match: Charlie (35, active) and David (20, active)
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
		VariableValues: map[string]any{
			"where": map[string]any{
				"AND": []any{
					map[string]any{
						"OR": []any{
							map[string]any{
								"age": map[string]any{
									"gt": 30,
								},
							},
							map[string]any{
								"age": map[string]any{
									"lt": 22,
								},
							},
						},
					},
					map[string]any{
						"active": map[string]any{
							"eq": true,
						},
					},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Charlie and David
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons matching nested condition, got %d", len(persons))
	}
}

// TestFilterWithNOTAndOR tests NOT combined with OR
func TestFilterWithNOTAndOR(t *testing.T) {
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

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
	})

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	// Query: NOT (age == 25 OR age == 35)
	// Should match: Alice (30)
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
		VariableValues: map[string]any{
			"where": map[string]any{
				"NOT": map[string]any{
					"OR": []any{
						map[string]any{
							"age": map[string]any{
								"eq": 25,
							},
						},
						map[string]any{
							"age": map[string]any{
								"eq": 35,
							},
						},
					},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return only Alice (30)
	if len(persons) != 1 {
		t.Errorf("Expected 1 person not matching age 25 or 35, got %d", len(persons))
	}
}

// TestLogicalOperatorsWithEdges tests logical operators on edge filtering
func TestLogicalOperatorsWithEdges(t *testing.T) {
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

	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2020),
	}, 1.0)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2021),
	}, 2.0)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", map[string]storage.Value{
		"since": storage.IntValue(2022),
	}, 3.0)

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	// Query: since < 2021 OR weight > 2.5
	// Should match: edge 1 (2020, 1.0) and edge 3 (2022, 3.0)
	query := `
		query($where: WhereInput) {
			edges(where: $where) {
				properties
				weight
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]any{
			"where": map[string]any{
				"OR": []any{
					map[string]any{
						"since": map[string]any{
							"lt": 2021,
						},
					},
					map[string]any{
						"weight": map[string]any{
							"gt": 2.5,
						},
					},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	edges := data["edges"].([]any)

	// Should return 2 edges
	if len(edges) != 2 {
		t.Errorf("Expected 2 edges matching OR condition, got %d", len(edges))
	}
}
