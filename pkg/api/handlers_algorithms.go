package api

import "time"

// Algorithm parameter limits to prevent resource exhaustion
const (
	// Traversal limits
	MinTraversalDepth     = 0
	MaxTraversalDepth     = 100
	DefaultTraversalDepth = 10

	// PageRank limits
	MinPageRankIterations     = 1
	MaxPageRankIterations     = 1000
	DefaultPageRankIterations = 20
	MinDampingFactor          = 0.0
	MaxDampingFactor          = 1.0
	DefaultDampingFactor      = 0.85

	// Cycle detection limits
	MinCycleLength = 1
	MaxCycleLength = 100

	// Algorithm timeout
	DefaultAlgorithmTimeout = 60 * time.Second
	MaxAlgorithmTimeout     = 5 * time.Minute
)

// MaxTraversalNodes caps how many nodes a single /traverse may return.
// Depth and the algorithm timeout alone don't bound output: a dense
// tenant graph materializes every reachable node into one slice + one
// JSON array, pinning memory for the request window (security audit
// H-8). On hitting the cap the traversal stops and the response carries
// X-Truncated: true. It is a var, not a const, only so tests can lower
// it to exercise the cap without building a 10k-node fixture.
var MaxTraversalNodes = 10000
