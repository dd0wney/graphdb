package query

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Traversal depth limits to prevent resource exhaustion
const (
	// DefaultMaxTraversalDepth is used when MaxDepth is not specified or is 0
	DefaultMaxTraversalDepth = 10
	// MaxAllowedTraversalDepth is the absolute maximum to prevent stack overflow in DFS
	// and memory exhaustion in BFS
	MaxAllowedTraversalDepth = 100
	// MinTraversalDepth is the minimum valid depth (0 means only start node)
	MinTraversalDepth = 0

	// DefaultMaxResults is used when MaxResults is not specified or is 0
	DefaultMaxResults = 10000
	// MaxAllowedResults is the absolute maximum to prevent memory exhaustion
	MaxAllowedResults = 1000000
)

// ErrInvalidTraversalDepth is returned when depth is out of valid range
var ErrInvalidTraversalDepth = fmt.Errorf("traversal depth out of valid range [%d, %d]", MinTraversalDepth, MaxAllowedTraversalDepth)

// ErrInvalidMaxResults is returned when MaxResults is negative
var ErrInvalidMaxResults = fmt.Errorf("MaxResults must be non-negative")

// ValidateTraversalOptions validates and normalizes traversal options.
// Returns an error if options are invalid.
// Note: MaxDepth=0 is valid and means "only return the start node".
func ValidateTraversalOptions(opts *TraversalOptions) error {
	// Validate depth (0 is valid - means only start node)
	if opts.MaxDepth < MinTraversalDepth {
		return fmt.Errorf("%w: got %d", ErrInvalidTraversalDepth, opts.MaxDepth)
	}
	if opts.MaxDepth > MaxAllowedTraversalDepth {
		return fmt.Errorf("%w: got %d (max %d)", ErrInvalidTraversalDepth, opts.MaxDepth, MaxAllowedTraversalDepth)
	}

	// Note: We don't apply a default for MaxDepth=0 because it has a valid meaning
	// (return only the start node). Callers who want the default should use -1 or
	// call WithDefaultDepth() explicitly.

	// Validate MaxResults
	if opts.MaxResults < 0 {
		return ErrInvalidMaxResults
	}

	// Apply default MaxResults if not set
	if opts.MaxResults == 0 {
		opts.MaxResults = DefaultMaxResults
	} else if opts.MaxResults > MaxAllowedResults {
		opts.MaxResults = MaxAllowedResults
		log.Printf("Warning: MaxResults capped to %d", MaxAllowedResults)
	}

	return nil
}

// WithDefaultDepth returns the depth or DefaultMaxTraversalDepth if depth is <= 0.
// Use this when you want to apply a default for unspecified depths.
func WithDefaultDepth(depth int) int {
	if depth <= 0 {
		return DefaultMaxTraversalDepth
	}
	if depth > MaxAllowedTraversalDepth {
		return MaxAllowedTraversalDepth
	}
	return depth
}

// TraversalOptions configures graph traversal
type TraversalOptions struct {
	StartNodeID   uint64
	Direction     Direction
	EdgeTypes     []string                 // Filter by edge types (empty = all types)
	MaxDepth      int                      // Maximum traversal depth
	MaxResults    int                      // Maximum nodes to return
	Predicate     func(*storage.Node) bool // Node filter function
	EdgePredicate func(*storage.Edge) bool // Edge filter function (for temporal/property filtering)
	FailOnMissing bool                     // If true, return error on first missing node; if false, track and continue
}

// TraversalError records an error encountered during traversal
type TraversalError struct {
	NodeID uint64
	Err    error
}

func (te TraversalError) Error() string {
	return fmt.Sprintf("node %d: %v", te.NodeID, te.Err)
}

// TraversalResult contains the results of a traversal
type TraversalResult struct {
	Nodes      []*storage.Node
	Paths      []Path
	SkippedIDs []uint64         // Node IDs that were skipped due to errors
	Errors     []TraversalError // Errors encountered during traversal
}

// Path represents a path through the graph
type Path struct {
	Nodes []*storage.Node
	Edges []*storage.Edge
}

// Traverser performs graph traversals
type Traverser struct {
	storage *storage.GraphStorage
}

// NewTraverser creates a new traverser
func NewTraverser(storage *storage.GraphStorage) *Traverser {
	return &Traverser{storage: storage}
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
