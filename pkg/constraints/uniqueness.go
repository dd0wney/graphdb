package constraints

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// UniqueScope defines the scope of uniqueness checking
type UniqueScope int

const (
	// ScopeGlobal means the property must be unique across all nodes
	ScopeGlobal UniqueScope = iota
	// ScopeLabel means the property must be unique within nodes of the same label
	ScopeLabel
)

func (s UniqueScope) String() string {
	switch s {
	case ScopeGlobal:
		return "Global"
	case ScopeLabel:
		return "Label"
	default:
		return "Unknown"
	}
}

// UniquePropertyConstraint ensures a property value is unique across nodes.
// This is useful for enforcing uniqueness on external IDs, emails, slugs, etc.
type UniquePropertyConstraint struct {
	// PropertyKey is the property that must be unique
	PropertyKey string

	// NodeLabel optionally restricts the constraint to nodes with this label.
	// If empty and Scope is ScopeLabel, the constraint validates per-label uniqueness.
	// If set and Scope is ScopeLabel, only nodes with this label are checked.
	NodeLabel string

	// Scope determines whether uniqueness is global or per-label
	Scope UniqueScope
}

// Name returns a human-readable name for this constraint
func (c *UniquePropertyConstraint) Name() string {
	if c.NodeLabel != "" {
		return fmt.Sprintf("Unique(%s.%s)", c.NodeLabel, c.PropertyKey)
	}
	if c.Scope == ScopeGlobal {
		return fmt.Sprintf("UniqueGlobal(%s)", c.PropertyKey)
	}
	return fmt.Sprintf("UniquePerLabel(%s)", c.PropertyKey)
}

// Validate checks that the property is unique according to the constraint scope
func (c *UniquePropertyConstraint) Validate(graph GraphReader) ([]Violation, error) {
	var violations []Violation

	switch c.Scope {
	case ScopeGlobal:
		violations = c.validateGlobal(graph)
	case ScopeLabel:
		violations = c.validatePerLabel(graph)
	}

	return violations, nil
}

// validateGlobal checks that the property is unique across all nodes (or all nodes with NodeLabel)
func (c *UniquePropertyConstraint) validateGlobal(graph GraphReader) []Violation {
	var violations []Violation

	// Map of property value (as string) -> list of node IDs with that value
	seen := make(map[string][]uint64)

	var nodes []*storage.Node
	var err error

	if c.NodeLabel != "" {
		// Only check nodes with specific label
		nodes, err = graph.FindNodesByLabel(c.NodeLabel)
		if err != nil {
			return []Violation{{
				Type:       InvalidStructure,
				Severity:   Error,
				Constraint: c.Name(),
				Message:    fmt.Sprintf("Failed to query nodes: %v", err),
			}}
		}
	} else {
		// Check all nodes
		nodes = graph.GetAllNodes()
	}

	// Build map of values to node IDs
	for _, node := range nodes {
		prop, exists := node.Properties[c.PropertyKey]
		if !exists {
			continue
		}

		// Convert value to string for comparison using Value.String() method
		valueKey := prop.String()
		seen[valueKey] = append(seen[valueKey], node.ID)
	}

	// Find duplicates
	for valueKey, nodeIDs := range seen {
		if len(nodeIDs) > 1 {
			// Create violation for each duplicate (after the first)
			for i := 1; i < len(nodeIDs); i++ {
				nodeID := nodeIDs[i]
				violations = append(violations, Violation{
					Type:       UniquenessViolation,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: c.Name(),
					Message: fmt.Sprintf("Duplicate value '%s' for property '%s' (also exists on node %d)",
						valueKey, c.PropertyKey, nodeIDs[0]),
					Details: map[string]any{
						"property":       c.PropertyKey,
						"value":          valueKey,
						"duplicate_of":   nodeIDs[0],
						"all_duplicates": nodeIDs,
					},
				})
			}
		}
	}

	return violations
}

// validatePerLabel checks uniqueness within each label group
func (c *UniquePropertyConstraint) validatePerLabel(graph GraphReader) []Violation {
	var violations []Violation

	if c.NodeLabel != "" {
		// Only check specific label
		return c.validateLabelGroup(graph, c.NodeLabel)
	}

	// Check each label separately
	labels := graph.GetAllLabels()
	for _, label := range labels {
		labelViolations := c.validateLabelGroup(graph, label)
		violations = append(violations, labelViolations...)
	}

	return violations
}

// validateLabelGroup checks uniqueness within a single label group
func (c *UniquePropertyConstraint) validateLabelGroup(graph GraphReader, label string) []Violation {
	var violations []Violation

	nodes, err := graph.FindNodesByLabel(label)
	if err != nil {
		return []Violation{{
			Type:       InvalidStructure,
			Severity:   Error,
			Constraint: c.Name(),
			Message:    fmt.Sprintf("Failed to query nodes with label '%s': %v", label, err),
		}}
	}

	// Map of property value -> list of node IDs
	seen := make(map[string][]uint64)

	for _, node := range nodes {
		prop, exists := node.Properties[c.PropertyKey]
		if !exists {
			continue
		}

		valueKey := prop.String()
		seen[valueKey] = append(seen[valueKey], node.ID)
	}

	// Find duplicates
	for valueKey, nodeIDs := range seen {
		if len(nodeIDs) > 1 {
			for i := 1; i < len(nodeIDs); i++ {
				nodeID := nodeIDs[i]
				violations = append(violations, Violation{
					Type:       UniquenessViolation,
					Severity:   Error,
					NodeID:     &nodeID,
					Constraint: c.Name(),
					Message: fmt.Sprintf("Duplicate value '%s' for property '%s' within label '%s' (also exists on node %d)",
						valueKey, c.PropertyKey, label, nodeIDs[0]),
					Details: map[string]any{
						"property":       c.PropertyKey,
						"value":          valueKey,
						"label":          label,
						"duplicate_of":   nodeIDs[0],
						"all_duplicates": nodeIDs,
					},
				})
			}
		}
	}

	return violations
}

// UniqueEdgeConstraint ensures only one edge of a specific type exists between two nodes.
// This is useful for preventing duplicate relationships.
type UniqueEdgeConstraint struct {
	// EdgeType is the edge type to check for uniqueness
	EdgeType string

	// SourceLabel optionally restricts to edges from nodes with this label
	SourceLabel string

	// TargetLabel optionally restricts to edges to nodes with this label
	TargetLabel string
}

// Name returns a human-readable name for this constraint
func (c *UniqueEdgeConstraint) Name() string {
	if c.SourceLabel != "" && c.TargetLabel != "" {
		return fmt.Sprintf("UniqueEdge(%s:%s->%s)", c.SourceLabel, c.EdgeType, c.TargetLabel)
	}
	return fmt.Sprintf("UniqueEdge(%s)", c.EdgeType)
}

// Validate checks that no duplicate edges exist between node pairs
func (c *UniqueEdgeConstraint) Validate(graph GraphReader) ([]Violation, error) {
	var violations []Violation

	edges, err := graph.FindEdgesByType(c.EdgeType)
	if err != nil {
		return nil, fmt.Errorf("failed to query edges: %w", err)
	}

	// Map of (fromID, toID) -> list of edge IDs
	edgePairs := make(map[string][]uint64)

	for _, edge := range edges {
		// Check source label if specified
		if c.SourceLabel != "" {
			sourceNode, err := graph.GetNode(edge.FromNodeID)
			if err != nil || !sourceNode.HasLabel(c.SourceLabel) {
				continue
			}
		}

		// Check target label if specified
		if c.TargetLabel != "" {
			targetNode, err := graph.GetNode(edge.ToNodeID)
			if err != nil || !targetNode.HasLabel(c.TargetLabel) {
				continue
			}
		}

		pairKey := fmt.Sprintf("%d->%d", edge.FromNodeID, edge.ToNodeID)
		edgePairs[pairKey] = append(edgePairs[pairKey], edge.ID)
	}

	// Find duplicates
	for pairKey, edgeIDs := range edgePairs {
		if len(edgeIDs) > 1 {
			for i := 1; i < len(edgeIDs); i++ {
				edgeID := edgeIDs[i]
				violations = append(violations, Violation{
					Type:       UniquenessViolation,
					Severity:   Error,
					EdgeID:     &edgeID,
					Constraint: c.Name(),
					Message: fmt.Sprintf("Duplicate edge of type '%s' between nodes %s (edge %d already exists)",
						c.EdgeType, pairKey, edgeIDs[0]),
					Details: map[string]any{
						"edge_type":      c.EdgeType,
						"node_pair":      pairKey,
						"duplicate_of":   edgeIDs[0],
						"all_duplicates": edgeIDs,
					},
				})
			}
		}
	}

	return violations, nil
}
