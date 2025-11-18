package constraints

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// PropertyConstraint validates node properties
type PropertyConstraint struct {
	NodeLabel    string             // Label to apply constraint to
	PropertyName string             // Name of the property
	Type         storage.ValueType  // Expected type (0 = any type)
	Required     bool               // Whether property must exist
	Min          *storage.Value     // Minimum value (for int/float)
	Max          *storage.Value     // Maximum value (for int/float)
}

// Name returns the constraint name
func (pc *PropertyConstraint) Name() string {
	return fmt.Sprintf("PropertyConstraint(%s.%s)", pc.NodeLabel, pc.PropertyName)
}

// Validate checks the property constraint against all nodes with the target label
func (pc *PropertyConstraint) Validate(graph *storage.GraphStorage) ([]Violation, error) {
	violations := make([]Violation, 0)

	// Get all nodes with the target label
	nodes, err := graph.FindNodesByLabel(pc.NodeLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to find nodes with label %s: %w", pc.NodeLabel, err)
	}

	for _, node := range nodes {
		// Check if property exists
		propValue, exists := node.GetProperty(pc.PropertyName)

		if !exists {
			// Property missing
			if pc.Required {
				nodeID := node.ID
				violations = append(violations, Violation{
					Type:       MissingProperty,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: pc.Name(),
					Message:    fmt.Sprintf("Node %d missing required property '%s'", node.ID, pc.PropertyName),
					Details: map[string]interface{}{
						"label":    pc.NodeLabel,
						"property": pc.PropertyName,
					},
				})
			}
			continue
		}

		// Check type if specified
		if pc.Type != 0 && propValue.Type != pc.Type {
			nodeID := node.ID
			violations = append(violations, Violation{
				Type:       InvalidType,
				Severity:   Error,
				NodeID:     &nodeID,
				Constraint: pc.Name(),
				Message: fmt.Sprintf("Node %d property '%s' has wrong type",
					node.ID, pc.PropertyName),
				Details: map[string]interface{}{
					"label":        pc.NodeLabel,
					"property":     pc.PropertyName,
					"actual_type":  propValue.Type,
					"expected_type": pc.Type,
				},
			})
			continue // Don't check range if type is wrong
		}

		// Check range for numeric types
		if pc.Min != nil || pc.Max != nil {
			if err := pc.validateRange(node, propValue, &violations); err != nil {
				return violations, err
			}
		}
	}

	return violations, nil
}

// validateRange checks if a numeric property is within the specified range
func (pc *PropertyConstraint) validateRange(node *storage.Node, propValue storage.Value, violations *[]Violation) error {
	nodeID := node.ID

	switch propValue.Type {
	case storage.TypeInt:
		value, err := propValue.AsInt()
		if err != nil {
			return fmt.Errorf("failed to get int value: %w", err)
		}

		if pc.Min != nil {
			minVal, err := pc.Min.AsInt()
			if err != nil {
				return fmt.Errorf("min value is not an int: %w", err)
			}
			if value < minVal {
				*violations = append(*violations, Violation{
					Type:       OutOfRange,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: pc.Name(),
					Message: fmt.Sprintf("Node %d property '%s' value %d is below minimum %d",
						node.ID, pc.PropertyName, value, minVal),
					Details: map[string]interface{}{
						"label":    pc.NodeLabel,
						"property": pc.PropertyName,
						"value":    value,
						"min":      minVal,
					},
				})
			}
		}

		if pc.Max != nil {
			maxVal, err := pc.Max.AsInt()
			if err != nil {
				return fmt.Errorf("max value is not an int: %w", err)
			}
			if value > maxVal {
				*violations = append(*violations, Violation{
					Type:       OutOfRange,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: pc.Name(),
					Message: fmt.Sprintf("Node %d property '%s' value %d is above maximum %d",
						node.ID, pc.PropertyName, value, maxVal),
					Details: map[string]interface{}{
						"label":    pc.NodeLabel,
						"property": pc.PropertyName,
						"value":    value,
						"max":      maxVal,
					},
				})
			}
		}

	case storage.TypeFloat:
		value, err := propValue.AsFloat()
		if err != nil {
			return fmt.Errorf("failed to get float value: %w", err)
		}

		if pc.Min != nil {
			minVal, err := pc.Min.AsFloat()
			if err != nil {
				return fmt.Errorf("min value is not a float: %w", err)
			}
			if value < minVal {
				*violations = append(*violations, Violation{
					Type:       OutOfRange,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: pc.Name(),
					Message: fmt.Sprintf("Node %d property '%s' value %.2f is below minimum %.2f",
						node.ID, pc.PropertyName, value, minVal),
					Details: map[string]interface{}{
						"label":    pc.NodeLabel,
						"property": pc.PropertyName,
						"value":    value,
						"min":      minVal,
					},
				})
			}
		}

		if pc.Max != nil {
			maxVal, err := pc.Max.AsFloat()
			if err != nil {
				return fmt.Errorf("max value is not a float: %w", err)
			}
			if value > maxVal {
				*violations = append(*violations, Violation{
					Type:       OutOfRange,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: pc.Name(),
					Message: fmt.Sprintf("Node %d property '%s' value %.2f is above maximum %.2f",
						node.ID, pc.PropertyName, value, maxVal),
					Details: map[string]interface{}{
						"label":    pc.NodeLabel,
						"property": pc.PropertyName,
						"value":    value,
						"max":      maxVal,
					},
				})
			}
		}
	}

	return nil
}
