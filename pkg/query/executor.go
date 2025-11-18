package query

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

const (
	// invalidColumnName is returned when column name cannot be determined
	invalidColumnName = "<invalid>"
)

// Executor executes parsed queries against a graph
type Executor struct {
	graph     *storage.GraphStorage
	optimizer *Optimizer
	cache     *QueryCache
}

// NewExecutor creates a new query executor
func NewExecutor(graph *storage.GraphStorage) *Executor {
	return &Executor{
		graph:     graph,
		optimizer: NewOptimizer(graph),
		cache:     NewQueryCache(),
	}
}

// Execute executes a query and returns results
func (e *Executor) Execute(query *Query) (*ResultSet, error) {
	// Build execution plan
	plan := e.buildExecutionPlan(query)

	// Optimize plan
	optimizedPlan := e.optimizer.Optimize(plan, query)

	// Execute optimized plan
	return e.executePlan(optimizedPlan, query)
}

// ExecuteWithText executes a query from text and uses query caching
func (e *Executor) ExecuteWithText(queryText string, query *Query) (*ResultSet, error) {
	// Check cache
	cachedPlan, found := e.cache.Get(queryText)

	var plan *ExecutionPlan

	if found {
		// Use cached plan
		plan = cachedPlan
	} else {
		// Build and optimize plan
		plan = e.buildExecutionPlan(query)
		plan = e.optimizer.Optimize(plan, query)

		// Cache the optimized plan
		e.cache.Put(queryText, plan)
	}

	// Execute plan
	result, err := e.executePlan(plan, query)

	// Record execution statistics (would need timing)
	// e.cache.RecordExecution(queryText, executionTime, true)

	return result, err
}

// ExecutionPlan represents a query execution plan
type ExecutionPlan struct {
	Steps []ExecutionStep
}

// ExecutionStep represents a single step in execution
type ExecutionStep interface {
	Execute(ctx *ExecutionContext) error
}

// ExecutionContext holds execution state
type ExecutionContext struct {
	graph    *storage.GraphStorage
	bindings map[string]interface{} // Variable bindings
	results  []*BindingSet
}

// BindingSet represents a set of variable bindings
type BindingSet struct {
	bindings map[string]interface{}
}

// ResultSet represents query results
type ResultSet struct {
	Columns []string
	Rows    []map[string]interface{}
	Count   int
}

// buildExecutionPlan creates an execution plan from a query
func (e *Executor) buildExecutionPlan(query *Query) *ExecutionPlan {
	plan := &ExecutionPlan{
		Steps: make([]ExecutionStep, 0),
	}

	// Add MATCH step
	if query.Match != nil {
		plan.Steps = append(plan.Steps, &MatchStep{match: query.Match})
	}

	// Add WHERE filter step
	if query.Where != nil {
		plan.Steps = append(plan.Steps, &FilterStep{where: query.Where})
	}

	// Add CREATE step
	if query.Create != nil {
		plan.Steps = append(plan.Steps, &CreateStep{create: query.Create})
	}

	// Add SET step
	if query.Set != nil {
		plan.Steps = append(plan.Steps, &SetStep{set: query.Set})
	}

	// Add DELETE step
	if query.Delete != nil {
		plan.Steps = append(plan.Steps, &DeleteStep{delete: query.Delete})
	}

	// Add RETURN projection step
	if query.Return != nil {
		plan.Steps = append(plan.Steps, &ReturnStep{
			returnClause: query.Return,
			limit:        query.Limit,
			skip:         query.Skip,
		})
	}

	return plan
}

// executePlan executes an execution plan
func (e *Executor) executePlan(plan *ExecutionPlan, query *Query) (*ResultSet, error) {
	ctx := &ExecutionContext{
		graph:    e.graph,
		bindings: make(map[string]interface{}),
		results:  make([]*BindingSet, 0),
	}

	// Start with empty binding
	ctx.results = append(ctx.results, &BindingSet{
		bindings: make(map[string]interface{}),
	})

	// Execute each step
	for _, step := range plan.Steps {
		if err := step.Execute(ctx); err != nil {
			return nil, err
		}
	}

	// Build final result set
	if query.Return != nil {
		return e.buildResultSet(ctx, query.Return, query.Limit, query.Skip), nil
	}

	// For write queries, return count
	return &ResultSet{
		Columns: []string{"affected"},
		Rows:    []map[string]interface{}{{"affected": len(ctx.results)}},
		Count:   len(ctx.results),
	}, nil
}


// FilterStep executes a WHERE clause
type FilterStep struct {
	where *WhereClause
}

func (fs *FilterStep) Execute(ctx *ExecutionContext) error {
	filtered := make([]*BindingSet, 0)

	for _, binding := range ctx.results {
		// Evaluate expression with this binding
		match, err := fs.where.Expression.Eval(binding.bindings)
		if err != nil {
			continue
		}

		if match {
			filtered = append(filtered, binding)
		}
	}

	ctx.results = filtered
	return nil
}

// convertToStorageValue converts a generic interface{} value to storage.Value
func convertToStorageValue(val interface{}) storage.Value {
	switch v := val.(type) {
	case string:
		return storage.StringValue(v)
	case int64:
		return storage.IntValue(v)
	case float64:
		return storage.FloatValue(v)
	case bool:
		return storage.BoolValue(v)
	default:
		return storage.StringValue(fmt.Sprintf("%v", v))
	}
}

// CreateStep executes a CREATE clause
type CreateStep struct {
	create *CreateClause
}

func (cs *CreateStep) Execute(ctx *ExecutionContext) error {
	for _, pattern := range cs.create.Patterns {
		for _, nodePattern := range pattern.Nodes {
			// Convert properties
			props := make(map[string]storage.Value)
			for key, val := range nodePattern.Properties {
				props[key] = convertToStorageValue(val)
			}

			// Create node
			node, err := ctx.graph.CreateNode(nodePattern.Labels, props)
			if err != nil {
				return err
			}

			// Bind variable
			if nodePattern.Variable != "" {
				for _, binding := range ctx.results {
					binding.bindings[nodePattern.Variable] = node
				}
			}
		}

		// Create relationships
		for _, relPattern := range pattern.Relationships {
			// Safe type assertions with validation
			if len(ctx.results) == 0 {
				return fmt.Errorf("no bindings available for relationship creation")
			}

			fromInterface, exists := ctx.results[0].bindings[relPattern.From.Variable]
			if !exists {
				return fmt.Errorf("from node variable '%s' not bound", relPattern.From.Variable)
			}
			fromNode, ok := fromInterface.(*storage.Node)
			if !ok {
				return fmt.Errorf("from node variable '%s' is not a Node", relPattern.From.Variable)
			}

			toInterface, exists := ctx.results[0].bindings[relPattern.To.Variable]
			if !exists {
				return fmt.Errorf("to node variable '%s' not bound", relPattern.To.Variable)
			}
			toNode, ok := toInterface.(*storage.Node)
			if !ok {
				return fmt.Errorf("to node variable '%s' is not a Node", relPattern.To.Variable)
			}

			props := make(map[string]storage.Value)
			for key, val := range relPattern.Properties {
				props[key] = convertToStorageValue(val)
			}

			_, err := ctx.graph.CreateEdge(fromNode.ID, toNode.ID, relPattern.Type, props, 1.0)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// SetStep executes a SET clause
type SetStep struct {
	set *SetClause
}

func (ss *SetStep) Execute(ctx *ExecutionContext) error {
	for _, binding := range ctx.results {
		for _, assignment := range ss.set.Assignments {
			// Get node from binding
			if obj, ok := binding.bindings[assignment.Variable]; ok {
				if node, ok := obj.(*storage.Node); ok {
					// Create updated properties map
					updatedProps := make(map[string]storage.Value)
					for k, v := range node.Properties {
						updatedProps[k] = v
					}
					updatedProps[assignment.Property] = convertToStorageValue(assignment.Value)

					// Update in storage
					if err := ctx.graph.UpdateNode(node.ID, updatedProps); err != nil {
						return fmt.Errorf("failed to update node %d: %w", node.ID, err)
					}
				}
			}
		}
	}

	return nil
}

// DeleteStep executes a DELETE clause
type DeleteStep struct {
	delete *DeleteClause
}

func (ds *DeleteStep) Execute(ctx *ExecutionContext) error {
	for _, binding := range ctx.results {
		for _, variable := range ds.delete.Variables {
			if obj, ok := binding.bindings[variable]; ok {
				if node, ok := obj.(*storage.Node); ok {
					// Delete node (DeleteNode automatically handles edge deletion)
					if err := ctx.graph.DeleteNode(node.ID); err != nil {
						return fmt.Errorf("failed to delete node %d: %w", node.ID, err)
					}
				}
			}
		}
	}

	return nil
}

// ReturnStep executes a RETURN clause
type ReturnStep struct {
	returnClause *ReturnClause
	limit        int
	skip         int
}

func (rs *ReturnStep) Execute(ctx *ExecutionContext) error {
	// SKIP and LIMIT are applied in buildResultSet, not here
	// This prevents double-applying pagination
	return nil
}

// buildColumnName builds a column name from a return item
func buildColumnName(item *ReturnItem) string {
	if item.Alias != "" {
		return item.Alias
	}

	if item.Aggregate != "" {
		if item.Expression != nil {
			return fmt.Sprintf("%s(%s.%s)", item.Aggregate, item.Expression.Variable, item.Expression.Property)
		}
		return fmt.Sprintf("%s(*)", item.Aggregate)
	}

	if item.Expression != nil {
		return fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)
	}

	return invalidColumnName
}

// buildAggregateResultSet builds results for aggregate queries without GROUP BY
func (e *Executor) buildAggregateResultSet(ctx *ExecutionContext, returnClause *ReturnClause) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]interface{}, 0),
	}

	computer := &AggregationComputer{}
	aggregateResult := computer.ComputeAggregates(ctx, returnClause.Items)

	// Build columns
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	// Add single row with aggregate results
	resultSet.Rows = append(resultSet.Rows, aggregateResult)
	resultSet.Count = 1
	return resultSet
}

// applyPostProcessing applies DISTINCT, ORDER BY, SKIP, and LIMIT

// buildGroupedResultSet builds results for GROUP BY queries
func (e *Executor) buildGroupedResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]interface{}, 0),
	}

	computer := &AggregationComputer{}
	groupedResults := computer.ComputeGroupedAggregates(ctx, returnClause.Items, returnClause.GroupBy)

	// Build columns
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	resultSet.Rows = groupedResults
	resultSet.Count = len(groupedResults)

	// Apply post-processing (ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, false)

	return resultSet
}

// buildResultSet builds the final result set
func (e *Executor) buildResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	// Check if we have GROUP BY
	if len(returnClause.GroupBy) > 0 {
		return e.buildGroupedResultSet(ctx, returnClause, limit, skip)
	}

	// Check if we have aggregates (without GROUP BY)
	if hasAggregates(returnClause.Items) {
		return e.buildAggregateResultSet(ctx, returnClause)
	}

	// Build regular results
	return e.buildRegularResultSet(ctx, returnClause, limit, skip)
}

// buildRegularResultSet builds results for regular (non-aggregate) queries
func (e *Executor) buildRegularResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]interface{}, 0),
	}

	// Determine columns
	for _, item := range returnClause.Items {
		resultSet.Columns = append(resultSet.Columns, buildColumnName(item))
	}

	// Build rows
	computer := &AggregationComputer{}
	for _, binding := range ctx.results {
		row := make(map[string]interface{})

		for i, item := range returnClause.Items {
			columnName := resultSet.Columns[i]

			// Extract value - check for nil Expression first
			if item.Expression != nil {
				if obj, ok := binding.bindings[item.Expression.Variable]; ok {
					if node, ok := obj.(*storage.Node); ok {
						if item.Expression.Property != "" {
							if prop, exists := node.Properties[item.Expression.Property]; exists {
								// Reuse ExtractValue from AggregationComputer
								row[columnName] = computer.ExtractValue(prop)
							}
						} else {
							// Return whole node
							row[columnName] = node
						}
					}
				}
			}
		}

		resultSet.Rows = append(resultSet.Rows, row)
	}

	// Apply post-processing (DISTINCT, ORDER BY, SKIP, LIMIT)
	e.applyPostProcessing(resultSet, returnClause, limit, skip, true)

	return resultSet
}

