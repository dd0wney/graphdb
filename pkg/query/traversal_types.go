package query

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/storage"
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

// ErrTraversalTruncated reports that a traversal stopped at an engine limit
// rather than because it ran out of graph.
//
// It travels BESIDE the results, never instead of them: a caller gets what was
// found plus this error, and a nil error is the positive assertion that the
// answer is complete. That is ADR 0003's enumeration rule extended to
// traversal, and it exists because the alternative is a limit that acts and
// does not tell the caller — an answer to a question nobody asked, indistinguishable
// from an answer to the one they did.
var ErrTraversalTruncated = fmt.Errorf("traversal stopped at an engine limit; the result is incomplete")

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
	if opts.MaxResults > MaxAllowedResults {
		return fmt.Errorf("%w: got %d (max %d)", ErrInvalidMaxResults, opts.MaxResults, MaxAllowedResults)
	}

	// Apply default MaxResults if not set (0 has no other valid meaning here,
	// unlike MaxDepth=0, so defaulting it is safe and the owner asked to keep it).
	if opts.MaxResults == 0 {
		opts.MaxResults = DefaultMaxResults
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
	StartNodeID uint64
	Direction   Direction
	EdgeTypes   []string // Filter by edge types (empty = all types)
	MaxDepth    int      // Maximum traversal depth
	// MaxResults bounds the nodes returned. LEAVE IT AT ZERO unless you
	// intend to detect saturation yourself.
	//
	// Zero carries two meanings at once, and that is the trap. It means
	// "apply DefaultMaxResults", and it also means "I did not choose this
	// bound, so tell me if you hit it". BFS and DFS report ErrTraversalTruncated
	// only for a bound the ENGINE chose, because a caller who names a limit and
	// reaches it received exactly what was requested.
	//
	// So naming a bound BUYS SILENCE. The careful-looking move — set it
	// explicitly rather than rely on a default — is the one that switches the
	// signal off. Two independent designs made that move before this comment
	// existed: GetNeighborhood in this package passed DefaultMaxResults by
	// name and suppressed the signal for every one of its callers, and a
	// downstream consumer wrote the same rule into its specification from the
	// constants alone, without seeing GetNeighborhood. Neither was careless.
	//
	// If you genuinely need a bound for memory, name it AND check saturation
	// yourself. The engine will not do it for you once you have named one.
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
