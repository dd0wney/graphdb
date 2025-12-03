package constraints

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// GraphReader defines the read-only operations needed for constraint validation.
// This interface enables dependency injection and makes constraints testable
// without requiring a full GraphStorage implementation.
type GraphReader interface {
	// Node operations
	GetNode(nodeID uint64) (*storage.Node, error)
	GetAllNodes() []*storage.Node
	FindNodesByLabel(label string) ([]*storage.Node, error)
	GetAllLabels() []string

	// Edge operations
	GetEdge(edgeID uint64) (*storage.Edge, error)
	FindEdgesByType(edgeType string) ([]*storage.Edge, error)
	GetOutgoingEdges(nodeID uint64) ([]*storage.Edge, error)
	GetIncomingEdges(nodeID uint64) ([]*storage.Edge, error)
}

// Severity indicates the importance of a violation
type Severity int

const (
	Info Severity = iota
	Warning
	Error
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "Info"
	case Warning:
		return "Warning"
	case Error:
		return "Error"
	default:
		return "Unknown"
	}
}

// ViolationType categorizes the type of constraint violation
type ViolationType int

const (
	MissingProperty ViolationType = iota
	InvalidType
	OutOfRange
	CardinalityViolation
	ForbiddenEdge
	InvalidStructure
	UniquenessViolation
)

func (vt ViolationType) String() string {
	switch vt {
	case MissingProperty:
		return "MissingProperty"
	case InvalidType:
		return "InvalidType"
	case OutOfRange:
		return "OutOfRange"
	case CardinalityViolation:
		return "CardinalityViolation"
	case ForbiddenEdge:
		return "ForbiddenEdge"
	case InvalidStructure:
		return "InvalidStructure"
	case UniquenessViolation:
		return "UniquenessViolation"
	default:
		return "Unknown"
	}
}

// Violation represents a constraint violation
type Violation struct {
	Type       ViolationType
	Severity   Severity
	NodeID     *uint64
	EdgeID     *uint64
	Constraint string
	Message    string
	Details    map[string]any
}

// Constraint is the interface that all constraint types must implement.
// It uses the GraphReader interface for dependency injection, enabling
// easier testing and looser coupling to the storage implementation.
type Constraint interface {
	// Validate checks the constraint against the graph
	// Returns a list of violations (empty if valid)
	Validate(graph GraphReader) ([]Violation, error)

	// Name returns a human-readable name for the constraint
	Name() string
}
