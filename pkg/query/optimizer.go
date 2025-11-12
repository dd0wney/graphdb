package query

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Optimizer optimizes query execution plans
type Optimizer struct {
	graph *storage.GraphStorage
	stats *storage.Statistics
}

// NewOptimizer creates a new query optimizer
func NewOptimizer(graph *storage.GraphStorage) *Optimizer {
	return &Optimizer{
		graph: graph,
		stats: &storage.Statistics{}, // Would get from graph.GetStatistics()
	}
}

// Optimize optimizes an execution plan
func (o *Optimizer) Optimize(plan *ExecutionPlan, query *Query) *ExecutionPlan {
	optimized := &ExecutionPlan{
		Steps: make([]ExecutionStep, 0, len(plan.Steps)),
	}

	// Apply optimization rules
	optimized = o.applyIndexSelection(plan, query)
	optimized = o.applyFilterPushdown(optimized, query)
	optimized = o.applyJoinOrdering(optimized, query)
	optimized = o.applyEarlyTermination(optimized, query)

	return optimized
}

// applyIndexSelection chooses optimal indexes for property lookups
func (o *Optimizer) applyIndexSelection(plan *ExecutionPlan, query *Query) *ExecutionPlan {
	optimized := &ExecutionPlan{Steps: make([]ExecutionStep, 0)}

	for _, step := range plan.Steps {
		// Check if this is a MatchStep that could use an index
		if matchStep, ok := step.(*MatchStep); ok {
			optimizedStep := o.optimizeMatchWithIndex(matchStep, query)
			optimized.Steps = append(optimized.Steps, optimizedStep)
		} else {
			optimized.Steps = append(optimized.Steps, step)
		}
	}

	return optimized
}

// optimizeMatchWithIndex optimizes a match step to use indexes when available
func (o *Optimizer) optimizeMatchWithIndex(match *MatchStep, query *Query) ExecutionStep {
	// If WHERE clause has property filters, use index lookup
	if query.Where != nil {
		// Check for property equality filters
		// Example: WHERE n.name = "Alice" -> Use index on "name" if available
		// This would require analyzing the WHERE AST to find property filters
		// For now, return the original step
	}

	return match
}

// applyFilterPushdown moves filters as early as possible in execution
func (o *Optimizer) applyFilterPushdown(plan *ExecutionPlan, query *Query) *ExecutionPlan {
	optimized := &ExecutionPlan{Steps: make([]ExecutionStep, 0)}

	// Find filter steps
	var filterSteps []*FilterStep
	var otherSteps []ExecutionStep

	for _, step := range plan.Steps {
		if filterStep, ok := step.(*FilterStep); ok {
			filterSteps = append(filterSteps, filterStep)
		} else {
			otherSteps = append(otherSteps, step)
		}
	}

	// Apply filters as early as possible
	// For now, just reconstruct the plan
	for _, step := range otherSteps {
		optimized.Steps = append(optimized.Steps, step)

		// Insert applicable filters after match steps
		if _, ok := step.(*MatchStep); ok {
			for _, filter := range filterSteps {
				optimized.Steps = append(optimized.Steps, ExecutionStep(filter))
			}
			filterSteps = nil // Clear applied filters
		}
	}

	// Add any remaining filters
	for _, filter := range filterSteps {
		optimized.Steps = append(optimized.Steps, filter)
	}

	return optimized
}

// applyJoinOrdering reorders joins to start with most selective
func (o *Optimizer) applyJoinOrdering(plan *ExecutionPlan, query *Query) *ExecutionPlan {
	// For multi-pattern matches, start with most selective patterns
	// Example: MATCH (a:Person {age: 30}), (b:Person)
	// Should process (a) first as it's more selective

	// This requires cardinality estimation
	// For now, return plan as-is
	return plan
}

// applyEarlyTermination adds LIMIT pushdown where possible
func (o *Optimizer) applyEarlyTermination(plan *ExecutionPlan, query *Query) *ExecutionPlan {
	if query.Limit == 0 {
		return plan
	}

	// If query has LIMIT and no ORDER BY, we can terminate early
	// This is already handled in ReturnStep, so no changes needed
	return plan
}

// estimateCardinality estimates result cardinality for a pattern
func (o *Optimizer) estimateCardinality(pattern *MatchClause) int {
	// Estimate number of results for a match pattern
	if pattern == nil || len(pattern.Patterns) == 0 {
		return 1000
	}

	// For now, estimate based on label if available
	firstPattern := pattern.Patterns[0]
	if len(firstPattern.Nodes) > 0 && len(firstPattern.Nodes[0].Labels) > 0 {
		label := firstPattern.Nodes[0].Labels[0]
		// Get actual count from graph statistics
		labelNodes, err := o.graph.FindNodesByLabel(label)
		if err == nil {
			return len(labelNodes)
		}
	}

	// Default estimation
	return 1000
}

// EstimateCost estimates the cost of executing a match pattern
func (o *Optimizer) EstimateCost(pattern *MatchClause) float64 {
	if pattern == nil || len(pattern.Patterns) == 0 {
		return 1000.0
	}

	// Basic cost model: primarily based on cardinality
	cardinality := float64(o.estimateCardinality(pattern))

	// Add cost multipliers for complexity
	cost := cardinality

	firstPattern := pattern.Patterns[0]

	// Add cost for relationships (joins are expensive)
	if len(firstPattern.Relationships) > 0 {
		cost *= float64(len(firstPattern.Relationships) + 1)
	}

	// Add cost for property filters (if properties are specified in pattern)
	if len(firstPattern.Nodes) > 0 && len(firstPattern.Nodes[0].Properties) > 0 {
		// Property filters reduce cardinality but add filtering cost
		cost *= 0.5 // Assume 50% selectivity on average
	}

	return cost
}

// OptimizationHint provides hints about potential optimizations
type OptimizationHint struct {
	Type          string // "index_available", "filter_early", "join_reorder"
	Description   string
	EstimatedGain float64 // Estimated speedup multiplier
}

// AnalyzeQuery analyzes a query and suggests optimizations
func (o *Optimizer) AnalyzeQuery(query *Query) []OptimizationHint {
	hints := make([]OptimizationHint, 0)

	// Check for missing indexes
	if query.Where != nil {
		// Would analyze WHERE clause for property filters
		// and check if indexes exist
		hints = append(hints, OptimizationHint{
			Type:          "index_available",
			Description:   "Consider creating index on frequently filtered properties",
			EstimatedGain: 10.0,
		})
	}

	// Check for suboptimal join order
	if query.Match != nil {
		// Would analyze match patterns
		hints = append(hints, OptimizationHint{
			Type:          "join_reorder",
			Description:   "Start with most selective pattern",
			EstimatedGain: 2.0,
		})
	}

	return hints
}

// QueryStatistics tracks query execution statistics
type QueryStatistics struct {
	QueryText          string
	ExecutionCount     int
	TotalExecutionTime int64 // microseconds
	AvgExecutionTime   int64
	LastOptimized      bool
}

// QueryCache caches compiled/optimized queries
type QueryCache struct {
	cache map[string]*ExecutionPlan
	stats map[string]*QueryStatistics
}

// NewQueryCache creates a new query cache
func NewQueryCache() *QueryCache {
	return &QueryCache{
		cache: make(map[string]*ExecutionPlan),
		stats: make(map[string]*QueryStatistics),
	}
}

// Get retrieves a cached plan
func (qc *QueryCache) Get(queryText string) (*ExecutionPlan, bool) {
	plan, ok := qc.cache[queryText]
	return plan, ok
}

// Put stores a plan in cache
func (qc *QueryCache) Put(queryText string, plan *ExecutionPlan) {
	qc.cache[queryText] = plan
}

// RecordExecution records query execution statistics
func (qc *QueryCache) RecordExecution(queryText string, executionTimeMicros int64, optimized bool) {
	stats, exists := qc.stats[queryText]
	if !exists {
		stats = &QueryStatistics{
			QueryText: queryText,
		}
		qc.stats[queryText] = stats
	}

	stats.ExecutionCount++
	stats.TotalExecutionTime += executionTimeMicros
	stats.AvgExecutionTime = stats.TotalExecutionTime / int64(stats.ExecutionCount)
	stats.LastOptimized = optimized
}

// GetTopQueries returns most frequently executed queries
func (qc *QueryCache) GetTopQueries(limit int) []*QueryStatistics {
	allStats := make([]*QueryStatistics, 0, len(qc.stats))
	for _, stat := range qc.stats {
		allStats = append(allStats, stat)
	}

	// Sort by execution count (descending)
	// Simple selection for top N
	if limit > len(allStats) {
		limit = len(allStats)
	}

	result := make([]*QueryStatistics, 0, limit)
	for i := 0; i < limit && i < len(allStats); i++ {
		maxIdx := i
		for j := i + 1; j < len(allStats); j++ {
			if allStats[j].ExecutionCount > allStats[maxIdx].ExecutionCount {
				maxIdx = j
			}
		}
		if maxIdx != i {
			allStats[i], allStats[maxIdx] = allStats[maxIdx], allStats[i]
		}
		result = append(result, allStats[i])
	}

	return result
}
