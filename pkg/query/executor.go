package query

import (
	"fmt"
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
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

// MatchStep executes a MATCH clause
type MatchStep struct {
	match *MatchClause
}

func (ms *MatchStep) Execute(ctx *ExecutionContext) error {
	newResults := make([]*BindingSet, 0)

	// For each existing binding
	for _, binding := range ctx.results {
		// For each pattern
		for _, pattern := range ms.match.Patterns {
			// Find matches for this pattern
			matches, err := ms.matchPattern(ctx, pattern, binding)
			if err != nil {
				return err
			}
			newResults = append(newResults, matches...)
		}
	}

	// Always update results, even if empty
	// If no matches found, results should be empty
	ctx.results = newResults

	return nil
}

func (ms *MatchStep) matchPattern(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Simple case: single node
	if len(pattern.Nodes) == 1 && len(pattern.Relationships) == 0 {
		return ms.matchNode(ctx, pattern.Nodes[0], existingBinding)
	}

	// Pattern with relationships
	if len(pattern.Nodes) >= 2 && len(pattern.Relationships) >= 1 {
		return ms.matchPath(ctx, pattern, existingBinding)
	}

	// Multiple independent nodes (cartesian product)
	if len(pattern.Nodes) >= 2 && len(pattern.Relationships) == 0 {
		return ms.matchCartesianProduct(ctx, pattern, existingBinding)
	}

	return results, nil
}

func (ms *MatchStep) matchNode(ctx *ExecutionContext, nodePattern *NodePattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Get all nodes with matching labels
	stats := ctx.graph.GetStatistics()
	nodeCount := stats.NodeCount

	for nodeID := uint64(1); nodeID <= nodeCount; nodeID++ {
		node, err := ctx.graph.GetNode(nodeID)
		if err != nil {
			continue
		}

		// Check labels
		if len(nodePattern.Labels) > 0 {
			if !ms.hasLabels(node, nodePattern.Labels) {
				continue
			}
		}

		// Check properties
		if !ms.matchProperties(node.Properties, nodePattern.Properties) {
			continue
		}

		// Create new binding
		newBinding := ms.copyBinding(existingBinding)
		if nodePattern.Variable != "" {
			newBinding.bindings[nodePattern.Variable] = node
		}
		results = append(results, newBinding)
	}

	return results, nil
}

func (ms *MatchStep) matchPath(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	results := make([]*BindingSet, 0)

	// Get starting nodes
	startNodePattern := pattern.Nodes[0]
	startNodes, err := ms.matchNode(ctx, startNodePattern, existingBinding)
	if err != nil {
		return nil, err
	}

	// For each starting node, traverse relationships
	for _, startBinding := range startNodes {
		// Safe type assertion with check
		nodeInterface, exists := startBinding.bindings[startNodePattern.Variable]
		if !exists {
			continue // Skip if binding doesn't exist
		}
		startNode, ok := nodeInterface.(*storage.Node)
		if !ok {
			continue // Skip if wrong type
		}

		// Traverse each relationship in pattern
		pathResults := ms.traversePath(ctx, startNode, pattern, 0, startBinding)
		results = append(results, pathResults...)
	}

	return results, nil
}

// matchCartesianProduct handles matching multiple independent nodes (no relationships)
// Returns the cartesian product of all matching nodes
func (ms *MatchStep) matchCartesianProduct(ctx *ExecutionContext, pattern *Pattern, existingBinding *BindingSet) ([]*BindingSet, error) {
	// Start with the existing binding, or create an initial empty one
	results := []*BindingSet{existingBinding}
	if existingBinding == nil {
		results = []*BindingSet{{bindings: make(map[string]interface{})}}
	}

	// For each node pattern, match nodes and compute cartesian product
	for _, nodePattern := range pattern.Nodes {
		newResults := make([]*BindingSet, 0)

		// Create an empty binding for matchNode
		emptyBinding := &BindingSet{bindings: make(map[string]interface{})}

		// Match nodes for this pattern
		nodeMatches, err := ms.matchNode(ctx, nodePattern, emptyBinding)
		if err != nil {
			return nil, err
		}

		// For each existing binding, combine with each matching node
		for _, existingResult := range results {
			for _, nodeMatch := range nodeMatches {
				// Create new binding combining existing and new
				newBinding := ms.copyBinding(existingResult)

				// Add the node binding from nodeMatch
				if nodePattern.Variable != "" {
					newBinding.bindings[nodePattern.Variable] = nodeMatch.bindings[nodePattern.Variable]
				}

				newResults = append(newResults, newBinding)
			}
		}

		results = newResults
	}

	return results, nil
}

func (ms *MatchStep) traversePath(ctx *ExecutionContext, currentNode *storage.Node, pattern *Pattern, relIndex int, currentBinding *BindingSet) []*BindingSet {
	results := make([]*BindingSet, 0)

	// Base case: no more relationships
	if relIndex >= len(pattern.Relationships) {
		return []*BindingSet{currentBinding}
	}

	rel := pattern.Relationships[relIndex]
	targetNodePattern := pattern.Nodes[relIndex+1]

	// Get edges based on direction
	var edges []*storage.Edge
	var err error

	switch rel.Direction {
	case DirectionOutgoing:
		edges, err = ctx.graph.GetOutgoingEdges(currentNode.ID)
	case DirectionIncoming:
		edges, err = ctx.graph.GetIncomingEdges(currentNode.ID)
	case DirectionBoth:
		outgoing, _ := ctx.graph.GetOutgoingEdges(currentNode.ID)
		incoming, _ := ctx.graph.GetIncomingEdges(currentNode.ID)
		edges = append(outgoing, incoming...)
	}

	if err != nil {
		return results
	}

	// Filter edges by type
	for _, edge := range edges {
		if rel.Type != "" && edge.Type != rel.Type {
			continue
		}

		// Get target node
		targetNodeID := edge.ToNodeID
		if rel.Direction == DirectionIncoming {
			targetNodeID = edge.FromNodeID
		}

		targetNode, err := ctx.graph.GetNode(targetNodeID)
		if err != nil {
			continue
		}

		// Check if target matches pattern
		if len(targetNodePattern.Labels) > 0 && !ms.hasLabels(targetNode, targetNodePattern.Labels) {
			continue
		}

		if !ms.matchProperties(targetNode.Properties, targetNodePattern.Properties) {
			continue
		}

		// Create new binding with edge and target node
		newBinding := ms.copyBinding(currentBinding)
		if rel.Variable != "" {
			newBinding.bindings[rel.Variable] = edge
		}
		if targetNodePattern.Variable != "" {
			newBinding.bindings[targetNodePattern.Variable] = targetNode
		}

		// Recursively traverse next relationship
		pathResults := ms.traversePath(ctx, targetNode, pattern, relIndex+1, newBinding)
		results = append(results, pathResults...)
	}

	return results
}

func (ms *MatchStep) hasLabels(node *storage.Node, labels []string) bool {
	for _, requiredLabel := range labels {
		found := false
		for _, nodeLabel := range node.Labels {
			if nodeLabel == requiredLabel {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (ms *MatchStep) matchProperties(nodeProps map[string]storage.Value, patternProps map[string]interface{}) bool {
	for key, patternValue := range patternProps {
		nodeValue, exists := nodeProps[key]
		if !exists {
			return false
		}

		// Simple value comparison
		if !ms.valuesEqual(nodeValue, patternValue) {
			return false
		}
	}
	return true
}

func (ms *MatchStep) valuesEqual(nodeValue storage.Value, patternValue interface{}) bool {
	switch v := patternValue.(type) {
	case string:
		nodeStr, _ := nodeValue.AsString()
		return nodeStr == v
	case int64:
		nodeInt, _ := nodeValue.AsInt()
		return nodeInt == v
	case float64:
		nodeFloat, _ := nodeValue.AsFloat()
		return nodeFloat == v
	case bool:
		nodeBool, _ := nodeValue.AsBool()
		return nodeBool == v
	}
	return false
}

func (ms *MatchStep) copyBinding(binding *BindingSet) *BindingSet {
	newBindings := make(map[string]interface{})
	for k, v := range binding.bindings {
		newBindings[k] = v
	}
	return &BindingSet{bindings: newBindings}
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
				props[key] = cs.convertValue(val)
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
				props[key] = cs.convertValue(val)
			}

			_, err := ctx.graph.CreateEdge(fromNode.ID, toNode.ID, relPattern.Type, props, 1.0)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (cs *CreateStep) convertValue(val interface{}) storage.Value {
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
					updatedProps[assignment.Property] = ss.convertValue(assignment.Value)

					// Update in storage
					ctx.graph.UpdateNode(node.ID, updatedProps)
				}
			}
		}
	}

	return nil
}

func (ss *SetStep) convertValue(val interface{}) storage.Value {
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
					ctx.graph.DeleteNode(node.ID)
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
	// Apply SKIP with bounds checking to prevent panic
	if rs.skip > 0 {
		if rs.skip >= len(ctx.results) {
			// Skip exceeds results, return empty
			ctx.results = ctx.results[:0]
			return nil
		}
		ctx.results = ctx.results[rs.skip:]
	}

	// Apply LIMIT with bounds checking
	if rs.limit > 0 && rs.limit < len(ctx.results) {
		ctx.results = ctx.results[:rs.limit]
	}

	return nil
}

// buildResultSet builds the final result set
func (e *Executor) buildResultSet(ctx *ExecutionContext, returnClause *ReturnClause, limit, skip int) *ResultSet {
	resultSet := &ResultSet{
		Columns: make([]string, 0),
		Rows:    make([]map[string]interface{}, 0),
	}

	// Determine columns
	for _, item := range returnClause.Items {
		columnName := item.Alias
		if columnName == "" {
			// Check for nil Expression to prevent nil pointer dereference
			if item.Expression == nil {
				columnName = "<invalid>"
			} else if item.Aggregate != "" {
				columnName = fmt.Sprintf("%s(%s.%s)", item.Aggregate, item.Expression.Variable, item.Expression.Property)
			} else {
				columnName = fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)
			}
		}
		resultSet.Columns = append(resultSet.Columns, columnName)
	}

	// Build rows
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
								// Extract the actual value based on type
								switch prop.Type {
								case storage.TypeString:
									val, _ := prop.AsString()
									row[columnName] = val
								case storage.TypeInt:
									val, _ := prop.AsInt()
									row[columnName] = val
								case storage.TypeFloat:
									val, _ := prop.AsFloat()
									row[columnName] = val
								case storage.TypeBool:
									val, _ := prop.AsBool()
									row[columnName] = val
								default:
									row[columnName] = prop.Data
								}
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

	// Apply DISTINCT
	if returnClause.Distinct {
		resultSet.Rows = e.deduplicateRows(resultSet.Rows)
	}

	// Apply ORDER BY
	if len(returnClause.OrderBy) > 0 {
		e.sortRows(resultSet.Rows, returnClause.OrderBy)
	}

	resultSet.Count = len(resultSet.Rows)
	return resultSet
}

func (e *Executor) deduplicateRows(rows []map[string]interface{}) []map[string]interface{} {
	seen := make(map[string]bool)
	result := make([]map[string]interface{}, 0)

	for _, row := range rows {
		key := fmt.Sprintf("%v", row)
		if !seen[key] {
			seen[key] = true
			result = append(result, row)
		}
	}

	return result
}

func (e *Executor) sortRows(rows []map[string]interface{}, orderBy []*OrderByItem) {
	if len(orderBy) == 0 {
		return
	}

	sort.Slice(rows, func(i, j int) bool {
		// Compare by first order by item
		item := orderBy[0]
		columnName := fmt.Sprintf("%s.%s", item.Expression.Variable, item.Expression.Property)

		valI := rows[i][columnName]
		valJ := rows[j][columnName]

		cmp := compareValues(valI, valJ)

		if item.Ascending {
			return cmp < 0
		}
		return cmp > 0
	})
}
