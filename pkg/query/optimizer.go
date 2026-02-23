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
	// Apply optimization rules sequentially
	result := o.applyIndexSelection(plan, query)
	result = o.applyFilterPushdown(result, query)
	result = o.applyJoinOrdering(result, query)
	result = o.applyEarlyTermination(result, query)

	return result
}

// applyIndexSelection chooses optimal indexes for property lookups
func (o *Optimizer) applyIndexSelection(plan *ExecutionPlan, query *Query) *ExecutionPlan {
	optimized := &ExecutionPlan{Steps: make([]ExecutionStep, 0)}

	for _, step := range plan.Steps {
		switch s := step.(type) {
		case *MatchStep:
			optimizedStep := o.optimizeMatchWithIndex(s, query)
			optimized.Steps = append(optimized.Steps, optimizedStep)
		default:
			// OptionalMatchStep and others pass through without index optimization
			optimized.Steps = append(optimized.Steps, step)
		}
	}

	return optimized
}

// optimizeMatchWithIndex optimizes a match step to use indexes when available
func (o *Optimizer) optimizeMatchWithIndex(match *MatchStep, query *Query) ExecutionStep {
	// Check if there's a WHERE clause with indexable conditions
	if query.Where == nil {
		return match
	}

	// Try to extract equality conditions from WHERE clause
	indexInfo := o.extractIndexableCondition(query.Where.Expression)
	if indexInfo == nil {
		return match
	}

	// Check if index exists for this property
	if !o.graph.HasPropertyIndex(indexInfo.propertyKey) {
		return match
	}

	// Get the variable and labels from the match pattern
	variable := ""
	var labels []string
	if match.match != nil && len(match.match.Patterns) > 0 {
		pattern := match.match.Patterns[0]
		if len(pattern.Nodes) > 0 {
			variable = pattern.Nodes[0].Variable
			labels = pattern.Nodes[0].Labels
		}
	}

	// Verify the property access matches the variable in the match pattern
	if indexInfo.variable != variable {
		return match // Property is on a different variable
	}

	// Convert value to storage.Value
	storageValue, ok := convertToStorageValueForIndex(indexInfo.value)
	if !ok {
		return match
	}

	// Return IndexLookupStep instead of MatchStep
	return &IndexLookupStep{
		propertyKey: indexInfo.propertyKey,
		value:       storageValue,
		variable:    variable,
		labels:      labels,
	}
}

// indexableCondition holds info about an indexable WHERE condition
type indexableCondition struct {
	variable    string // e.g., "n" from n.name
	propertyKey string // e.g., "name"
	value       any    // the literal value to match
}

// extractIndexableCondition tries to extract an equality condition that can use an index
func (o *Optimizer) extractIndexableCondition(expr Expression) *indexableCondition {
	if expr == nil {
		return nil
	}

	// Handle binary expressions
	binExpr, ok := expr.(*BinaryExpression)
	if !ok {
		return nil
	}

	// For AND expressions, try to extract from either side
	if binExpr.Operator == "AND" {
		if left := o.extractIndexableCondition(binExpr.Left); left != nil {
			return left
		}
		return o.extractIndexableCondition(binExpr.Right)
	}

	// Only handle equality for index lookups
	if binExpr.Operator != "=" {
		return nil
	}

	// Check if left is PropertyExpression and right is LiteralExpression
	propExpr, propOk := binExpr.Left.(*PropertyExpression)
	litExpr, litOk := binExpr.Right.(*LiteralExpression)

	if propOk && litOk {
		return &indexableCondition{
			variable:    propExpr.Variable,
			propertyKey: propExpr.Property,
			value:       litExpr.Value,
		}
	}

	// Also check the reverse: literal = property
	litExpr, litOk = binExpr.Left.(*LiteralExpression)
	propExpr, propOk = binExpr.Right.(*PropertyExpression)

	if propOk && litOk {
		return &indexableCondition{
			variable:    propExpr.Variable,
			propertyKey: propExpr.Property,
			value:       litExpr.Value,
		}
	}

	return nil
}

// convertToStorageValueForIndex converts a Go value to storage.Value for index lookup
func convertToStorageValueForIndex(val any) (storage.Value, bool) {
	switch v := val.(type) {
	case string:
		return storage.StringValue(v), true
	case int:
		return storage.IntValue(int64(v)), true
	case int64:
		return storage.IntValue(v), true
	case float64:
		return storage.FloatValue(v), true
	case bool:
		return storage.BoolValue(v), true
	default:
		return storage.Value{}, false
	}
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

		// Insert applicable filters after match steps (but not after optional match)
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
