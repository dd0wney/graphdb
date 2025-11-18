package constraints

import (
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestValidator_SingleConstraint tests validator with one constraint
func TestValidator_SingleConstraint(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create invalid node (missing required property)
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	constraint := &PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	}

	validator := NewValidator()
	validator.AddConstraint(constraint)

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected validation to fail")
	}

	if len(result.Violations) != 1 {
		t.Errorf("Expected 1 violation, got %d", len(result.Violations))
	}

	if result.CheckedAt.IsZero() {
		t.Error("Expected CheckedAt to be set")
	}
}

// TestValidator_MultipleConstraints tests validator with multiple constraints
func TestValidator_MultipleConstraints(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create user with multiple violations
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
		"age":  storage.IntValue(5), // Too young
		// Missing email
	})

	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	})

	minAge := storage.IntValue(18)
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "age",
		Type:         storage.TypeInt,
		Min:          &minAge,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected validation to fail")
	}

	// Should have 2 violations
	if len(result.Violations) != 2 {
		t.Errorf("Expected 2 violations, got %d", len(result.Violations))
	}
}

// TestValidator_AllValid tests when all constraints pass
func TestValidator_AllValid(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create valid user
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name":  storage.StringValue("Alice"),
		"email": storage.StringValue("alice@example.com"),
		"age":   storage.IntValue(25),
	})

	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	})

	minAge := storage.IntValue(18)
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "age",
		Type:         storage.TypeInt,
		Min:          &minAge,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected validation to pass")
	}

	if len(result.Violations) != 0 {
		t.Errorf("Expected 0 violations, got %d", len(result.Violations))
	}
}

// TestValidator_MixedConstraintTypes tests property + cardinality constraints
func TestValidator_MixedConstraintTypes(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create account without owner and invalid balance
	account, _ := graph.CreateNode([]string{"Account"}, map[string]storage.Value{
		"balance": storage.FloatValue(-100.0), // Negative balance
	})

	validator := NewValidator()

	// Property constraint: balance >= 0
	minBalance := storage.FloatValue(0.0)
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "Account",
		PropertyName: "balance",
		Type:         storage.TypeFloat,
		Min:          &minBalance,
	})

	// Cardinality constraint: must have at least 1 owner
	validator.AddConstraint(&CardinalityConstraint{
		NodeLabel: "Account",
		EdgeType:  "OWNS",
		Direction: Incoming,
		Min:       1,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected validation to fail")
	}

	// Should have 2 violations (balance + cardinality)
	if len(result.Violations) != 2 {
		t.Errorf("Expected 2 violations, got %d", len(result.Violations))
	}

	// Both violations should be for the same node
	for _, v := range result.Violations {
		if v.NodeID == nil || *v.NodeID != account.ID {
			t.Errorf("Expected violation for account node")
		}
	}
}

// TestValidator_EmptyGraph tests validator on empty graph
func TestValidator_EmptyGraph(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected empty graph to be valid")
	}

	if len(result.Violations) != 0 {
		t.Errorf("Expected 0 violations, got %d", len(result.Violations))
	}
}

// TestValidator_NoConstraints tests validator with no constraints
func TestValidator_NoConstraints(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	graph.CreateNode([]string{"User"}, nil)

	validator := NewValidator()

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected validation with no constraints to pass")
	}
}

// TestValidator_ConstraintError tests handling of constraint errors
func TestValidator_ConstraintError(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// This should not cause issues even if constraint references non-existent label
	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "NonExistentLabel",
		PropertyName: "prop",
		Required:     true,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate should handle non-existent labels gracefully: %v", err)
	}

	if !result.Valid {
		t.Error("Non-existent label should not cause violations")
	}
}

// TestValidator_FilterBySeverity tests getting violations by severity
func TestValidator_FilterBySeverity(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create node with violation
	graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})

	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	errors := result.GetViolationsBySeverity(Error)
	if len(errors) != 1 {
		t.Errorf("Expected 1 error violation, got %d", len(errors))
	}

	warnings := result.GetViolationsBySeverity(Warning)
	if len(warnings) != 0 {
		t.Errorf("Expected 0 warning violations, got %d", len(warnings))
	}
}

// TestValidator_GetViolationsByType tests getting violations by type
func TestValidator_GetViolationsByType(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	// Create node with property and cardinality violations
	graph.CreateNode([]string{"Account"}, map[string]storage.Value{
		"name": storage.StringValue("Account1"),
		// Missing balance (property violation)
		// Missing owner edge (cardinality violation)
	})

	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "Account",
		PropertyName: "balance",
		Required:     true,
	})
	validator.AddConstraint(&CardinalityConstraint{
		NodeLabel: "Account",
		EdgeType:  "OWNS",
		Direction: Incoming,
		Min:       1,
	})

	result, err := validator.Validate(graph)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	propertyViolations := result.GetViolationsByType(MissingProperty)
	if len(propertyViolations) != 1 {
		t.Errorf("Expected 1 property violation, got %d", len(propertyViolations))
	}

	cardinalityViolations := result.GetViolationsByType(CardinalityViolation)
	if len(cardinalityViolations) != 1 {
		t.Errorf("Expected 1 cardinality violation, got %d", len(cardinalityViolations))
	}
}

// TestValidator_TimestampRecorded tests that validation timestamp is recorded
func TestValidator_TimestampRecorded(t *testing.T) {
	graph := setupTestGraph(t)
	defer graph.Close()

	validator := NewValidator()
	validator.AddConstraint(&PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Required:     true,
	})

	before := time.Now()
	result, err := validator.Validate(graph)
	after := time.Now()

	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if result.CheckedAt.Before(before) || result.CheckedAt.After(after) {
		t.Error("CheckedAt timestamp not within expected range")
	}
}
