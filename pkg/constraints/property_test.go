package constraints

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func setupTestGraph(t *testing.T) *storage.GraphStorage {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create GraphStorage: %v", err)
	}
	return gs
}

// TestPropertyConstraint_Required tests that required properties are validated
func TestPropertyConstraint_Required(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create node without required property
	node1, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	// Create node with required property
	node2, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name":  storage.StringValue("Bob"),
		"email": storage.StringValue("bob@example.com"),
	})

	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (node1 missing email)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 {
		if violations[0].NodeID == nil || *violations[0].NodeID != node1.ID {
			t.Errorf("Expected violation for node %d, got %v", node1.ID, violations[0].NodeID)
		}
		if violations[0].Severity != Error {
			t.Errorf("Expected Error severity, got %v", violations[0].Severity)
		}
	}

	// node2 should not be in violations
	for _, v := range violations {
		if v.NodeID != nil && *v.NodeID == node2.ID {
			t.Errorf("Node2 should not have violations")
		}
	}
}

// TestPropertyConstraint_Type tests type validation
func TestPropertyConstraint_Type(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create node with correct type
	node1, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.IntValue(25),
	})

	// Create node with wrong type (string instead of int)
	node2, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.StringValue("twenty-five"),
	})

	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "age",
		Type:         storage.TypeInt,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 1 violation (node2 wrong type)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 {
		if violations[0].NodeID == nil || *violations[0].NodeID != node2.ID {
			t.Errorf("Expected violation for node %d", node2.ID)
		}
	}

	// node1 should not have violations
	for _, v := range violations {
		if v.NodeID != nil && *v.NodeID == node1.ID {
			t.Errorf("Node1 should not have violations")
		}
	}
}

// TestPropertyConstraint_Range tests range validation for integers
func TestPropertyConstraint_IntRange(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create nodes with different ages
	node1, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.IntValue(25), // Valid
	})
	node2, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.IntValue(5), // Too low
	})
	node3, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.IntValue(150), // Too high
	})
	node4, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.IntValue(18), // Valid (boundary)
	})

	minAge := storage.IntValue(18)
	maxAge := storage.IntValue(120)

	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "age",
		Type:         storage.TypeInt,
		Min:          &minAge,
		Max:          &maxAge,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have 2 violations (node2, node3)
	if len(violations) != 2 {
		t.Errorf("Expected 2 violations, got %d", len(violations))
	}

	violatedNodes := make(map[uint64]bool)
	for _, v := range violations {
		if v.NodeID != nil {
			violatedNodes[*v.NodeID] = true
		}
	}

	if !violatedNodes[node2.ID] {
		t.Errorf("Expected violation for node2")
	}
	if !violatedNodes[node3.ID] {
		t.Errorf("Expected violation for node3")
	}
	if violatedNodes[node1.ID] {
		t.Errorf("Node1 should not have violations")
	}
	if violatedNodes[node4.ID] {
		t.Errorf("Node4 should not have violations (boundary)")
	}
}

// TestPropertyConstraint_FloatRange tests range validation for floats
func TestPropertyConstraint_FloatRange(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	node1, _ := graph.CreateNode([]string{"Product"}, map[string]storage.Value{
		"price": storage.FloatValue(19.99), // Valid
	})
	node2, _ := graph.CreateNode([]string{"Product"}, map[string]storage.Value{
		"price": storage.FloatValue(-5.0), // Invalid (negative)
	})

	minPrice := storage.FloatValue(0.0)

	constraint := &PropertyConstraint{
		NodeLabel:    "Product",
		PropertyName: "price",
		Type:         storage.TypeFloat,
		Min:          &minPrice,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && (violations[0].NodeID == nil || *violations[0].NodeID != node2.ID) {
		t.Errorf("Expected violation for node2")
	}

	for _, v := range violations {
		if v.NodeID != nil && *v.NodeID == node1.ID {
			t.Errorf("Node1 should not have violations")
		}
	}
}

// TestPropertyConstraint_NoLabel tests nodes without the target label are ignored
func TestPropertyConstraint_NoLabel(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create User without email
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	// Create Admin without email (different label, should be ignored)
	graph.CreateNode([]string{"Admin"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})

	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should only have 1 violation (User node)
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}
}

// TestPropertyConstraint_EmptyGraph tests validation on empty graph
func TestPropertyConstraint_EmptyGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 0 {
		t.Errorf("Expected 0 violations for empty graph, got %d", len(violations))
	}
}

// TestPropertyConstraint_MultipleViolations tests node with multiple constraint violations
func TestPropertyConstraint_MultipleViolations(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Node with age that's wrong type AND out of range
	node, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"age": storage.StringValue("invalid"),
	})

	minAge := storage.IntValue(18)
	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "age",
		Type:         storage.TypeInt,
		Required:     true,
		Min:          &minAge,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should have at least 1 violation (type mismatch)
	if len(violations) == 0 {
		t.Error("Expected violations for type mismatch")
	}

	if len(violations) > 0 && (violations[0].NodeID == nil || *violations[0].NodeID != node.ID) {
		t.Errorf("Expected violation for node %d", node.ID)
	}
}
