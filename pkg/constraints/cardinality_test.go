package constraints

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestCardinalityConstraint_MinEdges tests minimum edge count validation
func TestCardinalityConstraint_MinEdges(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create Account that must have at least 1 OWNS edge
	account1, _ := graph.CreateNode([]string{"Account"}, map[string]storage.Value{
		"name": storage.StringValue("Account1"),
	})
	account2, _ := graph.CreateNode([]string{"Account"}, map[string]storage.Value{
		"name": storage.StringValue("Account2"),
	})
	owner, _ := graph.CreateNode([]string{"Person"}, nil)

	// account1 has 1 OWNS edge (valid)
	graph.CreateEdge(owner.ID, account1.ID, "OWNS", nil, 1.0)

	// account2 has 0 OWNS edges (invalid)

	constraint := &CardinalityConstraint{
		NodeLabel: "Account",
		EdgeType:  "OWNS",
		Direction: Incoming,
		Min:       1,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (account2)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 {
		if violations[0].NodeID == nil || *violations[0].NodeID != account2.ID {
			t.Errorf("Expected violation for account2")
		}
		if violations[0].Severity != Error {
			t.Errorf("Expected Error severity")
		}
	}
}

// TestCardinalityConstraint_MaxEdges tests maximum edge count validation
func TestCardinalityConstraint_MaxEdges(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Person can have at most 1 manager
	person1, _ := graph.CreateNode([]string{"Person"}, nil)
	person2, _ := graph.CreateNode([]string{"Person"}, nil)
	manager1, _ := graph.CreateNode([]string{"Manager"}, nil)
	manager2, _ := graph.CreateNode([]string{"Manager"}, nil)

	// person1 has 1 REPORTS_TO edge (valid)
	graph.CreateEdge(person1.ID, manager1.ID, "REPORTS_TO", nil, 1.0)

	// person2 has 2 REPORTS_TO edges (invalid - too many managers!)
	graph.CreateEdge(person2.ID, manager1.ID, "REPORTS_TO", nil, 1.0)
	graph.CreateEdge(person2.ID, manager2.ID, "REPORTS_TO", nil, 1.0)

	constraint := &CardinalityConstraint{
		NodeLabel: "Person",
		EdgeType:  "REPORTS_TO",
		Direction: Outgoing,
		Max:       1,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (person2)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && (violations[0].NodeID == nil || *violations[0].NodeID != person2.ID) {
		t.Errorf("Expected violation for person2")
	}
}

// TestCardinalityConstraint_ExactCount tests exact edge count
func TestCardinalityConstraint_ExactCount(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Triangle must have exactly 3 EDGE_OF edges
	triangle1, _ := graph.CreateNode([]string{"Triangle"}, nil)
	triangle2, _ := graph.CreateNode([]string{"Triangle"}, nil)
	edge1, _ := graph.CreateNode([]string{"Edge"}, nil)
	edge2, _ := graph.CreateNode([]string{"Edge"}, nil)
	edge3, _ := graph.CreateNode([]string{"Edge"}, nil)

	// triangle1 has exactly 3 edges (valid)
	graph.CreateEdge(edge1.ID, triangle1.ID, "EDGE_OF", nil, 1.0)
	graph.CreateEdge(edge2.ID, triangle1.ID, "EDGE_OF", nil, 1.0)
	graph.CreateEdge(edge3.ID, triangle1.ID, "EDGE_OF", nil, 1.0)

	// triangle2 has only 2 edges (invalid)
	graph.CreateEdge(edge1.ID, triangle2.ID, "EDGE_OF", nil, 1.0)
	graph.CreateEdge(edge2.ID, triangle2.ID, "EDGE_OF", nil, 1.0)

	constraint := &CardinalityConstraint{
		NodeLabel: "Triangle",
		EdgeType:  "EDGE_OF",
		Direction: Incoming,
		Min:       3,
		Max:       3,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (triangle2)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && (violations[0].NodeID == nil || *violations[0].NodeID != triangle2.ID) {
		t.Errorf("Expected violation for triangle2")
	}
}

// TestCardinalityConstraint_AnyDirection tests counting edges in any direction
func TestCardinalityConstraint_AnyDirection(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// User must have at least 1 FRIEND edge (either direction)
	user1, _ := graph.CreateNode([]string{"User"}, nil)
	user2, _ := graph.CreateNode([]string{"User"}, nil)
	user3, _ := graph.CreateNode([]string{"User"}, nil)

	// user1 has outgoing friend
	graph.CreateEdge(user1.ID, user2.ID, "FRIEND", nil, 1.0)

	// user2 has incoming friend (from user1)

	// user3 has no friends (invalid)

	constraint := &CardinalityConstraint{
		NodeLabel: "User",
		EdgeType:  "FRIEND",
		Direction: Any,
		Min:       1,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (user3)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && (violations[0].NodeID == nil || *violations[0].NodeID != user3.ID) {
		t.Errorf("Expected violation for user3")
	}
}

// TestCardinalityConstraint_AllEdgeTypes tests constraint without specific edge type
func TestCardinalityConstraint_AllEdgeTypes(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Node must have at least 1 outgoing edge of any type
	node1, _ := graph.CreateNode([]string{"Node"}, nil)
	node2, _ := graph.CreateNode([]string{"Node"}, nil)
	target, _ := graph.CreateNode([]string{"Target"}, nil)

	// node1 has an edge
	graph.CreateEdge(node1.ID, target.ID, "CONNECTS_TO", nil, 1.0)

	// node2 has no edges (invalid)

	constraint := &CardinalityConstraint{
		NodeLabel: "Node",
		EdgeType:  "", // Any edge type
		Direction: Outgoing,
		Min:       1,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (node2)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && (violations[0].NodeID == nil || *violations[0].NodeID != node2.ID) {
		t.Errorf("Expected violation for node2")
	}
}

// TestCardinalityConstraint_ZeroMin tests that Min=0 means optional
func TestCardinalityConstraint_ZeroMin(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Optional edge (0 or more)
	node, _ := graph.CreateNode([]string{"Node"}, nil)

	constraint := &CardinalityConstraint{
		NodeLabel: "Node",
		EdgeType:  "OPTIONAL_EDGE",
		Direction: Outgoing,
		Min:       0,
		Max:       5,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have no violations (0 is allowed)
	if len(violations) != 0 {
		t.Errorf("Expected 0 violations for Min=0, got %d", len(violations))
	}

	_ = node // Use node to avoid unused variable error
}

// TestCardinalityConstraint_EmptyGraph tests empty graph
func TestCardinalityConstraint_EmptyGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	constraint := &CardinalityConstraint{
		NodeLabel: "Node",
		EdgeType:  "EDGE",
		Direction: Outgoing,
		Min:       1,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 0 {
		t.Errorf("Expected 0 violations for empty graph, got %d", len(violations))
	}
}
