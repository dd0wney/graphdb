package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestFilterByEquality tests filtering with equality operator
func TestFilterByEquality(t *testing.T) {
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
				"age": map[string]any{
					"eq": 30,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	if len(persons) != 2 {
		t.Errorf("Expected 2 persons with age 30, got %d", len(persons))
	}
}

// TestFilterByGreaterThan tests filtering with gt operator
func TestFilterByGreaterThan(t *testing.T) {
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
				"age": map[string]any{
					"gt": 28,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Alice (30) and Charlie (35)
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons with age > 28, got %d", len(persons))
	}
}

// TestFilterByLessThan tests filtering with lt operator
func TestFilterByLessThan(t *testing.T) {
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
				"age": map[string]any{
					"lt": 30,
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
		t.Errorf("Expected 1 person with age < 30, got %d", len(persons))
	}
}

// TestFilterByRange tests filtering with gte and lte operators
func TestFilterByRange(t *testing.T) {
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
	for i := 20; i <= 40; i += 5 {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
			"age":  storage.IntValue(int64(i)),
		})
	}

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

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
				"age": map[string]any{
					"gte": 25,
					"lte": 35,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return ages 25, 30, 35 (3 persons)
	if len(persons) != 3 {
		t.Errorf("Expected 3 persons with age between 25 and 35, got %d", len(persons))
	}
}

// TestFilterByStringContains tests filtering with contains operator
func TestFilterByStringContains(t *testing.T) {
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
		"name": storage.StringValue("Alice Smith"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob Jones"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie Smith"),
	})

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

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
				"name": map[string]any{
					"contains": "Smith",
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Alice and Charlie (both have "Smith")
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons with name containing 'Smith', got %d", len(persons))
	}
}

// TestFilterByIn tests filtering with in operator
func TestFilterByIn(t *testing.T) {
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
				"age": map[string]any{
					"in": []any{25, 35},
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Bob (25) and Charlie (35)
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons with age in [25, 35], got %d", len(persons))
	}
}

// TestFilterMultipleConditions tests filtering with multiple field conditions
func TestFilterMultipleConditions(t *testing.T) {
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

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

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
				"age": map[string]any{
					"gte": 30,
				},
				"active": map[string]any{
					"eq": true,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return Alice (30, active) and Charlie (35, active)
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons with age >= 30 and active = true, got %d", len(persons))
	}
}

// TestFilterWithSortingAndPagination tests filtering combined with sorting and pagination
func TestFilterWithSortingAndPagination(t *testing.T) {
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

	// Create 10 test nodes
	for i := 1; i <= 10; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
			"age":  storage.IntValue(int64(i * 5)),
		})
	}

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

	query := `
		query($where: WhereInput, $orderBy: OrderByInput, $limit: Int) {
			persons(
				where: $where,
				orderBy: $orderBy,
				limit: $limit
			) {
				properties
			}
		}
	`

	result := graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		VariableValues: map[string]any{
			"where": map[string]any{
				"age": map[string]any{
					"gte": 20,
					"lte": 40,
				},
			},
			"orderBy": map[string]any{
				"field":     "age",
				"direction": "ASC",
			},
			"limit": 2,
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	// Should return first 2 persons in age range 20-40, sorted by age
	if len(persons) != 2 {
		t.Errorf("Expected 2 persons with filtering, sorting, and pagination, got %d", len(persons))
	}
}

// TestFilterEdges tests filtering edges by properties
func TestFilterEdges(t *testing.T) {
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
				"since": map[string]any{
					"gte": 2021,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	edges := data["edges"].([]any)

	// Should return 2 edges (since >= 2021)
	if len(edges) != 2 {
		t.Errorf("Expected 2 edges with since >= 2021, got %d", len(edges))
	}
}

// TestFilterNoResults tests that filtering with no matches returns empty array
func TestFilterNoResults(t *testing.T) {
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

	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithFiltering() error = %v", err)
	}

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
				"age": map[string]any{
					"gt": 100,
				},
			},
		},
	})

	if result.HasErrors() {
		t.Fatalf("GraphQL query failed: %v", result.Errors)
	}

	data := result.Data.(map[string]any)
	persons := data["persons"].([]any)

	if len(persons) != 0 {
		t.Errorf("Expected 0 persons, got %d", len(persons))
	}
}
