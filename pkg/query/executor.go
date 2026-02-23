package query

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
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
