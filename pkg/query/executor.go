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
func (e *Executor) ExecuteWithOptions(ctx context.Context, query *Query, opts PathOptions) (*ResultSet, error) {
	return e.execute(ctx, query, opts, nil)
}

// execute is the one place that every entry point with a context goes
// through. params are the parameters of this run: nil for an entry point that
// takes none. They replace any parameters that ctx carries from an outer run,
// so a nested call never reads the values of its caller.
func (e *Executor) execute(ctx context.Context, query *Query, opts PathOptions, params map[string]any) (result *ResultSet, err error) {
	// Panic recovery - prevent server crashes from query execution panics
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			log.Printf("PANIC in query execution: %v\n%s", r, stack)
			err = fmt.Errorf("query execution panicked: %v", r)
			result = nil
		}
	}()

	// Refuse a name no clause defines before any segment runs, for the whole
	// WITH chain and both sides of a UNION: an earlier segment can write. The
	// segments below run through executeSegment, which does not check again.
	if err := checkQueryNames(query); err != nil {
		return nil, err
	}

	// Refuse a parameter that this run does not hold, in a pattern or in an
	// expression of any segment, before any segment runs. Without it the
	// query returns no rows, or null, with no error. EXPLAIN builds the plan
	// and returns it before any step runs (executeSegment), and a plan needs no
	// value. A UNION is the exception: executeUnion runs its second part even
	// under EXPLAIN, so it keeps the check. PROFILE runs the query and keeps it.
	if !explainsOnly(query) {
		if err := validateParameterExpressions(query, params); err != nil {
			return nil, err
		}
	}
	ctx = withRunParams(ctx, params)
	// Set for every run, so that a nested call does not inherit PROFILE.
	ctx = withProfiling(ctx, query.Profile)

	return e.executeSegment(ctx, query, opts, query.InitialBindings)
}

// executeSegment runs one query segment, and the segments that follow it
// through WITH and UNION. It is ExecuteWithOptions after the static checks
// of the whole statement.
//
// initial holds the rows that enter this segment, or nil for one empty row. It
// is an argument and not a field of query: every run of a parsed Query shares
// it, and a write there raced with the other runs.
func (e *Executor) executeSegment(ctx context.Context, query *Query, opts PathOptions, initial []*BindingSet) (*ResultSet, error) {
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
		return e.executeUnion(ctx, query, opts, initial)
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

	// Handle WITH chaining — needs bindings, not just results
	if query.With != nil && query.Next != nil {
		return e.executeWithChain(ctx, optimizedPlan, query, opts, initial)
	}

	// Execute optimized plan with context
	return e.executePlanWithContext(ctx, optimizedPlan, query, opts, initial)
}

// executeWithChain handles WITH clause chaining between query segments
func (e *Executor) executeWithChain(ctx context.Context, plan *ExecutionPlan, query *Query, opts PathOptions, initial []*BindingSet) (*ResultSet, error) {
	execCtx := newExecutionContext(ctx, e.graph, opts)
	execCtx.results = seedRows(ctx, initial)

	// Refused before any step runs, so the refusal does not depend on the data.
	if err := checkWithChain(query); err != nil {
		return nil, err
	}

	// Execute each step
	var profiles []StepProfile
	for _, step := range plan.Steps {
		if err := execCtx.CheckCancellation(); err != nil {
			return nil, err
		}
		if err := runStep(execCtx, step, &profiles); err != nil {
			return nil, err
		}
	}

	projectedBindings, err := e.projectWith(execCtx, query.With.Items)
	if err != nil {
		return nil, err
	}
	// The projection keeps only the names the WITH lists, and a parameter is
	// not one of them. WITH ... WHERE x = $v reads it from these rows.
	projectedBindings = seedRows(ctx, projectedBindings)

	// Apply optional WITH WHERE filter
	if query.With.Where != nil {
		filtered := make([]*BindingSet, 0)
		for _, binding := range projectedBindings {
			match, err := evalPredicate(query.With.Where.Expression, binding.bindings)
			if err != nil {
				if isRefusal(err) {
					return nil, err
				}
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
	result, err := e.executeSegment(ctx, query.Next, opts, projectedBindings)
	if result != nil {
		// The profile covers this segment and then each one that follows.
		result.Profile = slices.Concat(profiles, result.Profile)
	}
	if result != nil && len(execCtx.notices) > 0 {
		result.Notices = slices.Concat(execCtx.notices, result.Notices)
	}
	return result, errors.Join(execCtx.truncation, err)
}

// ExecuteWithParams executes a parameterized query. Parameters are provided as a map
// and injected into the query before execution. ParameterRef values in property maps
// are resolved to actual values, and parameters are made available in bindings as "$name" keys.
func (e *Executor) ExecuteWithParams(query *Query, params map[string]any) (*ResultSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.queryTimeout)
	defer cancel()
	return e.ExecuteWithParamsContext(ctx, query, params)
}

// ExecuteWithParamsContext is ExecuteWithParams that honours a caller-supplied
// context (timeout / cancellation). The HTTP query handler uses this so that
// parameterized queries get both parameter substitution AND the request
// timeout — calling ExecuteWithContext directly drops req.Parameters and stores
// the literal "&{name}" instead of the value (#237).
func (e *Executor) ExecuteWithParamsContext(ctx context.Context, query *Query, params map[string]any) (*ResultSet, error) {
	prepared, err := e.injectParams(query, params)
	if err != nil {
		return nil, err
	}
	return e.execute(ctx, prepared, PathOptions{}, params)
}

// injectParams resolves ParameterRef values in property maps. execute passes
// params on with withRunParams, so that each segment can read them, and
// validates them. Shared by ExecuteWithParams[Context].
//
// It returns a copy of query for the caller to run and leaves query alone: the
// caller may run the same parsed Query again with other parameters.
func (e *Executor) injectParams(query *Query, params map[string]any) (*Query, error) {
	// EXPLAIN needs no value, so a parameter that is not supplied stays.
	// validateParameterExpressions, in execute, refuses what remains in any
	// query that runs.
	return resolveParameters(query, params, explainsOnly(query))
}

// explainsOnly reports whether the query returns a plan and runs no step.
func explainsOnly(query *Query) bool {
	return query.Explain && query.UnionNext == nil
}

// validateParameterExpressions refuses a parameter that params does not hold,
// in every expression and every pattern of every segment of the statement: the
// WITH chain and each part of a UNION. params may be nil. It walks with
// walkQueryExpressions, which is the one place that knows where a clause keeps
// an expression. The error is a RefusalError, so a server answers 400.
//
// A parameter that a pattern still holds is refused even when params has its
// name: resolveParameters replaces each one, so a survivor means that a clause
// escaped it, and a *ParameterRef matches no stored value.
func validateParameterExpressions(query *Query, params map[string]any) error {
	var err error
	walkQueryExpressions(query, func(clause string, expr Expression) {
		if err != nil {
			return
		}
		pe, ok := expr.(*ParameterExpression)
		if !ok {
			return
		}
		if _, exists := params[pe.Name]; !exists || patternClauses[clause] {
			err = refusef("missing parameter: $%s", pe.Name)
		}
	})
	return err
}

// executeUnion executes two query segments and combines their results.
// UNION deduplicates rows; UNION ALL preserves all rows.
func (e *Executor) executeUnion(ctx context.Context, query *Query, opts PathOptions, initial []*BindingSet) (*ResultSet, error) {
	// Execute first segment via a shallow copy to avoid mutating the original AST
	firstSegment := *query
	firstSegment.Union = nil
	firstSegment.UnionNext = nil

	first, err := e.executeSegment(ctx, &firstSegment, opts, initial)
	if err != nil {
		return nil, fmt.Errorf("UNION first segment: %w", err)
	}

	// Execute second segment (handles chained UNIONs recursively)
	second, err := e.executeSegment(ctx, query.UnionNext, opts, query.UnionNext.InitialBindings)
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

	combined.Profile = slices.Concat(first.Profile, second.Profile)
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
	// This path does not go through ExecuteWithOptions, so it checks here.
	if err := checkQueryNames(query); err != nil {
		return nil, err
	}
	if explainsOnly(query) {
		// A plan needs no value, and this path would otherwise run the query.
		return buildExplainResult(e.optimizer.Optimize(e.buildExecutionPlan(query), query)), nil
	}
	if err := validateParameterExpressions(query, nil); err != nil {
		return nil, err
	}

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
