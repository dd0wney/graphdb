package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// AST (Abstract Syntax Tree) represents a parsed query

// Query represents a complete query statement
type Query struct {
	Match  *MatchClause
	Where  *WhereClause
	Return *ReturnClause
	Create *CreateClause
	Delete *DeleteClause
	Set    *SetClause
	Limit  int
	Skip   int
}

// MatchClause represents a MATCH pattern
type MatchClause struct {
	Patterns []*Pattern
}

// Pattern represents a graph pattern to match
type Pattern struct {
	Nodes         []*NodePattern
	Relationships []*RelationshipPattern
}

// NodePattern represents a node in a pattern
type NodePattern struct {
	Variable   string
	Labels     []string
	Properties map[string]interface{}
}

// RelationshipPattern represents a relationship in a pattern
type RelationshipPattern struct {
	Variable   string
	Type       string
	Direction  Direction
	Properties map[string]interface{}
	From       *NodePattern
	To         *NodePattern
	MinHops    int // For variable-length paths
	MaxHops    int
}

// Direction represents relationship direction
type Direction int

const (
	DirectionOutgoing Direction = iota
	DirectionIncoming
	DirectionBoth
)

func (d Direction) String() string {
	switch d {
	case DirectionOutgoing:
		return "->"
	case DirectionIncoming:
		return "<-"
	case DirectionBoth:
		return "-"
	default:
		return "?"
	}
}

// WhereClause represents filtering conditions
type WhereClause struct {
	Expression Expression
}

// Expression is an interface for all expression types
type Expression interface {
	Eval(context map[string]interface{}) (bool, error)
}

// BinaryExpression represents binary operations (AND, OR, =, <, >, etc.)
type BinaryExpression struct {
	Left     Expression
	Operator string
	Right    Expression
}

func (be *BinaryExpression) Eval(context map[string]interface{}) (bool, error) {
	switch be.Operator {
	case "AND":
		left, err := be.Left.Eval(context)
		if err != nil || !left {
			return false, err
		}
		return be.Right.Eval(context)

	case "OR":
		left, err := be.Left.Eval(context)
		if err != nil {
			return false, err
		}
		if left {
			return true, nil
		}
		return be.Right.Eval(context)

	case "=", ">", "<", ">=", "<=", "!=":
		// Comparison operators
		return evalComparison(be.Left, be.Right, be.Operator, context)

	default:
		return false, fmt.Errorf("unknown operator: %s", be.Operator)
	}
}

// PropertyExpression represents property access (e.g., n.name)
type PropertyExpression struct {
	Variable string
	Property string
}

func (pe *PropertyExpression) Eval(context map[string]interface{}) (bool, error) {
	// This returns the property value, not a boolean
	// Used in comparisons
	return false, fmt.Errorf("property expression must be used in comparison")
}

// LiteralExpression represents a literal value
type LiteralExpression struct {
	Value interface{}
}

func (le *LiteralExpression) Eval(context map[string]interface{}) (bool, error) {
	// Convert value to boolean
	if b, ok := le.Value.(bool); ok {
		return b, nil
	}
	return false, fmt.Errorf("cannot convert literal to boolean")
}

// ReturnClause represents what to return
type ReturnClause struct {
	Items     []*ReturnItem
	Distinct  bool
	OrderBy   []*OrderByItem
	Ascending bool
}

// ReturnItem represents a single return item
type ReturnItem struct {
	Expression *PropertyExpression
	Alias      string
	Aggregate  string // COUNT, SUM, AVG, MIN, MAX
}

// OrderByItem represents ordering specification
type OrderByItem struct {
	Expression *PropertyExpression
	Ascending  bool
}

// CreateClause represents node/relationship creation
type CreateClause struct {
	Patterns []*Pattern
}

// DeleteClause represents deletion
type DeleteClause struct {
	Variables []string
	Detach    bool // DETACH DELETE removes relationships too
}

// SetClause represents property updates
type SetClause struct {
	Assignments []*Assignment
}

// Assignment represents a property assignment
type Assignment struct {
	Variable string
	Property string
	Value    interface{}
}

// Helper function to evaluate comparisons
func evalComparison(left, right Expression, op string, context map[string]interface{}) (bool, error) {
	// Extract actual values
	leftVal := extractValue(left, context)
	rightVal := extractValue(right, context)

	switch op {
	case "=":
		return leftVal == rightVal, nil
	case "!=":
		return leftVal != rightVal, nil
	case ">":
		return compareValues(leftVal, rightVal) > 0, nil
	case "<":
		return compareValues(leftVal, rightVal) < 0, nil
	case ">=":
		return compareValues(leftVal, rightVal) >= 0, nil
	case "<=":
		return compareValues(leftVal, rightVal) <= 0, nil
	default:
		return false, fmt.Errorf("unknown comparison operator: %s", op)
	}
}

// Extract actual value from expression
func extractValue(expr Expression, context map[string]interface{}) interface{} {
	switch e := expr.(type) {
	case *PropertyExpression:
		if obj, ok := context[e.Variable]; ok {
			// Handle storage.Node objects
			if node, ok := obj.(*storage.Node); ok {
				if val, found := node.Properties[e.Property]; found {
					// Extract the actual value from storage.Value based on type
					switch val.Type {
					case storage.TypeInt:
						if intVal, err := val.AsInt(); err == nil {
							return intVal
						}
					case storage.TypeString:
						if strVal, err := val.AsString(); err == nil {
							return strVal
						}
					case storage.TypeFloat:
						if floatVal, err := val.AsFloat(); err == nil {
							return floatVal
						}
					case storage.TypeBool:
						if boolVal, err := val.AsBool(); err == nil {
							return boolVal
						}
					}
				}
				return nil
			}
			// Fallback to map[string]interface{} for backward compatibility
			if m, ok := obj.(map[string]interface{}); ok {
				return m[e.Property]
			}
		}
		return nil
	case *LiteralExpression:
		return e.Value
	default:
		return nil
	}
}

// Compare values (simplified)
func compareValues(left, right interface{}) int {
	// Type assertion for common types
	switch l := left.(type) {
	case int:
		if r, ok := right.(int); ok {
			return l - r
		}
	case int64:
		if r, ok := right.(int64); ok {
			if l > r {
				return 1
			} else if l < r {
				return -1
			}
			return 0
		}
	case float64:
		if r, ok := right.(float64); ok {
			if l > r {
				return 1
			} else if l < r {
				return -1
			}
			return 0
		}
	case string:
		if r, ok := right.(string); ok {
			if l > r {
				return 1
			} else if l < r {
				return -1
			}
			return 0
		}
	}
	return 0
}
