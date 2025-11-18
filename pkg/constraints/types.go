package constraints

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
	Details    map[string]interface{}
}

// Constraint is the interface that all constraint types must implement
type Constraint interface {
	// Validate checks the constraint against the graph
	// Returns a list of violations (empty if valid)
	Validate(graph *storage.GraphStorage) ([]Violation, error)

	// Name returns a human-readable name for the constraint
	Name() string
}
