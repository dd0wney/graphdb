package main

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/constraints"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func main() {
	// Create a graph database
	graph, err := storage.NewGraphStorage("./data/constraints_demo")
	if err != nil {
		log.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	fmt.Println("=== GraphDB Constraint Validation Demo ===")

	// Create a validator
	validator := constraints.NewValidator()

	// Add property constraints
	fmt.Println("1. Adding Property Constraints...")
	validator.AddConstraint(&constraints.PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "email",
		Type:         storage.TypeString,
		Required:     true,
	})

	minAge := storage.IntValue(18)
	maxAge := storage.IntValue(120)
	validator.AddConstraint(&constraints.PropertyConstraint{
		NodeLabel:    "User",
		PropertyName: "age",
		Type:         storage.TypeInt,
		Required:     true,
		Min:          &minAge,
		Max:          &maxAge,
	})

	minBalance := storage.FloatValue(0.0)
	validator.AddConstraint(&constraints.PropertyConstraint{
		NodeLabel:    "Account",
		PropertyName: "balance",
		Type:         storage.TypeFloat,
		Required:     true,
		Min:          &minBalance,
	})

	// Add cardinality constraints
	fmt.Println("2. Adding Cardinality Constraints...")
	validator.AddConstraint(&constraints.CardinalityConstraint{
		NodeLabel: "Account",
		EdgeType:  "OWNS",
		Direction: constraints.Incoming,
		Min:       1, // Every account must have at least 1 owner
		Max:       5, // But no more than 5 owners
	})

	validator.AddConstraint(&constraints.CardinalityConstraint{
		NodeLabel: "User",
		EdgeType:  "FRIEND",
		Direction: constraints.Any,
		Min:       0,  // Friends are optional
		Max:       100, // But limited to 100
	})

	fmt.Println()

	// Create some test data with violations
	fmt.Println("3. Creating Test Data...")

	// Valid user
	validUser, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name":  storage.StringValue("Alice"),
		"email": storage.StringValue("alice@example.com"),
		"age":   storage.IntValue(25),
	})
	fmt.Println("   ✓ Created valid user: Alice")

	// Invalid user (missing email, age too low)
	_, _ = graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
		"age":  storage.IntValue(12), // Too young!
		// Missing email
	})
	fmt.Println("   ✗ Created invalid user: Bob (missing email, age < 18)")

	// Valid account with owner
	account1, _ := graph.CreateNode([]string{"Account"}, map[string]storage.Value{
		"name":    storage.StringValue("Savings"),
		"balance": storage.FloatValue(1000.50),
	})
	graph.CreateEdge(validUser.ID, account1.ID, "OWNS", nil, 1.0)
	fmt.Println("   ✓ Created valid account: Savings (owned by Alice)")

	// Invalid account (negative balance, no owner)
	_, _ = graph.CreateNode([]string{"Account"}, map[string]storage.Value{
		"name":    storage.StringValue("Checking"),
		"balance": storage.FloatValue(-50.0), // Negative!
		// No owner edge
	})
	fmt.Println("   ✗ Created invalid account: Checking (negative balance, no owner)")

	// User with too many friends
	socialButterfly, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
		"name":  storage.StringValue("Charlie"),
		"email": storage.StringValue("charlie@example.com"),
		"age":   storage.IntValue(30),
	})

	// Create 101 friends (exceeds max of 100)
	for i := 0; i < 101; i++ {
		friend, _ := graph.CreateNode([]string{"User"}, map[string]storage.Value{
			"name":  storage.StringValue(fmt.Sprintf("Friend%d", i)),
			"email": storage.StringValue(fmt.Sprintf("friend%d@example.com", i)),
			"age":   storage.IntValue(25),
		})
		graph.CreateEdge(socialButterfly.ID, friend.ID, "FRIEND", nil, 1.0)
	}
	fmt.Println("   ✗ Created user with 101 friends (exceeds max of 100)")

	// Run validation
	fmt.Println("\n4. Running Validation...")
	result, err := validator.Validate(graph)
	if err != nil {
		log.Fatalf("Validation failed: %v", err)
	}

	// Display results
	if result.Valid {
		fmt.Println("✓ Graph is VALID - all constraints satisfied!")
	} else {
		fmt.Printf("✗ Graph is INVALID - found %d violation(s):\n\n", len(result.Violations))

		for i, violation := range result.Violations {
			fmt.Printf("   Violation %d:\n", i+1)
			fmt.Printf("      Type:     %s\n", violation.Type)
			fmt.Printf("      Severity: %s\n", violation.Severity)
			if violation.NodeID != nil {
				node, _ := graph.GetNode(*violation.NodeID)
				name, _ := node.GetProperty("name")
				nameStr := "Unknown"
				if name.Type == storage.TypeString {
					nameStr, _ = name.AsString()
				}
				fmt.Printf("      Node:     %d (%s)\n", *violation.NodeID, nameStr)
			}
			fmt.Printf("      Message:  %s\n", violation.Message)
			fmt.Println()
		}
	}

	// Filter violations by severity
	fmt.Println("5. Violations by Severity:")
	errors := result.GetViolationsBySeverity(constraints.Error)
	warnings := result.GetViolationsBySeverity(constraints.Warning)
	fmt.Printf("   Errors:   %d\n", len(errors))
	fmt.Printf("   Warnings: %d\n\n", len(warnings))

	// Filter violations by type
	fmt.Println("6. Violations by Type:")
	propertyViolations := result.GetViolationsByType(constraints.MissingProperty)
	fmt.Printf("   Missing Properties:       %d\n", len(propertyViolations))

	rangeViolations := result.GetViolationsByType(constraints.OutOfRange)
	fmt.Printf("   Out of Range:             %d\n", len(rangeViolations))

	cardinalityViolations := result.GetViolationsByType(constraints.CardinalityViolation)
	fmt.Printf("   Cardinality Violations:   %d\n", len(cardinalityViolations))

	fmt.Println("\n=== Demo Complete ===")
	fmt.Printf("\nValidation performed at: %s\n", result.CheckedAt.Format("2006-01-02 15:04:05"))
}
