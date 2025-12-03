package constraints

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestUniquePropertyConstraint_GlobalScope tests global uniqueness
func TestUniquePropertyConstraint_GlobalScope(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create nodes with unique IDs
	node1, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("bob@example.com"),
	})
	// Create a duplicate
	node3, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"), // Duplicate!
	})

	constraint := &UniquePropertyConstraint{
		PropertyKey: "email",
		Scope:       ScopeGlobal,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 {
		// The violation should be for one of the duplicate nodes (node1 or node3)
		// Due to map iteration order, we can't guarantee which one is flagged
		violatedNodeID := *violations[0].NodeID
		if violatedNodeID != node1.ID && violatedNodeID != node3.ID {
			t.Errorf("Expected violation for node1 (ID=%d) or node3 (ID=%d), got NodeID=%d",
				node1.ID, node3.ID, violatedNodeID)
		}
		if violations[0].Type != UniquenessViolation {
			t.Errorf("Expected UniquenessViolation, got %v", violations[0].Type)
		}
	}
}

// TestUniquePropertyConstraint_WithLabel tests uniqueness within a specific label
func TestUniquePropertyConstraint_WithLabel(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create users with unique emails
	node1, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})
	// Create admin with same email (different label, should be allowed with per-label scope)
	graph.CreateNode([]string{"Admin"}, map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})
	// Create another user with same email (same label, should violate)
	node3, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})

	constraint := &UniquePropertyConstraint{
		PropertyKey: "email",
		NodeLabel:   "User",
		Scope:       ScopeGlobal, // Within User label only
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should only find the duplicate within User nodes
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && violations[0].NodeID != nil {
		// Violation should be for one of the User nodes with duplicate email
		violatedNodeID := *violations[0].NodeID
		if violatedNodeID != node1.ID && violatedNodeID != node3.ID {
			t.Errorf("Expected violation for node1 or node3")
		}
	}
}

// TestUniquePropertyConstraint_PerLabelScope tests per-label uniqueness
func TestUniquePropertyConstraint_PerLabelScope(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create users
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"slug": storage.StringValue("alice"),
	})
	userDup, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"slug": storage.StringValue("alice"), // Duplicate within User
	})

	// Create concepts
	graph.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"slug": storage.StringValue("alice"), // Same slug, but different label (OK)
	})
	conceptDup, _ := graph.CreateNode([]string{"Concept"}, map[string]storage.Value{
		"slug": storage.StringValue("alice"), // Duplicate within Concept
	})

	constraint := &UniquePropertyConstraint{
		PropertyKey: "slug",
		Scope:       ScopeLabel, // Unique per label
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should find 2 violations (one in User, one in Concept)
	if len(violations) != 2 {
		t.Errorf("Expected 2 violations, got %d", len(violations))
	}

	// Check that both duplicates are flagged
	violatedIDs := make(map[uint64]bool)
	for _, v := range violations {
		if v.NodeID != nil {
			violatedIDs[*v.NodeID] = true
		}
	}
	if !violatedIDs[userDup.ID] {
		t.Error("Expected violation for userDup")
	}
	if !violatedIDs[conceptDup.ID] {
		t.Error("Expected violation for conceptDup")
	}
}

// TestUniquePropertyConstraint_NoViolations tests when all values are unique
func TestUniquePropertyConstraint_NoViolations(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"id": storage.StringValue("user-1"),
	})
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"id": storage.StringValue("user-2"),
	})
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"id": storage.StringValue("user-3"),
	})

	constraint := &UniquePropertyConstraint{
		PropertyKey: "id",
		Scope:       ScopeGlobal,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 0 {
		t.Errorf("Expected 0 violations, got %d", len(violations))
	}
}

// TestUniquePropertyConstraint_MissingProperty tests nodes without the property
func TestUniquePropertyConstraint_MissingProperty(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create nodes, some without the unique property
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("alice@example.com"),
	})
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"), // No email
	})
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"email": storage.StringValue("charlie@example.com"),
	})

	constraint := &UniquePropertyConstraint{
		PropertyKey: "email",
		Scope:       ScopeGlobal,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// No violations - all emails are unique, missing email is not a uniqueness violation
	if len(violations) != 0 {
		t.Errorf("Expected 0 violations, got %d", len(violations))
	}
}

// TestUniquePropertyConstraint_IntegerValues tests uniqueness of integer properties
func TestUniquePropertyConstraint_IntegerValues(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	node1, _ := graph.CreateNode([]string{"Product"}, map[string]storage.Value{
		"sku": storage.IntValue(12345),
	})
	graph.CreateNode([]string{"Product"}, map[string]storage.Value{
		"sku": storage.IntValue(67890),
	})
	node3, _ := graph.CreateNode([]string{"Product"}, map[string]storage.Value{
		"sku": storage.IntValue(12345), // Duplicate
	})

	constraint := &UniquePropertyConstraint{
		PropertyKey: "sku",
		Scope:       ScopeGlobal,
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}
	if len(violations) > 0 && violations[0].NodeID != nil {
		violatedNodeID := *violations[0].NodeID
		if violatedNodeID != node1.ID && violatedNodeID != node3.ID {
			t.Errorf("Expected violation for node1 or node3, got node %d", violatedNodeID)
		}
	}
}

// TestUniquePropertyConstraint_Name tests the Name() method
func TestUniquePropertyConstraint_Name(t *testing.T) {
	tests := []struct {
		constraint *UniquePropertyConstraint
		expected   string
	}{
		{
			constraint: &UniquePropertyConstraint{PropertyKey: "email", Scope: ScopeGlobal},
			expected:   "UniqueGlobal(email)",
		},
		{
			constraint: &UniquePropertyConstraint{PropertyKey: "slug", Scope: ScopeLabel},
			expected:   "UniquePerLabel(slug)",
		},
		{
			constraint: &UniquePropertyConstraint{PropertyKey: "id", NodeLabel: "User", Scope: ScopeGlobal},
			expected:   "Unique(User.id)",
		},
	}

	for _, tt := range tests {
		name := tt.constraint.Name()
		if name != tt.expected {
			t.Errorf("Expected name '%s', got '%s'", tt.expected, name)
		}
	}
}

// TestUniqueEdgeConstraint tests unique edge validation
func TestUniqueEdgeConstraint(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	user1, _ := graph.CreateNode([]string{"User"}, nil)
	user2, _ := graph.CreateNode([]string{"User"}, nil)
	user3, _ := graph.CreateNode([]string{"User"}, nil)

	// Create edges
	edge1, _ := graph.CreateEdge(user1.ID, user2.ID, "FOLLOWS", nil, 1.0)
	graph.CreateEdge(user1.ID, user3.ID, "FOLLOWS", nil, 1.0)
	// Create duplicate edge
	edge3, _ := graph.CreateEdge(user1.ID, user2.ID, "FOLLOWS", nil, 1.0) // Duplicate!

	constraint := &UniqueEdgeConstraint{
		EdgeType: "FOLLOWS",
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && violations[0].EdgeID != nil {
		// Violation should be for one of the duplicate edges (edge1 or edge3)
		violatedEdgeID := *violations[0].EdgeID
		if violatedEdgeID != edge1.ID && violatedEdgeID != edge3.ID {
			t.Errorf("Expected violation for edge1 or edge3, got edge %d", violatedEdgeID)
		}
	}
}

// TestUniqueEdgeConstraint_NoViolations tests when all edges are unique
func TestUniqueEdgeConstraint_NoViolations(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	user1, _ := graph.CreateNode([]string{"User"}, nil)
	user2, _ := graph.CreateNode([]string{"User"}, nil)
	user3, _ := graph.CreateNode([]string{"User"}, nil)

	// Create unique edges
	graph.CreateEdge(user1.ID, user2.ID, "FOLLOWS", nil, 1.0)
	graph.CreateEdge(user1.ID, user3.ID, "FOLLOWS", nil, 1.0)
	graph.CreateEdge(user2.ID, user3.ID, "FOLLOWS", nil, 1.0)

	constraint := &UniqueEdgeConstraint{
		EdgeType: "FOLLOWS",
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if len(violations) != 0 {
		t.Errorf("Expected 0 violations, got %d", len(violations))
	}
}

// TestUniqueEdgeConstraint_DifferentTypes tests that different edge types are independent
func TestUniqueEdgeConstraint_DifferentTypes(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	user1, _ := graph.CreateNode([]string{"User"}, nil)
	user2, _ := graph.CreateNode([]string{"User"}, nil)

	// Create edges of different types between same nodes (OK)
	graph.CreateEdge(user1.ID, user2.ID, "FOLLOWS", nil, 1.0)
	graph.CreateEdge(user1.ID, user2.ID, "LIKES", nil, 1.0)
	graph.CreateEdge(user1.ID, user2.ID, "BLOCKS", nil, 1.0)

	constraint := &UniqueEdgeConstraint{
		EdgeType: "FOLLOWS",
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// No violations - only one FOLLOWS edge between these nodes
	if len(violations) != 0 {
		t.Errorf("Expected 0 violations, got %d", len(violations))
	}
}

// TestUniqueEdgeConstraint_WithLabels tests edge constraint with label filtering
func TestUniqueEdgeConstraint_WithLabels(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	user1, _ := graph.CreateNode([]string{"User"}, nil)
	user2, _ := graph.CreateNode([]string{"User"}, nil)
	admin, _ := graph.CreateNode([]string{"Admin"}, nil)

	// User -> User FOLLOWS (will check for duplicates)
	edge1, _ := graph.CreateEdge(user1.ID, user2.ID, "FOLLOWS", nil, 1.0)
	edge2, _ := graph.CreateEdge(user1.ID, user2.ID, "FOLLOWS", nil, 1.0) // Duplicate

	// User -> Admin FOLLOWS (different target label, not checked)
	graph.CreateEdge(user1.ID, admin.ID, "FOLLOWS", nil, 1.0)

	constraint := &UniqueEdgeConstraint{
		EdgeType:    "FOLLOWS",
		SourceLabel: "User",
		TargetLabel: "User",
	}

	violations, err := constraint.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Should only find the User->User duplicate
	if len(violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(violations))
	}

	if len(violations) > 0 && violations[0].EdgeID != nil {
		violatedEdgeID := *violations[0].EdgeID
		if violatedEdgeID != edge1.ID && violatedEdgeID != edge2.ID {
			t.Errorf("Expected violation for edge1 or edge2, got edge %d", violatedEdgeID)
		}
	}
}
