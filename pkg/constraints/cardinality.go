package constraints

import (
	"fmt"
)

// Direction specifies edge direction for cardinality constraints
type Direction int

const (
	Outgoing Direction = iota // Edges from this node
	Incoming                  // Edges to this node
	Any                       // Edges in either direction
)

func (d Direction) String() string {
	switch d {
	case Outgoing:
		return "Outgoing"
	case Incoming:
		return "Incoming"
	case Any:
		return "Any"
	default:
		return "Unknown"
	}
}

// CardinalityConstraint validates the number of edges a node has
type CardinalityConstraint struct {
	NodeLabel string    // Label to apply constraint to
	EdgeType  string    // Type of edge (empty = any type)
	Direction Direction // Direction of edges to count
	Min       int       // Minimum number of edges (0 = optional)
	Max       int       // Maximum number of edges (0 = unlimited)
}

// Name returns the constraint name
func (cc *CardinalityConstraint) Name() string {
	edgeType := cc.EdgeType
	if edgeType == "" {
		edgeType = "*"
	}
	return fmt.Sprintf("CardinalityConstraint(%s,%s,%s,[%d,%d])",
		cc.NodeLabel, edgeType, cc.Direction, cc.Min, cc.Max)
}

// Validate checks the cardinality constraint against all nodes with the target label
func (cc *CardinalityConstraint) Validate(graph GraphReader) ([]Violation, error) {
	violations := make([]Violation, 0)

	// Get all nodes with the target label
	nodes, err := graph.FindNodesByLabel(cc.NodeLabel)
	if err != nil {
		return nil, fmt.Errorf("failed to find nodes with label %s: %w", cc.NodeLabel, err)
	}

	for _, node := range nodes {
		edgeCount, err := cc.countEdges(graph, node.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to count edges for node %d: %w", node.ID, err)
		}

		// Check minimum
		if cc.Min > 0 && edgeCount < cc.Min {
			nodeID := node.ID
			violations = append(violations, Violation{
				Type:       CardinalityViolation,
				Severity:   Error,
				NodeID:     &nodeID,
				Constraint: cc.Name(),
				Message: fmt.Sprintf("Node %d has %d %s edge(s) of type '%s', minimum is %d",
					node.ID, edgeCount, cc.Direction, cc.EdgeType, cc.Min),
				Details: map[string]any{
					"label":      cc.NodeLabel,
					"edge_type":  cc.EdgeType,
					"direction":  cc.Direction.String(),
					"count":      edgeCount,
					"min":        cc.Min,
				},
			})
		}

		// Check maximum
		if cc.Max > 0 && edgeCount > cc.Max {
			nodeID := node.ID
			violations = append(violations, Violation{
				Type:       CardinalityViolation,
				Severity:   Error,
				NodeID:     &nodeID,
				Constraint: cc.Name(),
				Message: fmt.Sprintf("Node %d has %d %s edge(s) of type '%s', maximum is %d",
					node.ID, edgeCount, cc.Direction, cc.EdgeType, cc.Max),
				Details: map[string]any{
					"label":      cc.NodeLabel,
					"edge_type":  cc.EdgeType,
					"direction":  cc.Direction.String(),
					"count":      edgeCount,
					"max":        cc.Max,
				},
			})
		}
	}

	return violations, nil
}

// countEdges counts edges for a node based on direction and type
func (cc *CardinalityConstraint) countEdges(graph GraphReader, nodeID uint64) (int, error) {
	count := 0

	// Count outgoing edges
	if cc.Direction == Outgoing || cc.Direction == Any {
		outgoing, err := graph.GetOutgoingEdges(nodeID)
		if err != nil {
			return 0, err
		}
		for _, edge := range outgoing {
			if cc.EdgeType == "" || edge.Type == cc.EdgeType {
				count++
			}
		}
	}

	// Count incoming edges
	if cc.Direction == Incoming || cc.Direction == Any {
		incoming, err := graph.GetIncomingEdges(nodeID)
		if err != nil {
			return 0, err
		}
		for _, edge := range incoming {
			if cc.EdgeType == "" || edge.Type == cc.EdgeType {
				count++
			}
		}
	}

	return count, nil
}
