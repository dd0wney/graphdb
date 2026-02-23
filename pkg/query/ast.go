package query

// AST (Abstract Syntax Tree) represents a parsed query

// Query represents a complete query statement
type Query struct {
	Match   *MatchClause
	Where   *WhereClause
	Return  *ReturnClause
	Create  *CreateClause
	Delete  *DeleteClause
	Set     *SetClause
	Unwind  *UnwindClause
	Merge   *MergeClause
	With    *WithClause
	Next    *Query // For WITH chaining
	Limit   int
	Skip    int
	Explain bool
	Profile bool

	// InitialBindings are injected by WITH clause chaining
	InitialBindings []*BindingSet
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
	Properties map[string]any
}

// RelationshipPattern represents a relationship in a pattern
type RelationshipPattern struct {
	Variable   string
	Type       string
	Direction  Direction
	Properties map[string]any
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

// ReturnClause represents what to return
type ReturnClause struct {
	Items     []*ReturnItem
	Distinct  bool
	OrderBy   []*OrderByItem
	GroupBy   []*PropertyExpression
	Ascending bool
}

// ReturnItem represents a single return item
type ReturnItem struct {
	Expression *PropertyExpression
	ValueExpr  Expression // Broader type for function calls; if non-nil, takes precedence
	Alias      string
	Aggregate  string // COUNT, SUM, AVG, MIN, MAX, COLLECT
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
	Value    any
}

// UnwindClause represents an UNWIND operation
type UnwindClause struct {
	Expression *PropertyExpression
	Alias      string
}

// MergeClause represents a MERGE operation (match-or-create)
type MergeClause struct {
	Pattern  *Pattern
	OnMatch  *SetClause
	OnCreate *SetClause
}

// WithClause represents a WITH projection between query segments
type WithClause struct {
	Items []*ReturnItem
	Where *WhereClause
}
