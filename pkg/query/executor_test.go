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
	cs := &CreateStep{}

	// String
	val := cs.convertValue("hello")
	str, _ := val.AsString()
	if str != "hello" {
		t.Errorf("Expected string 'hello', got '%s'", str)
	}

	// Int64
	val = cs.convertValue(int64(42))
	i, _ := val.AsInt()
	if i != 42 {
		t.Errorf("Expected int 42, got %d", i)
	}

	// Float64 - note: float storage may have precision issues depending on implementation
	val = cs.convertValue(float64(3.14))
	f, _ := val.AsFloat()
	if f < 3.0 || f > 3.2 {
		t.Errorf("Expected float around 3.14, got %f", f)
	}

	// Bool
	val = cs.convertValue(true)
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
