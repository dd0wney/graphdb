package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

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
							Properties: map[string]any{
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
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
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
							Properties: map[string]any{
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
	_, _ = gs.CreateNode([]string{"Person"}, map[string]storage.Value{
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
							Properties: map[string]any{
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
							Properties: map[string]any{
								"name": "Alice",
							},
						},
						{
							Variable: "b",
							Labels:   []string{"Person"},
							Properties: map[string]any{
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
	nodes, _ := gs.FindNodesByLabelAcrossTenants("Person")
	if len(nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(nodes))
	}

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
							Properties: map[string]any{
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

	// Verify KNOWS edges exist between the two Person nodes. Which node
	// ends up as the edge source depends on the executor's MATCH iteration
	// order — Go map iteration is non-deterministic, so a previous version
	// of this test asserted "Alice has outgoing edges" via nodes[0] and
	// flaked on Linux when nodes[0] happened to be Bob, with the executor
	// having created edges only from Alice. Enumerate all Person nodes
	// instead; the contract under test is "MATCH+CREATE produces KNOWS
	// edges with the right properties," not "the edge source happens to
	// be the node we picked first."
	var knowsEdges []*storage.Edge
	for _, n := range nodes {
		out, err := gs.GetOutgoingEdges(n.ID)
		if err != nil {
			t.Fatalf("Failed to get edges for node %d: %v", n.ID, err)
		}
		for _, e := range out {
			if e.Type == "KNOWS" {
				knowsEdges = append(knowsEdges, e)
			}
		}
	}
	if len(knowsEdges) < 1 {
		t.Errorf("Expected at least 1 KNOWS edge across both Person nodes, got %d", len(knowsEdges))
	}

	// Verify edge has correct properties (all KNOWS edges share the same
	// template, so checking the first one is sufficient).
	if len(knowsEdges) > 0 {
		since, ok := knowsEdges[0].GetProperty("since")
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
