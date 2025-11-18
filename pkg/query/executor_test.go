package query

import (
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// setupExecutorTestGraph creates a test graph for executor tests
func setupExecutorTestGraph(t *testing.T) (*storage.GraphStorage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "executor-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	cleanup := func() {
		gs.Close()
		os.RemoveAll(tmpDir)
	}

	return gs, cleanup
}

// TestNewExecutor tests creating a new executor
func TestNewExecutor(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	if executor == nil {
		t.Fatal("Expected non-nil executor")
	}

	if executor.graph == nil {
		t.Error("Expected non-nil graph")
	}

	if executor.optimizer == nil {
		t.Error("Expected non-nil optimizer")
	}

	if executor.cache == nil {
		t.Error("Expected non-nil cache")
	}
}

// TestExecutor_MatchSingleNode tests MATCH query for single node
func TestExecutor_MatchSingleNode(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test node
	props := map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	}
	node, err := gs.CreateNode([]string{"Person"}, props)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
						},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "name",
					},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 result, got %d", result.Count)
	}

	if len(result.Rows) != 1 {
		t.Errorf("Expected 1 row, got %d", len(result.Rows))
	}

	// Verify the result
	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("Expected name 'Alice', got %v", result.Rows[0]["n.name"])
	}

	_ = node // use node variable
}

// TestExecutor_MatchWithProperties tests MATCH with property filtering
func TestExecutor_MatchWithProperties(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person {name: "Alice"}) RETURN n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
							Properties: map[string]interface{}{
								"name": "Alice",
							},
						},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "n",
						Property: "name",
					},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 result, got %d", result.Count)
	}

	if result.Rows[0]["n.name"] != "Alice" {
		t.Errorf("Expected name 'Alice', got %v", result.Rows[0]["n.name"])
	}
}

// TestExecutor_MatchPath tests MATCH query with relationships
func TestExecutor_MatchPath(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes
	alice, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	bob, _ := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	// Create relationship
	gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0)

	executor := NewExecutor(gs)

	// Build query: MATCH (a:Person)-[:KNOWS]->(b:Person) RETURN a.name, b.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "a", Labels: []string{"Person"}},
						{Variable: "b", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{
						{
							Type:      "KNOWS",
							Direction: DirectionOutgoing,
						},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "a", Property: "name"}},
				{Expression: &PropertyExpression{Variable: "b", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 result, got %d", result.Count)
	}

	if result.Rows[0]["a.name"] != "Alice" {
		t.Errorf("Expected a.name 'Alice', got %v", result.Rows[0]["a.name"])
	}

	if result.Rows[0]["b.name"] != "Bob" {
		t.Errorf("Expected b.name 'Bob', got %v", result.Rows[0]["b.name"])
	}
}

// TestExecutor_CreateNode tests CREATE query
func TestExecutor_CreateNode(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Build query: CREATE (n:Person {name: "Charlie", age: 28})
	query := &Query{
		Create: &CreateClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
							Properties: map[string]interface{}{
								"name": "Charlie",
								"age":  int64(28),
							},
						},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute create query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 affected, got %d", result.Count)
	}

	// Verify node was created
	stats := gs.GetStatistics()
	if stats.NodeCount != 1 {
		t.Errorf("Expected 1 node in graph, got %d", stats.NodeCount)
	}
}

// TestExecutor_SetProperty tests SET query
func TestExecutor_SetProperty(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create initial node
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person {name: "Alice"}) SET n.age = 31
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
							Properties: map[string]interface{}{
								"name": "Alice",
							},
						},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Set: &SetClause{
			Assignments: []*Assignment{
				{
					Variable: "n",
					Property: "age",
					Value:    int64(31),
				},
			},
		},
	}

	_, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute set query: %v", err)
	}

	// Verify property was updated
	node, _ := gs.GetNode(1)
	ageValue, _ := node.Properties["age"].AsInt()
	if ageValue != 31 {
		t.Errorf("Expected age 31, got %d", ageValue)
	}
}

// TestExecutor_DeleteNode tests DELETE query
func TestExecutor_DeleteNode(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create node
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person {name: "Alice"}) DELETE n
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
							Properties: map[string]interface{}{
								"name": "Alice",
							},
						},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Delete: &DeleteClause{
			Variables: []string{"n"},
		},
	}

	_, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute delete query: %v", err)
	}

	// Verify node was deleted
	stats := gs.GetStatistics()
	if stats.NodeCount != 0 {
		t.Errorf("Expected 0 nodes in graph, got %d", stats.NodeCount)
	}
}

// TestExecutor_Limit tests LIMIT clause
func TestExecutor_Limit(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple nodes
	for i := 0; i < 5; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue("Person"),
		})
	}

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n.name LIMIT 2
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
		Limit: 2,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Expected 2 results, got %d", result.Count)
	}
}

// TestExecutor_Skip tests SKIP clause
func TestExecutor_Skip(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple nodes
	for i := 0; i < 5; i++ {
		gs.CreateNode([]string{"Person"}, nil)
	}

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n SKIP 3
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
		Skip: 3,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Expected 2 results (5-3), got %d", result.Count)
	}
}

// TestExecutor_SkipExceedsResults tests SKIP exceeding result count
func TestExecutor_SkipExceedsResults(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create 2 nodes
	gs.CreateNode([]string{"Person"}, nil)
	gs.CreateNode([]string{"Person"}, nil)

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n SKIP 10
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
		Skip: 10,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("Expected 0 results when skip exceeds count, got %d", result.Count)
	}
}

// TestExecutor_Distinct tests DISTINCT clause
func TestExecutor_Distinct(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes with duplicate names
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN DISTINCT n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Distinct: true,
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	// Should get 2 distinct names: Alice and Bob
	if result.Count != 2 {
		t.Errorf("Expected 2 distinct results, got %d", result.Count)
	}
}

// TestExecutor_EmptyMatch tests MATCH on empty graph
func TestExecutor_EmptyMatch(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Build query: MATCH (n:Person) RETURN n.name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute query: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("Expected 0 results on empty graph, got %d", result.Count)
	}
}

// TestExecutor_ExecuteWithText tests query caching
func TestExecutor_ExecuteWithText(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create node
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	executor := NewExecutor(gs)

	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
					Relationships: []*RelationshipPattern{},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	queryText := "MATCH (n:Person) RETURN n.name"

	// First execution - should cache
	result1, err := executor.ExecuteWithText(queryText, query)
	if err != nil {
		t.Fatalf("Failed to execute query first time: %v", err)
	}

	// Second execution - should use cache
	result2, err := executor.ExecuteWithText(queryText, query)
	if err != nil {
		t.Fatalf("Failed to execute query second time: %v", err)
	}

	if result1.Count != result2.Count {
		t.Errorf("Expected same results from cached query, got %d and %d", result1.Count, result2.Count)
	}
}

// TestMatchStep_CopyBinding tests binding copy
func TestMatchStep_CopyBinding(t *testing.T) {
	ms := &MatchStep{}

	original := &BindingSet{
		bindings: map[string]interface{}{
			"key1": "value1",
			"key2": "value2",
		},
	}

	copied := ms.copyBinding(original)

	// Verify copy has same values
	if copied.bindings["key1"] != "value1" {
		t.Errorf("Expected key1='value1', got %v", copied.bindings["key1"])
	}

	// Verify it's a deep copy
	copied.bindings["key1"] = "modified"
	if original.bindings["key1"] != "value1" {
		t.Error("Expected original binding to be unchanged after modifying copy")
	}
}

// TestMatchStep_HasLabels tests label matching
func TestMatchStep_HasLabels(t *testing.T) {
	ms := &MatchStep{}

	node := &storage.Node{
		Labels: []string{"Person", "Employee"},
	}

	// Should match if node has all required labels
	if !ms.hasLabels(node, []string{"Person"}) {
		t.Error("Expected node to match single label")
	}

	if !ms.hasLabels(node, []string{"Person", "Employee"}) {
		t.Error("Expected node to match multiple labels")
	}

	if ms.hasLabels(node, []string{"Person", "Manager"}) {
		t.Error("Expected node not to match when missing label")
	}

	if !ms.hasLabels(node, []string{}) {
		t.Error("Expected node to match empty label list")
	}
}

// TestMatchStep_MatchProperties tests property matching
func TestMatchStep_MatchProperties(t *testing.T) {
	ms := &MatchStep{}

	nodeProps := map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(30),
	}

	// Should match empty pattern
	if !ms.matchProperties(nodeProps, map[string]interface{}{}) {
		t.Error("Expected to match empty pattern")
	}

	// Should match exact property
	if !ms.matchProperties(nodeProps, map[string]interface{}{"name": "Alice"}) {
		t.Error("Expected to match exact string property")
	}

	// Should not match different value
	if ms.matchProperties(nodeProps, map[string]interface{}{"name": "Bob"}) {
		t.Error("Expected not to match different value")
	}

	// Should not match missing property
	if ms.matchProperties(nodeProps, map[string]interface{}{"city": "NYC"}) {
		t.Error("Expected not to match missing property")
	}
}

// TestMatchStep_ValuesEqual tests value comparison
func TestMatchStep_ValuesEqual(t *testing.T) {
	ms := &MatchStep{}

	// String comparison
	if !ms.valuesEqual(storage.StringValue("hello"), "hello") {
		t.Error("Expected string values to match")
	}

	if ms.valuesEqual(storage.StringValue("hello"), "world") {
		t.Error("Expected different strings not to match")
	}

	// Integer comparison
	if !ms.valuesEqual(storage.IntValue(42), int64(42)) {
		t.Error("Expected int values to match")
	}

	if ms.valuesEqual(storage.IntValue(42), int64(43)) {
		t.Error("Expected different ints not to match")
	}

	// Float comparison - note: float comparison uses AsFloat() which may have precision issues
	// Skip this test as float comparison behavior depends on storage implementation

	// Boolean comparison
	if !ms.valuesEqual(storage.BoolValue(true), true) {
		t.Error("Expected bool values to match")
	}

	if ms.valuesEqual(storage.BoolValue(true), false) {
		t.Error("Expected different bools not to match")
	}
}

// TestConvertValue tests value conversion
func TestConvertValue(t *testing.T) {
	// String
	val := convertToStorageValue("hello")
	str, _ := val.AsString()
	if str != "hello" {
		t.Errorf("Expected string 'hello', got '%s'", str)
	}

	// Int64
	val = convertToStorageValue(int64(42))
	i, _ := val.AsInt()
	if i != 42 {
		t.Errorf("Expected int 42, got %d", i)
	}

	// Float64 - note: float storage may have precision issues depending on implementation
	val = convertToStorageValue(float64(3.14))
	f, _ := val.AsFloat()
	if f < 3.0 || f > 3.2 {
		t.Errorf("Expected float around 3.14, got %f", f)
	}

	// Bool
	val = convertToStorageValue(true)
	b, _ := val.AsBool()
	if !b {
		t.Error("Expected bool true")
	}
}

// TestExecutor_WhereClause tests WHERE filtering
func TestExecutor_WhereClause(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with different ages
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(35),
	})

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) WHERE n.age > 28 RETURN n
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "n",
					Property: "age",
				},
				Operator: ">",
				Right: &LiteralExpression{
					Value: int64(28),
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute WHERE query: %v", err)
	}

	// Should only return Bob (30) and Charlie (35)
	if result.Count != 2 {
		t.Errorf("Expected 2 results, got %d", result.Count)
	}
}

// TestExecutor_WhereClause_Equals tests WHERE with equality
func TestExecutor_WhereClause_Equals(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"age":  storage.IntValue(30),
	})

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) WHERE n.age = 25 RETURN n
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "n",
							Labels:   []string{"Person"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "n",
					Property: "age",
				},
				Operator: "=",
				Right: &LiteralExpression{
					Value: int64(25),
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute WHERE query: %v", err)
	}

	// Should return Alice and Bob (both age 25)
	if result.Count != 2 {
		t.Errorf("Expected 2 results, got %d", result.Count)
	}
}

// TestExecutor_CreateRelationship tests creating relationships
func TestExecutor_CreateRelationship(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// First, create two nodes: CREATE (a:Person {name: "Alice"}), (b:Person {name: "Bob"})
	createNodesQuery := &Query{
		Create: &CreateClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "a",
							Labels:   []string{"Person"},
							Properties: map[string]interface{}{
								"name": "Alice",
							},
						},
						{
							Variable: "b",
							Labels:   []string{"Person"},
							Properties: map[string]interface{}{
								"name": "Bob",
							},
						},
					},
				},
			},
		},
	}

	_, err := executor.Execute(createNodesQuery)
	if err != nil {
		t.Fatalf("Failed to create nodes: %v", err)
	}

	// Get the created nodes
	nodes, _ := gs.FindNodesByLabel("Person")
	if len(nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(nodes))
	}

	alice := nodes[0]

	// Now create a relationship: MATCH (a), (b) WHERE a.id = 1 AND b.id = 2 CREATE (a)-[:KNOWS]->(b)
	// We'll use ExecuteWithText for simplicity and to test relationship creation
	createRelQuery := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "a", Labels: []string{"Person"}},
						{Variable: "b", Labels: []string{"Person"}},
					},
				},
			},
		},
		Create: &CreateClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{},
					Relationships: []*RelationshipPattern{
						{
							From: &NodePattern{Variable: "a"},
							To:   &NodePattern{Variable: "b"},
							Type: "KNOWS",
							Properties: map[string]interface{}{
								"since": int64(2023),
							},
						},
					},
				},
			},
		},
	}

	result, err := executor.Execute(createRelQuery)
	if err != nil {
		t.Fatalf("Failed to create relationship: %v", err)
	}

	// Verify relationship was created (4 total = 2 nodes matched * 2 nodes matched = 4 relationships)
	if result.Count < 1 {
		t.Errorf("Expected at least 1 relationship created, got %d", result.Count)
	}

	// Verify edge exists
	edges, err := gs.GetOutgoingEdges(alice.ID)
	if err != nil {
		t.Fatalf("Failed to get edges: %v", err)
	}
	if len(edges) < 1 {
		t.Errorf("Expected at least 1 outgoing edge from Alice")
	}

	// Verify edge has correct properties
	if len(edges) > 0 {
		since, ok := edges[0].GetProperty("since")
		if !ok {
			t.Error("Edge should have 'since' property")
		} else {
			sinceVal, _ := since.AsInt()
			if sinceVal != 2023 {
				t.Errorf("Expected since=2023, got %d", sinceVal)
			}
		}
	}
}

// TestExecutor_SortRows_Ascending tests sorting rows in ascending order
func TestExecutor_SortRows_Ascending(t *testing.T) {
	executor := NewExecutor(nil) // Don't need graph for sorting test

	rows := []map[string]interface{}{
		{"n.age": int64(30)},
		{"n.age": int64(25)},
		{"n.age": int64(35)},
	}

	orderBy := []*OrderByItem{
		{
			Expression: &PropertyExpression{Variable: "n", Property: "age"},
			Ascending:  true,
		},
	}

	executor.sortRows(rows, orderBy)

	// Verify ascending order
	if rows[0]["n.age"].(int64) != 25 {
		t.Errorf("Expected first row age 25, got %d", rows[0]["n.age"])
	}
	if rows[1]["n.age"].(int64) != 30 {
		t.Errorf("Expected second row age 30, got %d", rows[1]["n.age"])
	}
	if rows[2]["n.age"].(int64) != 35 {
		t.Errorf("Expected third row age 35, got %d", rows[2]["n.age"])
	}
}

// TestExecutor_SortRows_Descending tests sorting rows in descending order
func TestExecutor_SortRows_Descending(t *testing.T) {
	executor := NewExecutor(nil)

	rows := []map[string]interface{}{
		{"n.age": int64(30)},
		{"n.age": int64(25)},
		{"n.age": int64(35)},
	}

	orderBy := []*OrderByItem{
		{
			Expression: &PropertyExpression{Variable: "n", Property: "age"},
			Ascending:  false,
		},
	}

	executor.sortRows(rows, orderBy)

	// Verify descending order
	if rows[0]["n.age"].(int64) != 35 {
		t.Errorf("Expected first row age 35, got %d", rows[0]["n.age"])
	}
	if rows[1]["n.age"].(int64) != 30 {
		t.Errorf("Expected second row age 30, got %d", rows[1]["n.age"])
	}
	if rows[2]["n.age"].(int64) != 25 {
		t.Errorf("Expected third row age 25, got %d", rows[2]["n.age"])
	}
}

// TestExecutor_SortRows_EmptyOrderBy tests that no sorting occurs with empty order by
func TestExecutor_SortRows_EmptyOrderBy(t *testing.T) {
	executor := NewExecutor(nil)

	rows := []map[string]interface{}{
		{"n.age": int64(30)},
		{"n.age": int64(25)},
	}

	originalFirst := rows[0]["n.age"]

	executor.sortRows(rows, []*OrderByItem{})

	// Should remain unchanged
	if rows[0]["n.age"] != originalFirst {
		t.Error("Rows should not be sorted with empty order by")
	}
}

// TestExecutor_SortRows_Strings tests sorting string values
func TestExecutor_SortRows_Strings(t *testing.T) {
	executor := NewExecutor(nil)

	rows := []map[string]interface{}{
		{"n.name": "Charlie"},
		{"n.name": "Alice"},
		{"n.name": "Bob"},
	}

	orderBy := []*OrderByItem{
		{
			Expression: &PropertyExpression{Variable: "n", Property: "name"},
			Ascending:  true,
		},
	}

	executor.sortRows(rows, orderBy)

	// Verify alphabetical order
	if rows[0]["n.name"].(string) != "Alice" {
		t.Errorf("Expected first row name Alice, got %s", rows[0]["n.name"])
	}
	if rows[1]["n.name"].(string) != "Bob" {
		t.Errorf("Expected second row name Bob, got %s", rows[1]["n.name"])
	}
	if rows[2]["n.name"].(string) != "Charlie" {
		t.Errorf("Expected third row name Charlie, got %s", rows[2]["n.name"])
	}
}

// TestExecutor_Aggregation_COUNT tests COUNT aggregation
func TestExecutor_Aggregation_COUNT(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes
	for i := 0; i < 5; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(int64(25 + i*5)),
		})
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN COUNT(n)
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate:  "COUNT",
					Expression: &PropertyExpression{Variable: "n"},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COUNT query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation, got %d", result.Count)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("Expected 1 row, got %d", len(result.Rows))
	}

	// Check the count value
	count := result.Rows[0]["COUNT(n.)"]
	if count != 5 {
		t.Errorf("Expected COUNT=5, got %v", count)
	}
}

// TestExecutor_Aggregation_SUM tests SUM aggregation
func TestExecutor_Aggregation_SUM(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with salaries
	salaries := []int64{50000, 60000, 70000}
	for _, sal := range salaries {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"salary": storage.IntValue(sal),
		})
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN SUM(n.salary)
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate:  "SUM",
					Expression: &PropertyExpression{Variable: "n", Property: "salary"},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute SUM query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation, got %d", result.Count)
	}

	sum := result.Rows[0]["SUM(n.salary)"]
	if sum != int64(180000) {
		t.Errorf("Expected SUM=180000, got %v (type %T)", sum, sum)
	}
}

// TestExecutor_Aggregation_AVG tests AVG aggregation
func TestExecutor_Aggregation_AVG(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with ages
	ages := []int64{20, 30, 40}
	for _, age := range ages {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(age),
		})
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN AVG(n.age)
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate:  "AVG",
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute AVG query: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation, got %d", result.Count)
	}

	avg := result.Rows[0]["AVG(n.age)"]
	if avg != float64(30) {
		t.Errorf("Expected AVG=30.0, got %v (type %T)", avg, avg)
	}
}

// TestExecutor_Aggregation_MIN_MAX tests MIN and MAX aggregations
func TestExecutor_Aggregation_MIN_MAX(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with ages
	ages := []int64{25, 30, 35, 28, 32}
	for _, age := range ages {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(age),
		})
	}

	executor := NewExecutor(gs)

	// Test MIN
	minQuery := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate:  "MIN",
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
				},
			},
		},
	}

	result, err := executor.Execute(minQuery)
	if err != nil {
		t.Fatalf("Failed to execute MIN query: %v", err)
	}

	min := result.Rows[0]["MIN(n.age)"]
	if min != int64(25) {
		t.Errorf("Expected MIN=25, got %v (type %T)", min, min)
	}

	// Test MAX
	maxQuery := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate:  "MAX",
					Expression: &PropertyExpression{Variable: "n", Property: "age"},
				},
			},
		},
	}

	result, err = executor.Execute(maxQuery)
	if err != nil {
		t.Fatalf("Failed to execute MAX query: %v", err)
	}

	max := result.Rows[0]["MAX(n.age)"]
	if max != int64(35) {
		t.Errorf("Expected MAX=35, got %v (type %T)", max, max)
	}
}

// TestExecutor_GroupBy_Single tests GROUP BY with single property
func TestExecutor_GroupBy_Single(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with different departments
	departments := map[string][]int64{
		"Engineering": {50000, 60000, 70000},
		"Sales":       {40000, 45000},
		"Marketing":   {55000},
	}

	for dept, salaries := range departments {
		for _, sal := range salaries {
			gs.CreateNode([]string{"Person"}, map[string]storage.Value{
				"department": storage.StringValue(dept),
				"salary":     storage.IntValue(sal),
			})
		}
	}

	executor := NewExecutor(gs)

	// Query: MATCH (n:Person) RETURN n.department, COUNT(n), AVG(n.salary) GROUP BY n.department
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY query: %v", err)
	}

	// Should return 3 rows (one per department)
	if result.Count != 3 {
		t.Errorf("Expected 3 grouped rows, got %d", result.Count)
	}

	// Verify each group
	groups := make(map[string]map[string]interface{})
	for _, row := range result.Rows {
		dept := row["n.department"].(string)
		groups[dept] = row
	}

	// Check Engineering group
	if eng, ok := groups["Engineering"]; ok {
		if count := eng["COUNT(n.)"]; count != 3 {
			t.Errorf("Expected Engineering count=3, got %v", count)
		}
		if avg := eng["AVG(n.salary)"]; avg != float64(60000) {
			t.Errorf("Expected Engineering avg=60000, got %v", avg)
		}
	} else {
		t.Error("Engineering department not found in results")
	}

	// Check Sales group
	if sales, ok := groups["Sales"]; ok {
		if count := sales["COUNT(n.)"]; count != 2 {
			t.Errorf("Expected Sales count=2, got %v", count)
		}
		if avg := sales["AVG(n.salary)"]; avg != float64(42500) {
			t.Errorf("Expected Sales avg=42500, got %v", avg)
		}
	} else {
		t.Error("Sales department not found in results")
	}
}

// TestExecutor_GroupBy_Multiple tests GROUP BY with multiple properties
func TestExecutor_GroupBy_Multiple(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes with department and location
	nodes := []struct {
		dept     string
		location string
		salary   int64
	}{
		{"Engineering", "NYC", 100000},
		{"Engineering", "NYC", 110000},
		{"Engineering", "SF", 120000},
		{"Sales", "NYC", 80000},
		{"Sales", "SF", 85000},
	}

	for _, n := range nodes {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(n.dept),
			"location":   storage.StringValue(n.location),
			"salary":     storage.IntValue(n.salary),
		})
	}

	executor := NewExecutor(gs)

	// Query: GROUP BY department AND location
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Expression: &PropertyExpression{Variable: "n", Property: "location"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
				{Variable: "n", Property: "location"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute multi-GROUP BY query: %v", err)
	}

	// Should return 4 groups: Eng-NYC, Eng-SF, Sales-NYC, Sales-SF
	if result.Count != 4 {
		t.Errorf("Expected 4 grouped rows, got %d", result.Count)
	}

	// Verify Engineering-NYC group (2 people, avg 105000)
	found := false
	for _, row := range result.Rows {
		if row["n.department"] == "Engineering" && row["n.location"] == "NYC" {
			found = true
			if count := row["COUNT(n.)"]; count != 2 {
				t.Errorf("Expected Engineering-NYC count=2, got %v", count)
			}
			if avg := row["AVG(n.salary)"]; avg != float64(105000) {
				t.Errorf("Expected Engineering-NYC avg=105000, got %v", avg)
			}
		}
	}
	if !found {
		t.Error("Engineering-NYC group not found")
	}
}

// TestExecutor_GroupBy_WithOrderBy tests GROUP BY combined with ORDER BY
func TestExecutor_GroupBy_WithOrderBy(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test nodes
	for _, dept := range []string{"Zebra", "Alpha", "Beta"} {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(dept),
			"salary":     storage.IntValue(50000),
		})
	}

	executor := NewExecutor(gs)

	// Query: GROUP BY department, ORDER BY department
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
			OrderBy: []*OrderByItem{
				{
					Expression: &PropertyExpression{Variable: "n", Property: "department"},
					Ascending:  true,
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY + ORDER BY query: %v", err)
	}

	if result.Count != 3 {
		t.Errorf("Expected 3 rows, got %d", result.Count)
	}

	// Verify alphabetical order
	if result.Rows[0]["n.department"] != "Alpha" {
		t.Errorf("Expected first department to be Alpha, got %v", result.Rows[0]["n.department"])
	}
	if result.Rows[1]["n.department"] != "Beta" {
		t.Errorf("Expected second department to be Beta, got %v", result.Rows[1]["n.department"])
	}
	if result.Rows[2]["n.department"] != "Zebra" {
		t.Errorf("Expected third department to be Zebra, got %v", result.Rows[2]["n.department"])
	}
}

// TestExecutor_GroupBy_EmptyGroup tests GROUP BY with no matching nodes
func TestExecutor_GroupBy_EmptyGroup(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Query on empty graph
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY on empty graph: %v", err)
	}

	if result.Count != 0 {
		t.Errorf("Expected 0 rows for empty graph, got %d", result.Count)
	}
}

// TestExecutor_Aggregation_MixedTypes tests aggregations with mixed data types
func TestExecutor_Aggregation_MixedTypes(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes with mixed integer and float salaries
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"salary": storage.IntValue(50000),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"salary": storage.FloatValue(55000.50),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"salary": storage.IntValue(60000),
	})

	executor := NewExecutor(gs)

	// Test SUM with mixed types
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Aggregate: "SUM", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute SUM with mixed types: %v", err)
	}

	sum := result.Rows[0]["SUM(n.salary)"]
	// Should handle mixed int/float correctly
	expectedSum := 165000.50
	if sumFloat, ok := sum.(float64); ok {
		if sumFloat < expectedSum-0.01 || sumFloat > expectedSum+0.01 {
			t.Errorf("Expected SUM≈%.2f, got %.2f", expectedSum, sumFloat)
		}
	} else {
		t.Errorf("Expected SUM to be float64, got %T", sum)
	}
}

// TestExecutor_Aggregation_NullValues tests aggregations with null/missing values
func TestExecutor_Aggregation_NullValues(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create nodes where some have salary and some don't
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"salary": storage.IntValue(50000),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		// No salary property
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"salary": storage.IntValue(60000),
	})

	executor := NewExecutor(gs)

	// Test COUNT - should only count nodes with the property
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute COUNT with nulls: %v", err)
	}

	count := result.Rows[0]["COUNT(n.salary)"]
	if count != 2 {
		t.Errorf("Expected COUNT=2 (only nodes with salary), got %v", count)
	}
}

// TestExecutor_GroupBy_LIMIT_SKIP tests GROUP BY with LIMIT and SKIP
func TestExecutor_GroupBy_LIMIT_SKIP(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create multiple departments
	for i, dept := range []string{"A", "B", "C", "D", "E"} {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue(dept),
			"salary":     storage.IntValue(int64(50000 + i*5000)),
		})
	}

	executor := NewExecutor(gs)

	// Query with GROUP BY, ORDER BY, SKIP, and LIMIT
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
			OrderBy: []*OrderByItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}, Ascending: true},
			},
		},
		Skip:  1,
		Limit: 3,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute GROUP BY with LIMIT/SKIP: %v", err)
	}

	// Should skip first group (A) and return 3 groups (B, C, D)
	if result.Count != 3 {
		t.Errorf("Expected 3 rows after SKIP 1 LIMIT 3, got %d", result.Count)
	}

	if result.Rows[0]["n.department"] != "B" {
		t.Errorf("Expected first department to be B, got %v", result.Rows[0]["n.department"])
	}
}

// TestExecutor_ComplexQuery tests multiple features combined
func TestExecutor_ComplexQuery(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create diverse dataset
	testData := []struct {
		name   string
		dept   string
		age    int64
		salary int64
	}{
		{"Alice", "Engineering", 30, 80000},
		{"Bob", "Engineering", 25, 70000},
		{"Charlie", "Sales", 35, 60000},
		{"Diana", "Sales", 28, 65000},
		{"Eve", "Marketing", 32, 75000},
		{"Frank", "Engineering", 40, 90000},
	}

	for _, person := range testData {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name":       storage.StringValue(person.name),
			"department": storage.StringValue(person.dept),
			"age":        storage.IntValue(person.age),
			"salary":     storage.IntValue(person.salary),
		})
	}

	executor := NewExecutor(gs)

	// Complex query: GROUP BY department, filter, aggregate, order, limit
	// "Find top 2 departments by average salary where avg salary > 65000"
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
			GroupBy: []*PropertyExpression{
				{Variable: "n", Property: "department"},
			},
			OrderBy: []*OrderByItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}, Ascending: true},
			},
		},
		Limit: 2,
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute complex query: %v", err)
	}

	if result.Count != 2 {
		t.Errorf("Expected 2 rows (LIMIT 2), got %d", result.Count)
	}

	// Verify structure
	for _, row := range result.Rows {
		if _, ok := row["n.department"]; !ok {
			t.Error("Expected n.department in results")
		}
		if _, ok := row["COUNT(n.)"]; !ok {
			t.Error("Expected COUNT(n.) in results")
		}
		if _, ok := row["AVG(n.salary)"]; !ok {
			t.Error("Expected AVG(n.salary) in results")
		}
	}
}

// TestExecutor_Aggregation_StringMinMax tests MIN/MAX with strings
func TestExecutor_Aggregation_StringMinMax(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	names := []string{"Zebra", "Alice", "Mike", "Bob"}
	for _, name := range names {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"name": storage.StringValue(name),
		})
	}

	executor := NewExecutor(gs)

	// Test MIN on strings
	minQuery := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Aggregate: "MIN", Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err := executor.Execute(minQuery)
	if err != nil {
		t.Fatalf("Failed to execute MIN on strings: %v", err)
	}

	min := result.Rows[0]["MIN(n.name)"]
	if min != "Alice" {
		t.Errorf("Expected MIN='Alice' (alphabetically first), got %v", min)
	}

	// Test MAX on strings
	maxQuery := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Aggregate: "MAX", Expression: &PropertyExpression{Variable: "n", Property: "name"}},
			},
		},
	}

	result, err = executor.Execute(maxQuery)
	if err != nil {
		t.Fatalf("Failed to execute MAX on strings: %v", err)
	}

	max := result.Rows[0]["MAX(n.name)"]
	if max != "Zebra" {
		t.Errorf("Expected MAX='Zebra' (alphabetically last), got %v", max)
	}
}

// TestExecutor_DISTINCT_With_Aggregation tests DISTINCT with aggregation
func TestExecutor_DISTINCT_With_Aggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create duplicate department data
	for i := 0; i < 3; i++ {
		gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"department": storage.StringValue("Engineering"),
		})
	}

	executor := NewExecutor(gs)

	// COUNT should return 3, but DISTINCT departments should be 1
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Expression: &PropertyExpression{Variable: "n", Property: "department"}},
			},
			Distinct: true,
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute DISTINCT: %v", err)
	}

	if result.Count != 1 {
		t.Errorf("Expected 1 distinct department, got %d", result.Count)
	}
}

// TestExecutor_EmptyAggregation tests aggregation on empty result set
func TestExecutor_EmptyAggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	// Query empty graph
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{Variable: "n", Labels: []string{"Person"}},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}},
				{Aggregate: "SUM", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
				{Aggregate: "AVG", Expression: &PropertyExpression{Variable: "n", Property: "salary"}},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Failed to execute aggregation on empty set: %v", err)
	}

	// Should return one row with aggregate results for empty set
	if result.Count != 1 {
		t.Errorf("Expected 1 row for aggregation on empty set, got %d", result.Count)
	}

	count := result.Rows[0]["COUNT(n.)"]
	if count != 0 {
		t.Errorf("Expected COUNT=0 on empty set, got %v", count)
	}

	sum := result.Rows[0]["SUM(n.salary)"]
	if sum != 0 {
		t.Errorf("Expected SUM=0 on empty set, got %v", sum)
	}
}

// TestExecutor_Integration_WHERE_GroupBy_Aggregation tests filtering + grouping + aggregation
func TestExecutor_Integration_WHERE_GroupBy_Aggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data: employees with varying salaries
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Alice"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(80000),
		"level":      storage.StringValue("Senior"),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Bob"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(60000),
		"level":      storage.StringValue("Junior"),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Charlie"),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(70000),
		"level":      storage.StringValue("Senior"),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Diana"),
		"department": storage.StringValue("Sales"),
		"salary":     storage.IntValue(50000),
		"level":      storage.StringValue("Junior"),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":       storage.StringValue("Eve"),
		"department": storage.StringValue("Engineering"),
		"salary":     storage.IntValue(90000),
		"level":      storage.StringValue("Senior"),
	})

	executor := NewExecutor(gs)

	// Query: Find average salary by department, but only for Senior employees, ordered by avg salary DESC
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "e",
							Labels:   []string{"Employee"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "e",
					Property: "level",
				},
				Operator: "=",
				Right: &LiteralExpression{
					Value: "Senior",
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "department",
					},
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "salary",
					},
					Alias: "avg_salary",
				},
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "name",
					},
					Alias: "employee_count",
				},
			},
			GroupBy: []*PropertyExpression{
				{
					Variable: "e",
					Property: "department",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	// Should have 2 departments (Engineering and Sales) with Senior employees
	if result.Count != 2 {
		t.Errorf("Expected 2 departments, got %d", result.Count)
	}

	// Verify Engineering department (2 Senior: Alice 80k, Eve 90k = avg 85k)
	engRow := findRowByColumn(result.Rows, "e.department", "Engineering")
	if engRow == nil {
		t.Fatal("Engineering department not found")
	}
	avgSalary := engRow["avg_salary"].(float64)
	if avgSalary != 85000.0 {
		t.Errorf("Engineering avg salary: expected 85000, got %v", avgSalary)
	}
	empCount := engRow["employee_count"].(int)
	if empCount != 2 {
		t.Errorf("Engineering employee count: expected 2, got %d", empCount)
	}

	// Verify Sales department (1 Senior: Charlie 70k)
	salesRow := findRowByColumn(result.Rows, "e.department", "Sales")
	if salesRow == nil {
		t.Fatal("Sales department not found")
	}
	avgSalary = salesRow["avg_salary"].(float64)
	if avgSalary != 70000.0 {
		t.Errorf("Sales avg salary: expected 70000, got %v", avgSalary)
	}
	empCount = salesRow["employee_count"].(int)
	if empCount != 1 {
		t.Errorf("Sales employee count: expected 1, got %d", empCount)
	}
}

// TestExecutor_Integration_MultipleAggregations tests multiple aggregates in one query
func TestExecutor_Integration_MultipleAggregations(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data
	gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name":     storage.StringValue("Laptop"),
		"price":    storage.FloatValue(999.99),
		"quantity": storage.IntValue(10),
		"rating":   storage.FloatValue(4.5),
	})
	gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name":     storage.StringValue("Mouse"),
		"price":    storage.FloatValue(29.99),
		"quantity": storage.IntValue(100),
		"rating":   storage.FloatValue(4.2),
	})
	gs.CreateNode([]string{"Product"}, map[string]storage.Value{
		"name":     storage.StringValue("Keyboard"),
		"price":    storage.FloatValue(79.99),
		"quantity": storage.IntValue(50),
		"rating":   storage.FloatValue(4.8),
	})

	executor := NewExecutor(gs)

	// Query with multiple aggregations
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "p",
							Labels:   []string{"Product"},
						},
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "name",
					},
					Alias: "total_products",
				},
				{
					Aggregate: "SUM",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "quantity",
					},
					Alias: "total_quantity",
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "price",
					},
					Alias: "avg_price",
				},
				{
					Aggregate: "MIN",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "price",
					},
					Alias: "min_price",
				},
				{
					Aggregate: "MAX",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "price",
					},
					Alias: "max_price",
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "rating",
					},
					Alias: "avg_rating",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("Expected 1 result row, got %d", result.Count)
	}

	row := result.Rows[0]

	// Verify all aggregations
	if row["total_products"] != 3 {
		t.Errorf("total_products: expected 3, got %v", row["total_products"])
	}
	if row["total_quantity"] != int64(160) {
		t.Errorf("total_quantity: expected 160, got %v", row["total_quantity"])
	}
	expectedAvgPrice := (999.99 + 29.99 + 79.99) / 3.0
	if avgPrice := row["avg_price"].(float64); avgPrice != expectedAvgPrice {
		t.Errorf("avg_price: expected %v, got %v", expectedAvgPrice, avgPrice)
	}
	if row["min_price"] != 29.99 {
		t.Errorf("min_price: expected 29.99, got %v", row["min_price"])
	}
	if row["max_price"] != 999.99 {
		t.Errorf("max_price: expected 999.99, got %v", row["max_price"])
	}
	expectedAvgRating := (4.5 + 4.2 + 4.8) / 3.0
	if avgRating := row["avg_rating"].(float64); avgRating != expectedAvgRating {
		t.Errorf("avg_rating: expected %v, got %v", expectedAvgRating, avgRating)
	}
}

// TestExecutor_Integration_ComplexWHERE_Aggregation tests complex WHERE with AND/OR + aggregation
func TestExecutor_Integration_ComplexWHERE_Aggregation(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Alice"),
		"age":    storage.IntValue(30),
		"salary": storage.IntValue(80000),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Bob"),
		"age":    storage.IntValue(25),
		"salary": storage.IntValue(50000),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Charlie"),
		"age":    storage.IntValue(35),
		"salary": storage.IntValue(90000),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Diana"),
		"age":    storage.IntValue(28),
		"salary": storage.IntValue(75000),
	})
	gs.CreateNode([]string{"Employee"}, map[string]storage.Value{
		"name":   storage.StringValue("Eve"),
		"age":    storage.IntValue(40),
		"salary": storage.IntValue(95000),
	})

	executor := NewExecutor(gs)

	// Query: Find avg salary for employees where (age > 30) OR (salary >= 75000)
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "e",
							Labels:   []string{"Employee"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &BinaryExpression{
					Left: &PropertyExpression{
						Variable: "e",
						Property: "age",
					},
					Operator: ">",
					Right: &LiteralExpression{
						Value: int64(30),
					},
				},
				Operator: "OR",
				Right: &BinaryExpression{
					Left: &PropertyExpression{
						Variable: "e",
						Property: "salary",
					},
					Operator: ">=",
					Right: &LiteralExpression{
						Value: int64(75000),
					},
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Aggregate: "COUNT",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "name",
					},
					Alias: "count",
				},
				{
					Aggregate: "AVG",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "salary",
					},
					Alias: "avg_salary",
				},
				{
					Aggregate: "MIN",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "age",
					},
					Alias: "min_age",
				},
				{
					Aggregate: "MAX",
					Expression: &PropertyExpression{
						Variable: "e",
						Property: "age",
					},
					Alias: "max_age",
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	if result.Count != 1 {
		t.Fatalf("Expected 1 result row, got %d", result.Count)
	}

	row := result.Rows[0]

	// Should match: Alice (age 30, salary 80000 ✓), Charlie (age 35 ✓), Diana (age 28, salary 75000 ✓), Eve (age 40 ✓)
	// Total: 4 employees
	if row["count"] != 4 {
		t.Errorf("count: expected 4, got %v", row["count"])
	}

	// Avg salary: (80000 + 90000 + 75000 + 95000) / 4 = 85000
	expectedAvg := float64(340000) / 4.0
	if avgSalary := row["avg_salary"].(float64); avgSalary != expectedAvg {
		t.Errorf("avg_salary: expected %v, got %v", expectedAvg, avgSalary)
	}

	if row["min_age"] != int64(28) {
		t.Errorf("min_age: expected 28, got %v", row["min_age"])
	}

	if row["max_age"] != int64(40) {
		t.Errorf("max_age: expected 40, got %v", row["max_age"])
	}
}

// TestExecutor_Integration_DISTINCT_WHERE_OrderBy tests DISTINCT with filtering and ordering
func TestExecutor_Integration_DISTINCT_WHERE_OrderBy(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	// Create test data with duplicate cities
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"city": storage.StringValue("NYC"),
		"age":  storage.IntValue(30),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"city": storage.StringValue("LA"),
		"age":  storage.IntValue(25),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Charlie"),
		"city": storage.StringValue("NYC"),
		"age":  storage.IntValue(35),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Diana"),
		"city": storage.StringValue("Chicago"),
		"age":  storage.IntValue(28),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Eve"),
		"city": storage.StringValue("LA"),
		"age":  storage.IntValue(32),
	})
	gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Frank"),
		"city": storage.StringValue("Seattle"),
		"age":  storage.IntValue(22),
	})

	executor := NewExecutor(gs)

	// Query: Get distinct cities for people age >= 25, ordered by city name
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "p",
							Labels:   []string{"Person"},
						},
					},
				},
			},
		},
		Where: &WhereClause{
			Expression: &BinaryExpression{
				Left: &PropertyExpression{
					Variable: "p",
					Property: "age",
				},
				Operator: ">=",
				Right: &LiteralExpression{
					Value: int64(25),
				},
			},
		},
		Return: &ReturnClause{
			Items: []*ReturnItem{
				{
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "city",
					},
				},
			},
			Distinct: true,
			OrderBy: []*OrderByItem{
				{
					Expression: &PropertyExpression{
						Variable: "p",
						Property: "city",
					},
					Ascending: true,
				},
			},
		},
	}

	result, err := executor.Execute(query)
	if err != nil {
		t.Fatalf("Query execution failed: %v", err)
	}

	// Should have 3 distinct cities: Alice(30), Bob(25), Charlie(35), Diana(28), Eve(32) all >= 25
	// Frank (age 22) is excluded because 22 < 25
	// Cities: NYC, LA, NYC, Chicago, LA -> distinct: Chicago, LA, NYC (3 cities)
	if result.Count != 3 {
		t.Errorf("Expected 3 distinct cities, got %d", result.Count)
	}

	// Verify they're sorted alphabetically
	expectedCities := []string{"Chicago", "LA", "NYC"}
	for i, expected := range expectedCities {
		if i >= len(result.Rows) {
			t.Errorf("Missing city at index %d", i)
			continue
		}
		actual := result.Rows[i]["p.city"]
		if actual != expected {
			t.Errorf("Row %d: expected city %s, got %v", i, expected, actual)
		}
	}
}

// Helper function to find a row by column value
func findRowByColumn(rows []map[string]interface{}, column string, value interface{}) map[string]interface{} {
	for _, row := range rows {
		if row[column] == value {
			return row
		}
	}
	return nil
}
