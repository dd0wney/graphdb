package query

import (
	"context"
	"fmt"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

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
	context  context.Context // For cancellation/timeout
	graph    *storage.GraphStorage
	bindings map[string]any // Variable bindings
	results  []*BindingSet
}

// IsCancelled checks if the execution context has been cancelled
func (ec *ExecutionContext) IsCancelled() bool {
	if ec.context == nil {
		return false
	}
	select {
	case <-ec.context.Done():
		return true
	default:
		return false
	}
}

// CheckCancellation returns an error if the context is cancelled
func (ec *ExecutionContext) CheckCancellation() error {
	if ec.context == nil {
		return nil
	}
	select {
	case <-ec.context.Done():
		return fmt.Errorf("query execution cancelled: %w", ec.context.Err())
	default:
		return nil
	}
}

// BindingSet represents a set of variable bindings
type BindingSet struct {
	bindings map[string]any
}

// StepProfile holds profiling data for a single execution step
type StepProfile struct {
	StepName string
	Detail   string
	Duration time.Duration
	RowsOut  int
}

// ResultSet represents query results
type ResultSet struct {
	Columns []string
	Rows    []map[string]any
	Count   int
	Profile []StepProfile // Populated when PROFILE is used
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

	// Add UNWIND step (after filter, before create)
	if query.Unwind != nil {
		plan.Steps = append(plan.Steps, &UnwindStep{unwind: query.Unwind})
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

// executePlan executes an execution plan (without context - for backwards compatibility)
func (e *Executor) executePlan(plan *ExecutionPlan, query *Query) (*ResultSet, error) {
	return e.executePlanWithContext(context.Background(), plan, query)
}

// executePlanWithContext executes an execution plan with context support
func (e *Executor) executePlanWithContext(ctx context.Context, plan *ExecutionPlan, query *Query) (*ResultSet, error) {
	execCtx := &ExecutionContext{
		context:  ctx,
		graph:    e.graph,
		bindings: make(map[string]any),
		results:  make([]*BindingSet, 0),
	}

	// Start with empty binding
	execCtx.results = append(execCtx.results, &BindingSet{
		bindings: make(map[string]any),
	})

	// Execute each step with cancellation checks
	for i, step := range plan.Steps {
		// Check for cancellation before each step
		if err := execCtx.CheckCancellation(); err != nil {
			return nil, fmt.Errorf("cancelled at step %d: %w", i, err)
		}

		if err := step.Execute(execCtx); err != nil {
			return nil, err
		}
	}

	// Final cancellation check before building results
	if err := execCtx.CheckCancellation(); err != nil {
		return nil, fmt.Errorf("cancelled before building results: %w", err)
	}

	// Build final result set
	if query.Return != nil {
		return e.buildResultSet(execCtx, query.Return, query.Limit, query.Skip), nil
	}

	// For write queries, return count
	return &ResultSet{
		Columns: []string{"affected"},
		Rows:    []map[string]any{{"affected": len(execCtx.results)}},
		Count:   len(execCtx.results),
	}, nil
}
