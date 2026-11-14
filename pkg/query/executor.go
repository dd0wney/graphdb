package query

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
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
//
// It is ExecuteWithOptions with the zero PathOptions, which is today's traversal
// behaviour. Callers that want the node-visited mode or an expansion-time filter
// call ExecuteWithOptions directly.
func (e *Executor) ExecuteWithContext(ctx context.Context, query *Query) (*ResultSet, error) {
	return e.ExecuteWithOptions(ctx, query, PathOptions{})
}

// ExecuteWithOptions executes a query with per-call variable-length traversal
// policy. See PathOptions.
//
// The policy is a parameter rather than Executor state because pkg/api shares
// one Executor across every request: a field there would change every tenant's
// rows and would race with a query already running.
//
// NOTE: ExecuteWithText does not pass through here. It reads and fills the plan
// cache and calls executePlan directly, so it always runs the zero PathOptions.
// A caller that wants the options parses first and calls this, giving up the
// plan cache.
func (e *Executor) ExecuteWithOptions(ctx context.Context, query *Query, opts PathOptions) (result *ResultSet, err error) {
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

	// Refuse an unanswerable request before reading any data. A check inside
	// the traversal fires only when a start node exists, which would make the
	// refusal depend on the graph rather than on the request.
	if err := opts.validate(query); err != nil {
		return nil, err
	}

	// Handle UNION before normal execution
	if query.Union != nil && query.UnionNext != nil {
		return e.executeUnion(ctx, query, opts)
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
		return e.executeWithProfiling(ctx, optimizedPlan, query, opts)
	}

	// Handle WITH chaining — needs bindings, not just results
	if query.With != nil && query.Next != nil {
		return e.executeWithChain(ctx, optimizedPlan, query, opts)
	}

	// Execute optimized plan with context
	return e.executePlanWithContext(ctx, optimizedPlan, query, opts)
}

// executeWithChain handles WITH clause chaining between query segments
func (e *Executor) executeWithChain(ctx context.Context, plan *ExecutionPlan, query *Query, opts PathOptions) (*ResultSet, error) {
	execCtx := newExecutionContext(ctx, e.graph, opts)

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

		// A parameter is not a variable, so a WITH does not remove it. Without
		// this copy the next segment read each "$name" as null, and a write
		// there ran on a null condition. The values come from the segment, not
		// from a row, because the segment can have no row.
		for key, value := range query.params {
			newBinding.bindings[key] = value
		}

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

	// Execute the next query segment with projected bindings as initial state.
	// execCtx.truncation (from THIS segment's steps) and the next segment's
	// own error travel together: errors.Join(nil, nil) is nil, so a complete
	// chain still asserts completeness, and errors.Is sees through the join.
	// opts travels into the next segment too. Dropping it here would make the
	// options apply to the first segment of a WITH chain and silently revert
	// for the rest, which is worse than not offering them at all.
	query.Next.InitialBindings = projectedBindings
	result, err := e.ExecuteWithOptions(ctx, query.Next, opts)
	if result != nil && len(execCtx.notices) > 0 {
		result.Notices = slices.Concat(execCtx.notices, result.Notices)
	}
	return result, errors.Join(execCtx.truncation, err)
}

// ExecuteWithParams executes a parameterized query. Parameters are provided as a map
// and injected into the query before execution. ParameterRef values in property maps
// are resolved to actual values, and parameters are made available in bindings as "$name" keys.
func (e *Executor) ExecuteWithParams(query *Query, params map[string]any) (*ResultSet, error) {
	if err := e.injectParams(query, params); err != nil {
		return nil, err
	}
	return e.Execute(query)
}

// ExecuteWithParamsContext is ExecuteWithParams that honours a caller-supplied
// context (timeout / cancellation). The HTTP query handler uses this so that
// parameterized queries get both parameter substitution AND the request
// timeout — calling ExecuteWithContext directly drops req.Parameters and stores
// the literal "&{name}" instead of the value (#237).
func (e *Executor) ExecuteWithParamsContext(ctx context.Context, query *Query, params map[string]any) (*ResultSet, error) {
	if err := e.injectParams(query, params); err != nil {
		return nil, err
	}
	return e.ExecuteWithContext(ctx, query)
}

// injectParams resolves ParameterRef values in property maps, validates that
// every ParameterExpression has a corresponding param, and seeds the initial
// bindings with "$name" keys. Shared by ExecuteWithParams[Context].
func (e *Executor) injectParams(query *Query, params map[string]any) error {
	// Validate and resolve ParameterRef values in property maps
	if err := resolveParameters(query, params); err != nil {
		return err
	}

	// Validate that all ParameterExpression references have corresponding params
	if err := validateParameterExpressions(query, params); err != nil {
		return err
	}

	// A parameter is a row key with the "$" prefix, which avoids a collision
	// with a variable. Each segment keeps the keys, so that no segment has to
	// read them from a row that may not exist. The assignment replaces the
	// field of an earlier run, so that a second run does not keep an old value.
	keys := make(map[string]any, len(params))
	for k, v := range params {
		keys["$"+k] = v
	}
	setSegmentParams(query, keys)
	query.InitialBindings = []*BindingSet{parameterRow(keys)}
	return nil
}

// setSegmentParams sets the parameter keys on query and on each later segment,
// to any depth.
func setSegmentParams(query *Query, keys map[string]any) {
	query.params = keys
	// The callback never returns an error, so the result is always nil.
	_ = forEachLaterSegment(query, func(segment *Query) error {
		setSegmentParams(segment, keys)
		return nil
	})
}

// parameterRow returns a new row that holds the parameter keys. The row is a
// copy, because a step can add a variable to a row that it runs on.
func parameterRow(keys map[string]any) *BindingSet {
	row := &BindingSet{bindings: make(map[string]any, len(keys))}
	for k, v := range keys {
		row.bindings[k] = v
	}
	return row
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
	for _, merge := range query.Merges {
		if err := resolvePatternParams(merge.Pattern, params); err != nil {
			return err
		}
	}
	for _, optional := range query.OptionalMatches {
		for _, pattern := range optional.Patterns {
			if err := resolvePatternParams(pattern, params); err != nil {
				return err
			}
		}
	}
	return forEachLaterSegment(query, func(segment *Query) error {
		return resolveParameters(segment, params)
	})
}

// forEachLaterSegment calls fn on the segment after a WITH and on the next
// branch of a UNION. A check or a rewrite that looked only at the first segment
// let a parameter in a later one read as null, and a write there ran on a null
// condition.
func forEachLaterSegment(query *Query, fn func(*Query) error) error {
	for _, segment := range []*Query{query.Next, query.UnionNext} {
		if segment == nil {
			continue
		}
		if err := fn(segment); err != nil {
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
// It does not modify the map that it reads. The caller (resolvePatternParams) stores the
// new map in the parsed query, so the ParameterRef is gone after the first run, and a second
// run of the same parsed query keeps the first value. A caller that runs one parsed query
// two times with different values for a parameter in a pattern map must parse it again.
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
// ParameterExpression nodes and ensures corresponding params exist. It visits
// each field of Query and of its clauses that can hold an Expression: WHERE,
// OPTIONAL MATCH WHERE, RETURN items, ORDER BY items, WITH items, WITH WHERE,
// SET values, the SET of MERGE (ON CREATE and ON MATCH), and CALL arguments.
// A parameter in a pattern map is a ParameterRef, which resolveParameters checks
// (MATCH, CREATE, MERGE and OPTIONAL MATCH patterns). A parameter in a list literal is
// a ParameterRef in a LiteralExpression, which validateExprParams checks.
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
		if err := validateItemParams(query.Return.Items, params); err != nil {
			return err
		}
		for _, item := range query.Return.OrderBy {
			if err := validateExprParams(item.ValueExpr, params); err != nil {
				return err
			}
		}
	}
	if query.With != nil {
		if err := validateItemParams(query.With.Items, params); err != nil {
			return err
		}
		if query.With.Where != nil {
			if err := validateExprParams(query.With.Where.Expression, params); err != nil {
				return err
			}
		}
	}
	if err := validateSetParams(query.Set, params); err != nil {
		return err
	}
	for _, merge := range query.Merges {
		if err := validateSetParams(merge.OnCreate, params); err != nil {
			return err
		}
		if err := validateSetParams(merge.OnMatch, params); err != nil {
			return err
		}
	}
	if query.Call != nil {
		for _, arg := range query.Call.Arguments {
			if err := validateExprParams(arg, params); err != nil {
				return err
			}
		}
	}
	return forEachLaterSegment(query, func(segment *Query) error {
		return validateParameterExpressions(segment, params)
	})
}

func validateItemParams(items []*ReturnItem, params map[string]any) error {
	for _, item := range items {
		if err := validateExprParams(item.ValueExpr, params); err != nil {
			return err
		}
	}
	return nil
}

func validateSetParams(set *SetClause, params map[string]any) error {
	if set == nil {
		return nil
	}
	for _, assignment := range set.Assignments {
		if err := validateExprParams(assignment.ValueExpr, params); err != nil {
			return err
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
	case *ArithmeticExpression:
		if err := validateExprParams(e.Left, params); err != nil {
			return err
		}
		return validateExprParams(e.Right, params)
	case *UnaryExpression:
		return validateExprParams(e.Operand, params)
	case *LiteralExpression:
		// A list literal can hold a *ParameterRef.
		if list, ok := e.Value.([]any); ok {
			for _, item := range list {
				if ref, ok := item.(*ParameterRef); ok {
					if _, found := params[ref.Name]; !found {
						return fmt.Errorf("missing parameter: $%s", ref.Name)
					}
				}
			}
		}
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
func (e *Executor) executeUnion(ctx context.Context, query *Query, opts PathOptions) (*ResultSet, error) {
	// Execute first segment via a shallow copy to avoid mutating the original AST
	firstSegment := *query
	firstSegment.Union = nil
	firstSegment.UnionNext = nil

	first, err := e.ExecuteWithOptions(ctx, &firstSegment, opts)
	if err != nil {
		return nil, fmt.Errorf("UNION first segment: %w", err)
	}

	// Execute second segment (handles chained UNIONs recursively). A parameter
	// belongs to the whole query, so the next branch gets the "$name" keys that
	// injectParams set on it. Without them each parameter read as null there.
	// The keys come from the segment, not from a row: the first part can have
	// no row, and then no row could carry them.
	// The assignment is unconditional, so a run with no parameter does not keep
	// the values of an earlier run of the same parsed query.
	query.UnionNext.InitialBindings = nil
	if len(query.UnionNext.params) > 0 {
		query.UnionNext.InitialBindings = []*BindingSet{parameterRow(query.UnionNext.params)}
	}
	second, err := e.ExecuteWithOptions(ctx, query.UnionNext, opts)
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
		// Both sides ran; a notice from either is still true of the query.
		Notices: slices.Concat(first.Notices, second.Notices),
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
