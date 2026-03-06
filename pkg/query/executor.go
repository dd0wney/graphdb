package query

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

const (
	// DefaultQueryTimeout is the default timeout for query execution
	DefaultQueryTimeout = 30 * time.Second

	// MaxQueryTimeout is the maximum allowed query timeout
	MaxQueryTimeout = 5 * time.Minute
)

// Executor executes parsed queries against a graph
type Executor struct {
	graph        *storage.GraphStorage
	optimizer    *Optimizer
	cache        *QueryCache
	queryTimeout time.Duration
	searchIndex  any // *search.FullTextIndex, stored as any to avoid import cycle

	// Vector search closures (set via SetVectorSearch)
	vectorSimilarity VectorSimilarityFunc
	vectorSearch     VectorSearchFunc
	hasVectorIndex   HasVectorIndexFunc
	getNode          GetNodeFunc
}

// NewExecutor creates a new query executor
func NewExecutor(graph *storage.GraphStorage) *Executor {
	return &Executor{
		graph:        graph,
		optimizer:    NewOptimizer(graph),
		cache:        NewQueryCache(),
		queryTimeout: DefaultQueryTimeout,
	}
}

// NewExecutorWithTimeout creates a new query executor with custom timeout
func NewExecutorWithTimeout(graph *storage.GraphStorage, timeout time.Duration) *Executor {
	return &Executor{
		graph:        graph,
		optimizer:    NewOptimizer(graph),
		cache:        NewQueryCache(),
		queryTimeout: ValidateQueryTimeout(timeout),
	}
}

// SetQueryTimeout sets the query timeout
func (e *Executor) SetQueryTimeout(timeout time.Duration) {
	e.queryTimeout = ValidateQueryTimeout(timeout)
}

// Execute executes a query and returns results.
// Includes panic recovery to prevent server crashes from malformed queries.
// Uses the default query timeout.
func (e *Executor) Execute(query *Query) (*ResultSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.queryTimeout)
	defer cancel()
	return e.ExecuteWithContext(ctx, query)
}

// ExecuteWithContext executes a query with context for cancellation and timeout support.
// Includes panic recovery to prevent server crashes from malformed queries.
func (e *Executor) ExecuteWithContext(ctx context.Context, query *Query) (result *ResultSet, err error) {
	// Panic recovery - prevent server crashes from query execution panics
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Printf("PANIC in query execution: %v\n%s", r, stack)
			err = fmt.Errorf("query execution panicked: %v", r)
			result = nil
		}
	}()

	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("query cancelled before execution: %w", ctx.Err())
	default:
	}

	// Handle UNION before normal execution
	if query.Union != nil && query.UnionNext != nil {
		return e.executeUnion(ctx, query)
	}

	// Build execution plan
	plan := e.buildExecutionPlan(query)

	// Check for cancellation after planning
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("query cancelled during planning: %w", ctx.Err())
	default:
	}

	// Optimize plan
	optimizedPlan := e.optimizer.Optimize(plan, query)

	// EXPLAIN: return the plan without executing
	if query.Explain {
		return buildExplainResult(optimizedPlan), nil
	}

	// PROFILE: execute with timing instrumentation
	if query.Profile {
		return e.executeWithProfiling(ctx, optimizedPlan, query)
	}

	// Handle WITH chaining — needs bindings, not just results
	if query.With != nil && query.Next != nil {
		return e.executeWithChain(ctx, optimizedPlan, query)
	}

	// Execute optimized plan with context
	return e.executePlanWithContext(ctx, optimizedPlan, query)
}

// executeWithChain handles WITH clause chaining between query segments
func (e *Executor) executeWithChain(ctx context.Context, plan *ExecutionPlan, query *Query) (*ResultSet, error) {
	execCtx := &ExecutionContext{
		context:  ctx,
		graph:    e.graph,
		bindings: make(map[string]any),
		results:  make([]*BindingSet, 0),
	}

	// Use initial bindings if provided, otherwise start with empty binding
	if query.InitialBindings != nil {
		execCtx.results = query.InitialBindings
	} else {
		execCtx.results = append(execCtx.results, &BindingSet{
			bindings: make(map[string]any),
		})
	}

	// Execute each step
	for _, step := range plan.Steps {
		if err := execCtx.CheckCancellation(); err != nil {
			return nil, err
		}
		if err := step.Execute(execCtx); err != nil {
			return nil, err
		}
	}

	// Project bindings through WITH items
	computer := &AggregationComputer{}
	projectedBindings := make([]*BindingSet, 0, len(execCtx.results))

	for _, binding := range execCtx.results {
		newBinding := &BindingSet{bindings: make(map[string]any)}

		for _, item := range query.With.Items {
			alias := item.Alias
			if alias == "" && item.Expression != nil {
				alias = item.Expression.Variable
			}
			if alias == "" {
				continue
			}

			// Extract the value from the current binding
			if item.ValueExpr != nil {
				newBinding.bindings[alias] = extractValue(item.ValueExpr, binding.bindings)
			} else if item.Expression != nil {
				if item.Expression.Property == "" {
					// Pass the whole node/variable through
					newBinding.bindings[alias] = binding.bindings[item.Expression.Variable]
				} else {
					newBinding.bindings[alias] = e.extractValueFromBinding(binding, item.Expression, computer)
				}
			}
		}

		projectedBindings = append(projectedBindings, newBinding)
	}

	// Apply optional WITH WHERE filter
	if query.With.Where != nil {
		filtered := make([]*BindingSet, 0)
		for _, binding := range projectedBindings {
			match, err := query.With.Where.Expression.Eval(binding.bindings)
			if err != nil {
				continue
			}
			if match {
				filtered = append(filtered, binding)
			}
		}
		projectedBindings = filtered
	}

	// Execute the next query segment with projected bindings as initial state
	query.Next.InitialBindings = projectedBindings
	return e.ExecuteWithContext(ctx, query.Next)
}

// ExecuteWithParams executes a parameterized query. Parameters are provided as a map
// and injected into the query before execution. ParameterRef values in property maps
// are resolved to actual values, and parameters are made available in bindings as "$name" keys.
func (e *Executor) ExecuteWithParams(query *Query, params map[string]any) (*ResultSet, error) {
	// Validate and resolve ParameterRef values in property maps
	if err := resolveParameters(query, params); err != nil {
		return nil, err
	}

	// Validate that all ParameterExpression references have corresponding params
	if err := validateParameterExpressions(query, params); err != nil {
		return nil, err
	}

	// Inject params into initial bindings with "$" prefix to avoid collision with variables
	bindings := &BindingSet{bindings: make(map[string]any)}
	for k, v := range params {
		bindings.bindings["$"+k] = v
	}
	query.InitialBindings = []*BindingSet{bindings}

	return e.Execute(query)
}

// resolveParameters replaces ParameterRef values in pattern property maps with actual param values
func resolveParameters(query *Query, params map[string]any) error {
	if query.Match != nil {
		for _, pattern := range query.Match.Patterns {
			if err := resolvePatternParams(pattern, params); err != nil {
				return err
			}
		}
	}
	if query.Create != nil {
		for _, pattern := range query.Create.Patterns {
			if err := resolvePatternParams(pattern, params); err != nil {
				return err
			}
		}
	}
	if query.Merge != nil {
		if err := resolvePatternParams(query.Merge.Pattern, params); err != nil {
			return err
		}
	}
	return nil
}

func resolvePatternParams(pattern *Pattern, params map[string]any) error {
	for _, node := range pattern.Nodes {
		resolved, err := resolvePropertyParams(node.Properties, params)
		if err != nil {
			return err
		}
		node.Properties = resolved
	}
	for _, rel := range pattern.Relationships {
		resolved, err := resolvePropertyParams(rel.Properties, params)
		if err != nil {
			return err
		}
		rel.Properties = resolved
	}
	return nil
}

// resolvePropertyParams returns a new map with ParameterRef values replaced by actual values.
// The original map is not modified, making repeated calls with different params safe.
func resolvePropertyParams(props map[string]any, params map[string]any) (map[string]any, error) {
	if props == nil {
		return nil, nil
	}
	resolved := make(map[string]any, len(props))
	for key, val := range props {
		if ref, ok := val.(*ParameterRef); ok {
			actual, exists := params[ref.Name]
			if !exists {
				return nil, fmt.Errorf("missing parameter: $%s", ref.Name)
			}
			resolved[key] = actual
		} else {
			resolved[key] = val
		}
	}
	return resolved, nil
}

// validateParameterExpressions walks all expression trees in the query to find
// ParameterExpression nodes and ensures corresponding params exist.
func validateParameterExpressions(query *Query, params map[string]any) error {
	if query.Where != nil {
		if err := validateExprParams(query.Where.Expression, params); err != nil {
			return err
		}
	}
	for _, om := range query.OptionalMatches {
		if om.Where != nil {
			if err := validateExprParams(om.Where.Expression, params); err != nil {
				return err
			}
		}
	}
	if query.Return != nil {
		for _, item := range query.Return.Items {
			if item.ValueExpr != nil {
				if err := validateExprParams(item.ValueExpr, params); err != nil {
					return err
				}
			}
		}
	}
	if query.With != nil {
		if query.With.Where != nil {
			if err := validateExprParams(query.With.Where.Expression, params); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateExprParams(expr Expression, params map[string]any) error {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ParameterExpression:
		if _, ok := params[e.Name]; !ok {
			return fmt.Errorf("missing parameter: $%s", e.Name)
		}
	case *BinaryExpression:
		if err := validateExprParams(e.Left, params); err != nil {
			return err
		}
		return validateExprParams(e.Right, params)
	case *FunctionCallExpression:
		for _, arg := range e.Args {
			if err := validateExprParams(arg, params); err != nil {
				return err
			}
		}
	case *CaseExpression:
		if err := validateExprParams(e.Operand, params); err != nil {
			return err
		}
		for _, wc := range e.WhenClauses {
			if err := validateExprParams(wc.Condition, params); err != nil {
				return err
			}
			if err := validateExprParams(wc.Result, params); err != nil {
				return err
			}
		}
		if err := validateExprParams(e.ElseResult, params); err != nil {
			return err
		}
	}
	return nil
}

// executeUnion executes two query segments and combines their results.
// UNION deduplicates rows; UNION ALL preserves all rows.
func (e *Executor) executeUnion(ctx context.Context, query *Query) (*ResultSet, error) {
	// Execute first segment via a shallow copy to avoid mutating the original AST
	firstSegment := *query
	firstSegment.Union = nil
	firstSegment.UnionNext = nil

	first, err := e.ExecuteWithContext(ctx, &firstSegment)
	if err != nil {
		return nil, fmt.Errorf("UNION first segment: %w", err)
	}

	// Execute second segment (handles chained UNIONs recursively)
	second, err := e.ExecuteWithContext(ctx, query.UnionNext)
	if err != nil {
		return nil, fmt.Errorf("UNION second segment: %w", err)
	}

	// Validate column count
	if len(first.Columns) != len(second.Columns) {
		return nil, fmt.Errorf("UNION column count mismatch: first has %d columns, second has %d",
			len(first.Columns), len(second.Columns))
	}

	// Remap second segment's rows to first segment's column names
	combined := &ResultSet{
		Columns: first.Columns,
		Rows:    make([]map[string]any, 0, len(first.Rows)+len(second.Rows)),
	}

	combined.Rows = append(combined.Rows, first.Rows...)

	for _, row := range second.Rows {
		remapped := make(map[string]any, len(first.Columns))
		for i, col := range first.Columns {
			if i < len(second.Columns) {
				remapped[col] = row[second.Columns[i]]
			}
		}
		combined.Rows = append(combined.Rows, remapped)
	}

	// Deduplicate for UNION (not UNION ALL)
	if !query.Union.All {
		combined.Rows = deduplicateRows(combined.Rows, first.Columns)
	}

	combined.Count = len(combined.Rows)
	return combined, nil
}

// deduplicateRows removes duplicate rows based on all column values
func deduplicateRows(rows []map[string]any, columns []string) []map[string]any {
	seen := make(map[string]bool)
	result := make([]map[string]any, 0, len(rows))

	for _, row := range rows {
		key := rowKey(row, columns)
		if !seen[key] {
			seen[key] = true
			result = append(result, row)
		}
	}
	return result
}

// rowKey builds a string key from row values for deduplication.
// Uses type-prefixed encoding to distinguish nil from "" and int from float.
func rowKey(row map[string]any, columns []string) string {
	var b strings.Builder
	for i, col := range columns {
		if i > 0 {
			b.WriteByte(0)
		}
		v := row[col]
		if v == nil {
			b.WriteString("N:")
		} else {
			fmt.Fprintf(&b, "V:%v", v)
		}
	}
	return b.String()
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
