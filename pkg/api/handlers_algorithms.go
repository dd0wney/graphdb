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
