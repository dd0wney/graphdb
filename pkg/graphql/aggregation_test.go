package graphql

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// TestAggregateCount tests counting nodes
func TestAggregateCount(t *testing.T) {
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
			"age":  storage.IntValue(int64(i * 10)),
		})
	}

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			personsAggregate {
				count
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
	aggregate := data["personsAggregate"].(map[string]any)
	count := int(aggregate["count"].(int))

	if count != 5 {
		t.Errorf("Expected count to be 5, got %d", count)
	}
}

// TestAggregateMinMax tests min and max aggregations
func TestAggregateMinMax(t *testing.T) {
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
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(40),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(30),
	})

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			personsAggregate {
				min {
					age
				}
				max {
					age
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
	aggregate := data["personsAggregate"].(map[string]any)
	min := aggregate["min"].(map[string]any)
	max := aggregate["max"].(map[string]any)

	minAge := min["age"].(float64)
	maxAge := max["age"].(float64)

	if minAge != 25.0 {
		t.Errorf("Expected min age to be 25, got %f", minAge)
	}
	if maxAge != 40.0 {
		t.Errorf("Expected max age to be 40, got %f", maxAge)
	}
}

// TestAggregateAverage tests average aggregation
func TestAggregateAverage(t *testing.T) {
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

	// Create Person nodes with scores: 10, 20, 30 (average = 20)
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Alice"),
		"score": storage.IntValue(10),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Bob"),
		"score": storage.IntValue(20),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Charlie"),
		"score": storage.IntValue(30),
	})

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			personsAggregate {
				avg {
					score
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
	aggregate := data["personsAggregate"].(map[string]any)
	avg := aggregate["avg"].(map[string]any)

	avgScore := avg["score"].(float64)

	if avgScore != 20.0 {
		t.Errorf("Expected avg score to be 20.0, got %f", avgScore)
	}
}

// TestAggregateSum tests sum aggregation
func TestAggregateSum(t *testing.T) {
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

	// Create Person nodes with scores: 10, 20, 30 (sum = 60)
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Alice"),
		"score": storage.IntValue(10),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Bob"),
		"score": storage.IntValue(20),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Charlie"),
		"score": storage.IntValue(30),
	})

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			personsAggregate {
				sum {
					score
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
	aggregate := data["personsAggregate"].(map[string]any)
	sum := aggregate["sum"].(map[string]any)

	sumScore := sum["score"].(float64)

	if sumScore != 60.0 {
		t.Errorf("Expected sum score to be 60, got %f", sumScore)
	}
}

// TestAggregateMultipleFields tests aggregating multiple fields at once
func TestAggregateMultipleFields(t *testing.T) {
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

	// Create Person nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Alice"),
		"age":   storage.IntValue(25),
		"score": storage.IntValue(85),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":  storage.StringValue("Bob"),
		"age":   storage.IntValue(30),
		"score": storage.IntValue(90),
	})

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			personsAggregate {
				count
				avg {
					age
					score
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
	aggregate := data["personsAggregate"].(map[string]any)

	count := int(aggregate["count"].(int))
	if count != 2 {
		t.Errorf("Expected count to be 2, got %d", count)
	}

	avg := aggregate["avg"].(map[string]any)
	avgAge := avg["age"].(float64)
	avgScore := avg["score"].(float64)

	if avgAge != 27.5 {
		t.Errorf("Expected avg age to be 27.5, got %f", avgAge)
	}
	if avgScore != 87.5 {
		t.Errorf("Expected avg score to be 87.5, got %f", avgScore)
	}
}

// TestAggregateEdges tests aggregating edges
func TestAggregateEdges(t *testing.T) {
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

	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 1.5)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 2.5)
	gs.CreateEdge(node1.ID, node2.ID, "KNOWS", nil, 3.0)

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			edgesAggregate {
				count
				avg {
					weight
				}
				min {
					weight
				}
				max {
					weight
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
	aggregate := data["edgesAggregate"].(map[string]any)

	count := int(aggregate["count"].(int))
	if count != 3 {
		t.Errorf("Expected count to be 3, got %d", count)
	}

	avg := aggregate["avg"].(map[string]any)
	avgWeight := avg["weight"].(float64)
	// (1.5 + 2.5 + 3.0) / 3 = 7.0 / 3 ≈ 2.333...
	if avgWeight < 2.33 || avgWeight > 2.34 {
		t.Errorf("Expected avg weight to be ~2.33, got %f", avgWeight)
	}

	min := aggregate["min"].(map[string]any)
	minWeight := min["weight"].(float64)
	if minWeight != 1.5 {
		t.Errorf("Expected min weight to be 1.5, got %f", minWeight)
	}

	max := aggregate["max"].(map[string]any)
	maxWeight := max["weight"].(float64)
	if maxWeight != 3.0 {
		t.Errorf("Expected max weight to be 3.0, got %f", maxWeight)
	}
}

// TestAggregateEmptyResult tests aggregating when no nodes match
func TestAggregateEmptyResult(t *testing.T) {
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

	// Create a Person node to register the label, then delete it
	node, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Temp"),
	})
	gs.DeleteNode(node.ID)

	schema, err := GenerateSchemaWithAggregation(gs)
	if err != nil {
		t.Fatalf("GenerateSchemaWithAggregation() error = %v", err)
	}

	query := `
		{
			personsAggregate {
				count
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
	aggregate := data["personsAggregate"].(map[string]any)
	count := int(aggregate["count"].(int))

	if count != 0 {
		t.Errorf("Expected count to be 0, got %d", count)
	}
}
